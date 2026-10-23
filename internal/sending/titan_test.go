package sending

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// simulatedSMTP is a local implicit-TLS SMTP service that follows a scripted
// response table. It records every command so a test can prove which stages ran
// and which messages were attempted.
type simulatedSMTP struct {
	authCode  int // response to AUTH; 0 means 235
	mailCode  int
	rcptCode  int
	dataCode  int // response to DATA; 0 means 354
	finalCode int // final response after the terminating dot; 0 means 250
	// dropAfterData closes the connection after the complete message without a
	// final response, simulating a lost acceptance reply.
	dropAfterData bool
	quitCode      int
	ehloNoAuth    bool
	// certificate lets a test share one throwaway certificate with another
	// simulated service, so one root pool verifies both.
	certificate *tls.Certificate

	mu           sync.Mutex
	commands     []string
	message      string
	transactions int
}

func (s *simulatedSMTP) start(t *testing.T) (address string, roots *x509.CertPool) {
	t.Helper()
	certificate, roots := selfSignedCertificate(t)
	if s.certificate != nil {
		certificate = *s.certificate
		roots = nil
	}
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := listener.Close(); err != nil {
			t.Error(err)
		}
	})
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go s.serve(conn)
		}
	}()
	return listener.Addr().String(), roots
}

func (s *simulatedSMTP) serve(conn net.Conn) {
	defer closeConnection(conn)
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	reply := func(code int, message string) bool {
		if _, err := writer.WriteString(strconv.Itoa(code) + " " + message + "\r\n"); err != nil {
			return false
		}
		return writer.Flush() == nil
	}
	if !reply(220, "simulated ESMTP") {
		return
	}
	inData := false
	var body strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if inData {
			if line == "." {
				inData = false
				s.mu.Lock()
				s.message = body.String()
				s.transactions++
				s.mu.Unlock()
				if s.dropAfterData {
					return
				}
				if !reply(orDefault(s.finalCode, 250), "accepted") {
					return
				}
				continue
			}
			body.WriteString(line + "\n")
			continue
		}
		s.mu.Lock()
		s.commands = append(s.commands, line)
		s.mu.Unlock()
		verb := strings.ToUpper(strings.Fields(line)[0])
		switch verb {
		case "EHLO", "HELO":
			if _, err := writer.WriteString("250-simulated\r\n"); err != nil {
				return
			}
			if s.ehloNoAuth {
				if !reply(250, "OK") {
					return
				}
				continue
			}
			if _, err := writer.WriteString("250 AUTH PLAIN\r\n"); err != nil {
				return
			}
			if writer.Flush() != nil {
				return
			}
		case "AUTH":
			if !reply(orDefault(s.authCode, 235), "authentication") {
				return
			}
		case "MAIL":
			if !reply(orDefault(s.mailCode, 250), "sender ok") {
				return
			}
		case "RCPT":
			if !reply(orDefault(s.rcptCode, 250), "recipient ok") {
				return
			}
		case "DATA":
			code := orDefault(s.dataCode, 354)
			if code != 354 {
				if !reply(code, "no data") {
					return
				}
				continue
			}
			if !reply(354, "send data") {
				return
			}
			inData = true
		case "QUIT":
			_ = reply(orDefault(s.quitCode, 221), "bye")
			return
		default:
			if !reply(250, "ok") {
				return
			}
		}
	}
}

func (s *simulatedSMTP) hasCommand(prefix string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, command := range s.commands {
		if strings.HasPrefix(strings.ToUpper(command), strings.ToUpper(prefix)) {
			return true
		}
	}
	return false
}

func (s *simulatedSMTP) countCommands(prefix string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, command := range s.commands {
		if strings.HasPrefix(strings.ToUpper(command), strings.ToUpper(prefix)) {
			count++
		}
	}
	return count
}

func (s *simulatedSMTP) sentMessage() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.message
}

func orDefault(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

// selfSignedCertificate builds a throwaway certificate for 127.0.0.1 and
// returns its root pool, so certificate verification is exercised for real.
func selfSignedCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "simulated smtp"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
		IsCA:         true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
	)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool.AddCert(parsed)
	return certificate, pool
}

func TestTitanSendAcceptsSingleRecipientHTMLMessage(t *testing.T) {
	server := &simulatedSMTP{}
	address, roots := server.start(t)
	sender := NewTitan(address, "owner@example.test", "password", TitanOptions{
		Timeout: 5 * time.Second,
		TLS:     &tls.Config{RootCAs: roots, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12},
	})

	result := sender.Send(context.Background(), Message{
		Recipient: "recipient@example.test",
		Subject:   "Héllo subject",
		HTML:      "<p>Héllo body</p>",
	})
	if result.Kind != OutcomeSubmitted {
		t.Fatalf("result = %+v, want submitted", result)
	}
	if count := server.countCommands("RCPT"); count != 1 {
		t.Fatalf("RCPT count = %d, want one recipient per message", count)
	}
	message := server.sentMessage()
	if !strings.Contains(message, "Subject:") || !strings.Contains(message, "text/html") {
		t.Fatalf("message is not an HTML message with a subject: %q", message)
	}
	if strings.Contains(message, "Content-Disposition: attachment") {
		t.Fatal("message must not carry attachments")
	}
}

