package sending

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"

	mailyak "github.com/domodwyer/mailyak/v3"
)

// Titan validation failures, reported without credential or provider detail so
// the UI can show an actionable, non-secret message.
var (
	ErrTitanAuth        = errors.New("mailbox credentials were rejected by Titan")
	ErrTitanCertificate = errors.New("the Titan server certificate could not be verified")
	ErrTitanUnreachable = errors.New("the Titan SMTP endpoint could not be reached")
)

// Titan submits one HTML message per connection through GoDaddy Titan's
// implicit-TLS SMTP endpoint. It performs explicit AUTH, MAIL, RCPT, DATA and
// final-response stages so acceptance boundaries stay observable, never retries
// internally, and treats a lost final response as uncertain rather than
// retryable.
type Titan struct {
	address  string
	username string
	password string
	timeout  time.Duration
	tls      *tls.Config
}

// TitanOptions configures one Titan sender. Address is the provider endpoint;
// it is never taken from operator input. TLS is only set by simulated servers
// in tests; production always verifies the provider certificate.
type TitanOptions struct {
	Timeout time.Duration
	TLS     *tls.Config
}

// NewTitan binds a sender to one authenticated mailbox. The full mailbox
// address is both the SMTP username and the sender.
func NewTitan(address, username, password string, options TitanOptions) *Titan {
	timeout := options.Timeout
	if timeout <= 0 || timeout > 30*time.Second {
		timeout = 20 * time.Second
	}
	return &Titan{address: address, username: username, password: password, timeout: timeout, tls: options.TLS}
}

// ValidateTitan proves control of a Titan mailbox without sending a message:
// it establishes the encrypted connection, authenticates, and quits. No MAIL,
// RCPT or DATA transaction is issued, so no message is attempted.
func ValidateTitan(ctx context.Context, address, username, password string, options TitanOptions) error {
	sender := NewTitan(address, username, password, options)
	client, conn, cancel, err := sender.connect(ctx)
	if err != nil {
		if _, ok := errors.AsType[*tls.CertificateVerificationError](err); ok {
			return ErrTitanCertificate
		}
		return ErrTitanUnreachable
	}
	defer cancel()
	defer closeConnection(conn)
	if err := sender.authenticate(client); err != nil {
		if isPermanentSMTP(err) {
			return ErrTitanAuth
		}
		return ErrTitanUnreachable
	}
	// QUIT failure after authentication cannot invalidate the proven control.
	quitSession(client)
	return nil
}

// Send submits exactly one message. It returns a semantic result and never a
// provider diagnostic.
func (t *Titan) Send(ctx context.Context, message Message) SendResult {
	address, err := mail.ParseAddress(message.Recipient)
	if err != nil || address.Address != message.Recipient || strings.ContainsAny(message.Recipient, "\r\n") {
		return SendResult{Kind: OutcomeDefiniteFailure, Error: "The recipient address is invalid.", RequiresCorrection: true}
	}
	payload, err := t.mime(message)
	if err != nil {
		return SendResult{Kind: OutcomeDefiniteFailure, Error: "The message could not be prepared for submission."}
	}
	client, conn, cancel, err := t.connect(ctx)
	if err != nil {
		return resultForConnect(err)
	}
	defer cancel()
	defer closeConnection(conn)
	if err := t.authenticate(client); err != nil {
		return resultForAuth(err)
	}
	if err := client.Mail(t.username); err != nil {
		return resultBeforeData(err)
	}
	if err := client.Rcpt(message.Recipient); err != nil {
		return resultBeforeData(err)
	}
	writer, err := client.Data()
	if err != nil {
		return resultBeforeData(err)
	}
	if _, err := writer.Write(payload); err != nil {
		// The body was partially transmitted: the server might still accept
		// it, so this is never a safe retry.
		return SendResult{Kind: OutcomeUncertain, Error: "The submission outcome is unknown; review is required.", PauseMailbox: true}
	}
	// Close sends the terminating dot and returns the server's final response.
	if err := writer.Close(); err != nil {
		return resultAfterData(err)
	}
	// Acceptance is now definite. A later QUIT/close failure cannot undo it.
	quitSession(client)
	return SendResult{Kind: OutcomeSubmitted}
}

// mime builds the single-recipient HTML message with the shared MIME builder.
func (t *Titan) mime(message Message) ([]byte, error) {
	host, _, err := net.SplitHostPort(t.address)
	if err != nil {
		host = t.address
	}
	mail := mailyak.New(host, nil)
	mail.From(t.username)
	mail.To(message.Recipient)
	mail.Subject(message.Subject)
	mail.HTML().Set(message.HTML)
	buffer, err := mail.MimeBuf()
	if err != nil {
		return nil, fmt.Errorf("build the message: %w", err)
	}
	return buffer.Bytes(), nil
}