func TestTitanSendClassification(t *testing.T) {
	for _, tc := range []struct {
		name       string
		script     *simulatedSMTP
		want       OutcomeKind
		correction bool
	}{
		{name: "permanent recipient rejection", script: &simulatedSMTP{rcptCode: 550}, want: OutcomeDefiniteFailure, correction: true},
		{name: "temporary recipient rejection", script: &simulatedSMTP{rcptCode: 451}, want: OutcomeSafeRetry},
		{name: "authentication failure", script: &simulatedSMTP{authCode: 535}, want: OutcomeAuthFailure},
		{name: "data command rejection", script: &simulatedSMTP{dataCode: 554}, want: OutcomeDefiniteFailure, correction: true},
		{name: "lost final response", script: &simulatedSMTP{dropAfterData: true}, want: OutcomeUncertain},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := tc.script
			address, roots := server.start(t)
			sender := NewTitan(address, "owner@example.test", "password", TitanOptions{
				Timeout: 5 * time.Second,
				TLS:     &tls.Config{RootCAs: roots, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12},
			})
			result := sender.Send(context.Background(), Message{Recipient: "recipient@example.test", Subject: "s", HTML: "<p>b</p>"})
			if result.Kind != tc.want {
				t.Fatalf("kind = %q, want %q (result %+v)", result.Kind, tc.want, result)
			}
			if result.RequiresCorrection != tc.correction {
				t.Fatalf("RequiresCorrection = %v, want %v", result.RequiresCorrection, tc.correction)
			}
			if result.Error == "" {
				t.Fatal("a non-submitted result needs an operator-safe reason")
			}
		})
	}
}

func TestTitanSendUncertainIsNotRetriedInternally(t *testing.T) {
	server := &simulatedSMTP{dropAfterData: true}
	address, roots := server.start(t)
	sender := NewTitan(address, "owner@example.test", "password", TitanOptions{
		Timeout: 5 * time.Second,
		TLS:     &tls.Config{RootCAs: roots, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12},
	})
	if result := sender.Send(context.Background(), Message{Recipient: "recipient@example.test", Subject: "s", HTML: "<p>b</p>"}); result.Kind != OutcomeUncertain {
		t.Fatalf("kind = %q, want uncertain", result.Kind)
	}
	if count := server.countCommands("MAIL"); count != 1 {
		t.Fatalf("MAIL count = %d; a sender must never retry internally", count)
	}
}

func TestTitanSendRejectsInvalidRecipientBeforeConnecting(t *testing.T) {
	server := &simulatedSMTP{}
	address, roots := server.start(t)
	sender := NewTitan(address, "owner@example.test", "password", TitanOptions{
		Timeout: 5 * time.Second,
		TLS:     &tls.Config{RootCAs: roots, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12},
	})
	result := sender.Send(context.Background(), Message{Recipient: "not an address", Subject: "s", HTML: "<p>b</p>"})
	if result.Kind != OutcomeDefiniteFailure || !result.RequiresCorrection {
		t.Fatalf("result = %+v, want a correction-required definite failure", result)
	}
	if len(server.commands) != 0 {
		t.Fatal("an invalid recipient must not open an SMTP transaction")
	}
}

func TestTitanCertificateFailureIsNotRetried(t *testing.T) {
	server := &simulatedSMTP{}
	address, _ := server.start(t)
	sender := NewTitan(address, "owner@example.test", "password", TitanOptions{Timeout: 5 * time.Second})
	result := sender.Send(context.Background(), Message{Recipient: "recipient@example.test", Subject: "s", HTML: "<p>b</p>"})
	if result.Kind != OutcomeDefiniteFailure || !result.PauseMailbox {
		t.Fatalf("result = %+v, want a paused definite failure", result)
	}
	if only := server.commands; len(only) != 0 {
		t.Fatal("certificate verification failed after an SMTP command was issued")
	}
}

func TestValidateTitanProvesControlWithoutSending(t *testing.T) {
	server := &simulatedSMTP{}
	address, roots := server.start(t)
	if err := ValidateTitan(context.Background(), address, "owner@example.test", "password", TitanOptions{
		Timeout: 5 * time.Second,
		TLS:     &tls.Config{RootCAs: roots, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12},
	}); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !server.hasCommand("AUTH") {
		t.Fatal("validation must authenticate")
	}
	if server.hasCommand("MAIL") || server.hasCommand("RCPT") || server.hasCommand("DATA") {
		t.Fatal("validation must not start a message transaction")
	}
}

func TestValidateTitanReportsActionableFailures(t *testing.T) {
	server := &simulatedSMTP{authCode: 535}
	address, roots := server.start(t)
	err := ValidateTitan(context.Background(), address, "owner@example.test", "wrong", TitanOptions{
		Timeout: 5 * time.Second,
		TLS:     &tls.Config{RootCAs: roots, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12},
	})
	if !errors.Is(err, ErrTitanAuth) {
		t.Fatalf("err = %v, want ErrTitanAuth", err)
	}

	// An untrusted certificate is a distinct, actionable failure.
	untrusted := &simulatedSMTP{}
	untrustedAddress, _ := untrusted.start(t)
	err = ValidateTitan(context.Background(), untrustedAddress, "owner@example.test", "password", TitanOptions{Timeout: 5 * time.Second})
	if !errors.Is(err, ErrTitanCertificate) {
		t.Fatalf("err = %v, want ErrTitanCertificate", err)
	}
}