// connect establishes a context-aware implicit-TLS connection with certificate
// and hostname verification. It never downgrades to plaintext.
func (t *Titan) connect(ctx context.Context) (*smtp.Client, net.Conn, context.CancelFunc, error) {
	host, _, err := net.SplitHostPort(t.address)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("invalid SMTP endpoint: %w", err)
	}
	config := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if t.tls != nil {
		config = t.tls.Clone()
		if config.ServerName == "" {
			config.ServerName = host
		}
	}
	dialer := &tls.Dialer{NetDialer: &net.Dialer{Timeout: t.timeout}, Config: config}
	dialCtx, cancelDial := context.WithTimeout(ctx, t.timeout)
	conn, err := dialer.DialContext(dialCtx, "tcp", t.address)
	cancelDial()
	if err != nil {
		return nil, nil, nil, err
	}
	// A cancelled operation or an expired deadline must unblock a pending
	// read/write instead of leaving the worker waiting on the network.
	deadline := time.Now().Add(t.timeout)
	if err := conn.SetDeadline(deadline); err != nil {
		closeConnection(conn)
		return nil, nil, nil, err
	}
	stop := context.AfterFunc(ctx, func() { closeConnection(conn) })
	cancel := func() {
		stop()
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		cancel()
		closeConnection(conn)
		return nil, nil, nil, err
	}
	return client, conn, cancel, nil
}

func (t *Titan) authenticate(client *smtp.Client) error {
	host, _, err := net.SplitHostPort(t.address)
	if err != nil {
		return err
	}
	if ok, _ := client.Extension("AUTH"); !ok {
		return errors.New("the SMTP server does not offer authentication")
	}
	return client.Auth(smtp.PlainAuth("", t.username, t.password, host))
}

// resultForConnect classifies a failure before any SMTP command. A TLS or
// hostname verification failure is a configuration failure, not a transient
// network error, so it pauses the mailbox instead of retrying.
func resultForConnect(err error) SendResult {
	// Every certificate verification failure (hostname mismatch, unknown
	// authority, expired) arrives wrapped in this type.
	if _, ok := errors.AsType[*tls.CertificateVerificationError](err); ok {
		return SendResult{Kind: OutcomeDefiniteFailure, Error: "Titan's server certificate could not be verified; reconnect the mailbox.", PauseMailbox: true}
	}
	var netErr net.Error
	if errors.As(err, &netErr) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		// DNS, dial and TLS handshake failures happen before any SMTP command,
		// so submission provably did not occur.
		return SendResult{Kind: OutcomeSafeRetry, Error: "Titan could not be reached before submission."}
	}
	return SendResult{Kind: OutcomeSafeRetry, Error: "Titan could not be reached before submission."}
}

func resultForAuth(err error) SendResult {
	if isPermanentSMTP(err) {
		return SendResult{Kind: OutcomeAuthFailure, Error: "Titan rejected the mailbox credentials; reconnect the mailbox.", PauseMailbox: true}
	}
	return SendResult{Kind: OutcomeAuthFailure, Error: "Titan authentication failed; reconnect the mailbox.", PauseMailbox: true}
}

// resultBeforeData classifies MAIL/RCPT/DATA command failures, where the
// message body has definitely not been accepted.
func resultBeforeData(err error) SendResult {
	if code, ok := smtpCode(err); ok {
		switch {
		case code == 550 || code == 551 || code == 553 || code == 554:
			return SendResult{Kind: OutcomeDefiniteFailure, Error: "Titan rejected the recipient address.", RequiresCorrection: true}
		case code == 421 || code == 450 || code == 451 || code == 452:
			return SendResult{Kind: OutcomeSafeRetry, Error: "Titan temporarily rejected the submission."}
		case code >= 500:
			return SendResult{Kind: OutcomeDefiniteFailure, Error: "Titan rejected the submission; operator action is required.", PauseMailbox: true}
		}
	}
	var netErr net.Error
	if errors.As(err, &netErr) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return SendResult{Kind: OutcomeSafeRetry, Error: "The connection ended before submission."}
	}
	return SendResult{Kind: OutcomeSafeRetry, Error: "The submission could not be started."}
}

// resultAfterData classifies the final DATA response. A reply that definitively
// rejects the message is not a duplicate risk; a lost reply is.
func resultAfterData(err error) SendResult {
	if code, ok := smtpCode(err); ok {
		switch {
		case code == 552 || code == 554:
			return SendResult{Kind: OutcomeDefiniteFailure, Error: "Titan rejected the message content.", RequiresCorrection: true}
		case code == 450 || code == 451 || code == 452:
			return SendResult{Kind: OutcomeSafeRetry, Error: "Titan temporarily rejected the message."}
		case code >= 500:
			return SendResult{Kind: OutcomeDefiniteFailure, Error: "Titan rejected the message; operator action is required.", PauseMailbox: true}
		}
	}
	return SendResult{Kind: OutcomeUncertain, Error: "The submission outcome is unknown; review is required.", PauseMailbox: true}
}

func smtpCode(err error) (int, bool) {
	if protoErr, ok := errors.AsType[*textproto.Error](err); ok {
		return protoErr.Code, true
	}
	return 0, false
}

// closeConnection closes the SMTP transport after the exchange. A close error
// cannot change an outcome that the server's responses already decided, so it
// is deliberately not propagated.
func closeConnection(conn net.Conn) {
	conn.Close() //nolint:errcheck // close errors cannot change a settled SMTP outcome
}

// quitSession ends an authenticated SMTP session. QUIT failure is harmless:
// authentication or message acceptance was already established.
func quitSession(client *smtp.Client) {
	client.Quit() //nolint:errcheck // the session outcome is already settled
}

func isPermanentSMTP(err error) bool {
	code, ok := smtpCode(err)
	return ok && code >= 500
}
