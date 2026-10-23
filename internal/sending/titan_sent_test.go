package sending

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/mail"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// simulatedFolder is one entry of a simulated mailbox listing.
type simulatedFolder struct {
	name       string
	attributes string
}

// appendedCopy is one received APPEND, recorded for content assertions.
type appendedCopy struct {
	mailbox, command string
	date             time.Time
	raw              []byte
}

// simulatedIMAP is a local implicit-TLS IMAP service that follows a scripted
// response table. It records every command and every appended message so a test
// can prove which mailbox was used and exactly what was saved.
type simulatedIMAP struct {
	folders          []simulatedFolder
	loginCode        int  // non-zero rejects LOGIN
	listCode         int  // non-zero rejects LIST
	appendCode       int  // non-zero rejects APPEND
	logoutIncomplete bool // answers BYE without the tagged LOGOUT completion
	dropGreeting     bool // accepts the transport and never greets
	dropAfterAppend  bool // receives the message without confirming APPEND
	// certificate lets a test share one throwaway certificate with the SMTP
	// service, so one root pool verifies both.
	certificate *tls.Certificate

	mu       sync.Mutex
	commands []string
	logins   []string
	appends  int
	appended []appendedCopy
}

var (
	literalLength = regexp.MustCompile(`\{(\d+)\}$`)
	appendDate    = regexp.MustCompile(`(\d{2}-[A-Za-z]{3}-\d{4} \d{2}:\d{2}:\d{2} [+-]\d{4})`)
)

func (s *simulatedIMAP) start(t *testing.T) (address string, roots *x509.CertPool) {
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

func (s *simulatedIMAP) serve(conn net.Conn) {
	defer closeConnection(conn)
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	reply := func(line string) bool {
		if _, err := writer.WriteString(line + "\r\n"); err != nil {
			return false
		}
		return writer.Flush() == nil
	}
	if s.dropGreeting {
		return
	}
	if !reply("* OK simulated IMAP4rev1 ready") {
		return
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		tag, verb := fields[0], strings.ToUpper(fields[1])
		s.mu.Lock()
		s.commands = append(s.commands, line)
		s.mu.Unlock()
		switch verb {
		case "LOGIN":
			if len(fields) > 2 {
				s.mu.Lock()
				s.logins = append(s.logins, strings.Trim(fields[2], `"`))
				s.mu.Unlock()
			}
			if s.loginCode != 0 {
				if !reply(tag + " NO authentication rejected") {
					return
				}
				continue
			}
			if !reply(tag + " OK LOGIN completed") {
				return
			}
		case "LIST":
			if s.listCode != 0 {
				if !reply(tag + " NO listing unavailable") {
					return
				}
				continue
			}
			for _, folder := range s.folders {
				if !reply(fmt.Sprintf(`* LIST (%s) "/" "%s"`, folder.attributes, folder.name)) {
					return
				}
			}
			if !reply(tag + " OK LIST completed") {
				return
			}
		case "APPEND":
			length, ok := literalSize(line)
			if !ok {
				_ = reply(tag + " BAD append")
				return
			}
			// A tagged NO that arrives while the client is still waiting for a
			// literal continuation can be lost by the client library. Read the
			// literal first, then refuse, so the refusal is deterministic.
			if !reply("+ Ready for literal data") {
				return
			}
			raw := make([]byte, length)
			if _, err := io.ReadFull(reader, raw); err != nil {
				return
			}
			// The client terminates the literal with CRLF.
			if _, err := reader.ReadString('\n'); err != nil {
				return
			}
			if s.appendCode != 0 {
				if !reply(tag + " NO append refused") {
					return
				}
				continue
			}
			s.mu.Lock()
			s.appends++
			s.appended = append(s.appended, appendedCopy{mailbox: appendMailbox(fields), command: line, date: appendTimestamp(line), raw: raw})
			s.mu.Unlock()
			if s.dropAfterAppend {
				return
			}
			if !reply(tag + " OK APPEND completed") {
				return
			}
		case "LOGOUT":
			if !reply("* BYE logging out") {
				return
			}
			if s.logoutIncomplete {
				return
			}
			_ = reply(tag + " OK LOGOUT completed")
			return
		default:
			if !reply(tag + " OK completed") {
				return
			}
		}
	}
}

func literalSize(line string) (int, bool) {
	match := literalLength.FindStringSubmatch(line)
	if match == nil {
		return 0, false
	}
	size := 0
	for _, digit := range match[1] {
		size = size*10 + int(digit-'0')
	}
	return size, size > 0
}

func appendMailbox(fields []string) string {
	if len(fields) < 3 {
		return ""
	}
	return strings.Trim(fields[2], `"`)
}

func appendTimestamp(line string) time.Time {
	match := appendDate.FindStringSubmatch(line)
	if match == nil {
		return time.Time{}
	}
	at, err := time.Parse("02-Jan-2006 15:04:05 -0700", match[1])
	if err != nil {
		return time.Time{}
	}
	return at
}

func (s *simulatedIMAP) snapshot() (commands, logins []string, appends int, appended []appendedCopy) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.commands...), append([]string(nil), s.logins...), s.appends, append([]appendedCopy(nil), s.appended...)
}

func (s *simulatedIMAP) hasCommand(verb string) bool {
	commands, _, _, _ := s.snapshot()
	for _, command := range commands {
		fields := strings.Fields(command)
		if len(fields) > 1 && strings.EqualFold(fields[1], verb) {
			return true
		}
	}
	return false
}

const (
	simulatedMailboxUser = "owner@example.test"
	simulatedMailboxPass = "mailbox-password"
)

// copyServers starts a simulated SMTP service and one simulated IMAP service
// that share a throwaway certificate, and returns a sender bound to both.
func copyServers(t *testing.T, imapServer *simulatedIMAP) (*simulatedSMTP, *Titan) {
	t.Helper()
	certificate, roots := selfSignedCertificate(t)
	imapServer.certificate = &certificate
	smtpServer := &simulatedSMTP{certificate: &certificate}
	smtpAddress, _ := smtpServer.start(t)
	imapAddress, _ := imapServer.start(t)
	sender := NewTitan(smtpAddress, simulatedMailboxUser, simulatedMailboxPass, TitanOptions{
		Timeout:     5 * time.Second,
		IMAPAddress: imapAddress,
		TLS:         &tls.Config{RootCAs: roots, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12},
	})
	return smtpServer, sender
}

func sentMailboxListing() []simulatedFolder {
	return []simulatedFolder{{name: "Sent", attributes: `\Sent`}}
}

func TestTitanSendCopyAppendsExactMessageToAdvertisedSentFolder(t *testing.T) {
	imapServer := &simulatedIMAP{folders: []simulatedFolder{
		{name: "Inbox", attributes: `\HasNoChildren`},
		{name: "Sent", attributes: `\HasNoChildren`},
		{name: "Gesendet", attributes: `\HasNoChildren \Sent`},
	}}
	smtpServer, sender := copyServers(t, imapServer)

	result := sender.Send(context.Background(), Message{Recipient: "recipient@example.test", Subject: "Candidate subject", HTML: "<p>Héllo body</p>"})
	if result.Kind != OutcomeSubmitted {
		t.Fatalf("result = %+v, want submitted", result)
	}
	if result.SaveCopy == nil {
		t.Fatal("Titan cannot save Sent copies natively, so it must return the copy hook")
	}
	acceptedAt := time.Date(2026, 10, 23, 5, 21, 18, 0, time.UTC)
	if err := result.SaveCopy(context.Background(), acceptedAt); err != nil {
		t.Fatalf("save copy: %v", err)
	}

	_, logins, appends, appended := imapServer.snapshot()
	if appends != 1 || len(appended) != 1 {
		t.Fatalf("append attempts = %d, want exactly one", appends)
	}
	if len(logins) != 1 || logins[0] != simulatedMailboxUser {
		t.Fatalf("IMAP logins = %v, want the bound mailbox", logins)
	}
	saved := appended[0]
	if saved.mailbox != "Gesendet" {
		t.Fatalf("appended to %q, want the advertised special-use folder", saved.mailbox)
	}
	if !strings.Contains(saved.command, `\Seen`) {
		t.Fatalf("append flags = %q, want the copy marked read", saved.command)
	}
	if !saved.date.Equal(acceptedAt) {
		t.Fatalf("internal date = %s, want the acceptance time %s", saved.date, acceptedAt)
	}
	if count := smtpServer.countCommands("MAIL"); count != 1 {
		t.Fatalf("MAIL count = %d; saving a copy must not open another SMTP transaction", count)
	}

	raw := string(saved.raw)
	transmitted := smtpServer.sentMessage()
	if strings.TrimRight(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") != strings.TrimRight(transmitted, "\n") {
		t.Fatalf("saved copy differs from the transmitted message:\nsaved:\n%s\ntransmitted:\n%s", raw, transmitted)
	}
	parsed, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("saved copy is not a MIME message: %v", err)
	}
	if got := parsed.Header.Get("To"); !strings.Contains(got, "recipient@example.test") {
		t.Fatalf("To = %q, want the submitted recipient", got)
	}
	if got := parsed.Header.Get("Subject"); got != "Candidate subject" {
		t.Fatalf("Subject = %q, want the submitted subject", got)
	}
	if !strings.Contains(raw, "text/html") {
		t.Fatal("saved copy carries no HTML part")
	}
}

func TestTitanSendCopyFallsBackToExactSentFolder(t *testing.T) {
	imapServer := &simulatedIMAP{folders: []simulatedFolder{
		{name: "Inbox", attributes: `\HasNoChildren`},
		{name: "Archive", attributes: `\HasNoChildren`},
		{name: "Sent", attributes: `\HasNoChildren`},
	}}
	_, sender := copyServers(t, imapServer)

	result := sender.Send(context.Background(), Message{Recipient: "recipient@example.test", Subject: "s", HTML: "<p>b</p>"})
	if result.Kind != OutcomeSubmitted || result.SaveCopy == nil {
		t.Fatalf("result = %+v, want a submitted result with a copy hook", result)
	}
	if err := result.SaveCopy(context.Background(), time.Now()); err != nil {
		t.Fatalf("save copy: %v", err)
	}
	_, _, appends, appended := imapServer.snapshot()
	if appends != 1 || appended[0].mailbox != "Sent" {
		t.Fatalf("appends = %d in %+v, want the exact Sent fallback", appends, appended)
	}
}

func TestTitanSendCopyRefusesMissingOrAmbiguousSentFolder(t *testing.T) {
	for _, tc := range []struct {
		name    string
		folders []simulatedFolder
	}{
		{name: "missing", folders: []simulatedFolder{{name: "Inbox", attributes: `\HasNoChildren`}}},
		{name: "ambiguous", folders: []simulatedFolder{
			{name: "Sent", attributes: `\Sent`},
			{name: "Gesendet", attributes: `\Sent`},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			imapServer := &simulatedIMAP{folders: tc.folders}
			_, sender := copyServers(t, imapServer)

			result := sender.Send(context.Background(), Message{Recipient: "recipient@example.test", Subject: "s", HTML: "<p>b</p>"})
			if result.SaveCopy == nil {
				t.Fatal("a submitted result must carry the copy hook")
			}
			if err := result.SaveCopy(context.Background(), time.Now()); !errors.Is(err, ErrTitanSentCopy) {
				t.Fatalf("err = %v, want an unconfirmed copy", err)
			}
			_, _, appends, _ := imapServer.snapshot()
			if appends != 0 {
				t.Fatalf("appends = %d; an unusable destination must not receive the copy", appends)
			}
			for _, forbidden := range []string{"CREATE", "SELECT", "EXPUNGE", "CLOSE"} {
				if imapServer.hasCommand(forbidden) {
					t.Fatalf("copy saving issued %s; it must not create, select or expunge a mailbox", forbidden)
				}
			}
		})
	}
}

func TestTitanSendCopyFailuresAreUnconfirmedAndSanitized(t *testing.T) {
	for _, tc := range []struct {
		name      string
		imap      *simulatedIMAP
		timeout   time.Duration
		untrusted bool
		cancel    bool
	}{
		{name: "authentication rejected", imap: &simulatedIMAP{folders: sentMailboxListing(), loginCode: 1}},
		{name: "discovery refused", imap: &simulatedIMAP{folders: sentMailboxListing(), listCode: 1}},
		{name: "append refused", imap: &simulatedIMAP{folders: sentMailboxListing(), appendCode: 1}},
		{name: "stalled greeting", imap: &simulatedIMAP{dropGreeting: true}, timeout: 150 * time.Millisecond},
		{name: "operation cancelled", imap: &simulatedIMAP{folders: sentMailboxListing()}, cancel: true},
		{name: "untrusted certificate", imap: &simulatedIMAP{folders: sentMailboxListing()}, untrusted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The SMTP endpoint always shares the trusted certificate; only the
			// mailbox endpoint is untrusted in that case.
			smtpCertificate, roots := selfSignedCertificate(t)
			imapCertificate := smtpCertificate
			if tc.untrusted {
				imapCertificate, _ = selfSignedCertificate(t)
			}
			tc.imap.certificate = &imapCertificate
			smtpServer := &simulatedSMTP{certificate: &smtpCertificate}
			smtpAddress, _ := smtpServer.start(t)
			imapAddress, _ := tc.imap.start(t)
			timeout := tc.timeout
			if timeout == 0 {
				timeout = 5 * time.Second
			}
			sender := NewTitan(smtpAddress, simulatedMailboxUser, simulatedMailboxPass, TitanOptions{
				Timeout:     timeout,
				IMAPAddress: imapAddress,
				TLS:         &tls.Config{RootCAs: roots, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12},
			})
			assertUnconfirmedCopy(t, sender, tc.imap, tc.cancel)
		})
	}
}

// assertUnconfirmedCopy sends one message and proves its copy fails safely: the
// submission stays accepted, the error names only a stage, and no credential or
// message content is exposed.
func assertUnconfirmedCopy(t *testing.T, sender *Titan, imapServer *simulatedIMAP, cancel bool) {
	t.Helper()
	result := sender.Send(context.Background(), Message{Recipient: "recipient@example.test", Subject: "Candidate subject", HTML: "<p>secret body</p>"})
	if result.Kind != OutcomeSubmitted || result.SaveCopy == nil {
		t.Fatalf("result = %+v, want a submitted result with a copy hook", result)
	}
	ctx := context.Background()
	if cancel {
		cancelled, stop := context.WithCancel(ctx)
		stop()
		ctx = cancelled
	}
	err := result.SaveCopy(ctx, time.Now())
	if !errors.Is(err, ErrTitanSentCopy) {
		t.Fatalf("err = %v, want an unconfirmed copy", err)
	}
	for _, secret := range []string{simulatedMailboxPass, "secret body", "simulated", "refused", "rejected", simulatedMailboxUser} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("copy error %q leaks %q", err, secret)
		}
	}
	_, _, appends, _ := imapServer.snapshot()
	if appends > 1 {
		t.Fatalf("append attempts = %d, want at most one", appends)
	}
}

func TestTitanSendCopyHandlesLostAndUncleanConfirmations(t *testing.T) {
	t.Run("lost append response", func(t *testing.T) {
		imapServer := &simulatedIMAP{folders: sentMailboxListing(), dropAfterAppend: true}
		smtpServer, sender := copyServers(t, imapServer)

		result := sender.Send(context.Background(), Message{Recipient: "recipient@example.test", Subject: "s", HTML: "<p>b</p>"})
		if err := result.SaveCopy(context.Background(), time.Now()); !errors.Is(err, ErrTitanSentCopy) {
			t.Fatalf("err = %v, want an unconfirmed copy", err)
		}
		_, _, appends, _ := imapServer.snapshot()
		if appends != 1 {
			t.Fatalf("append attempts = %d, want one without a retry", appends)
		}
		if count := smtpServer.countCommands("MAIL"); count != 1 {
			t.Fatalf("MAIL count = %d; an unconfirmed copy must never resend the email", count)
		}
	})

	t.Run("incomplete logout after confirmation", func(t *testing.T) {
		imapServer := &simulatedIMAP{folders: sentMailboxListing(), logoutIncomplete: true}
		_, sender := copyServers(t, imapServer)

		result := sender.Send(context.Background(), Message{Recipient: "recipient@example.test", Subject: "s", HTML: "<p>b</p>"})
		if err := result.SaveCopy(context.Background(), time.Now()); err != nil {
			t.Fatalf("confirmed copy reported a cleanup failure: %v", err)
		}
		_, _, appends, _ := imapServer.snapshot()
		if appends != 1 {
			t.Fatalf("append attempts = %d, want one", appends)
		}
	})
}

func TestTitanSendRejectionReturnsNoCopyHook(t *testing.T) {
	imapServer := &simulatedIMAP{folders: sentMailboxListing()}
	certificate, roots := selfSignedCertificate(t)
	imapServer.certificate = &certificate
	smtpServer := &simulatedSMTP{certificate: &certificate, rcptCode: 550}
	smtpAddress, _ := smtpServer.start(t)
	imapAddress, _ := imapServer.start(t)
	sender := NewTitan(smtpAddress, simulatedMailboxUser, simulatedMailboxPass, TitanOptions{
		Timeout:     5 * time.Second,
		IMAPAddress: imapAddress,
		TLS:         &tls.Config{RootCAs: roots, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12},
	})

	result := sender.Send(context.Background(), Message{Recipient: "recipient@example.test", Subject: "s", HTML: "<p>b</p>"})
	if result.Kind != OutcomeDefiniteFailure {
		t.Fatalf("result = %+v, want a definite failure", result)
	}
	if result.SaveCopy != nil {
		t.Fatal("a rejected submission must never produce a Sent copy")
	}
	if commands, _, _, _ := imapServer.snapshot(); len(commands) != 0 {
		t.Fatalf("a rejected submission opened a mailbox session: %v", commands)
	}
}

func TestValidateTitanCreatesNoSentCopy(t *testing.T) {
	certificate, roots := selfSignedCertificate(t)
	imapServer := &simulatedIMAP{folders: sentMailboxListing(), certificate: &certificate}
	smtpServer := &simulatedSMTP{certificate: &certificate}
	smtpAddress, _ := smtpServer.start(t)
	imapAddress, _ := imapServer.start(t)
	if err := ValidateTitan(context.Background(), smtpAddress, simulatedMailboxUser, simulatedMailboxPass, TitanOptions{
		Timeout:     5 * time.Second,
		IMAPAddress: imapAddress,
		TLS:         &tls.Config{RootCAs: roots, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12},
	}); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if commands, _, appends, _ := imapServer.snapshot(); len(commands) != 0 || appends != 0 {
		t.Fatalf("connection validation touched the mailbox: %v", commands)
	}
}

// TestTitanSentCopiesStayWithinTheirMailbox proves each copy is saved with the
// credentials and transport of the connection that submitted the message.
func TestTitanSentCopiesStayWithinTheirMailbox(t *testing.T) {
	owners := []string{"first@example.test", "second@example.test"}
	mailboxes := []*simulatedIMAP{{folders: sentMailboxListing()}, {folders: sentMailboxListing()}}
	senders := make([]*Titan, len(owners))
	for index, owner := range owners {
		certificate, roots := selfSignedCertificate(t)
		mailboxes[index].certificate = &certificate
		smtpServer := &simulatedSMTP{certificate: &certificate}
		smtpAddress, _ := smtpServer.start(t)
		imapAddress, _ := mailboxes[index].start(t)
		senders[index] = NewTitan(smtpAddress, owner, simulatedMailboxPass, TitanOptions{
			Timeout:     5 * time.Second,
			IMAPAddress: imapAddress,
			TLS:         &tls.Config{RootCAs: roots, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12},
		})
	}
	for index, sender := range senders {
		result := sender.Send(context.Background(), Message{Recipient: "recipient@example.test", Subject: "s", HTML: "<p>b</p>"})
		if result.SaveCopy == nil {
			t.Fatalf("%s: submitted result must carry the copy hook", owners[index])
		}
		if err := result.SaveCopy(context.Background(), time.Now()); err != nil {
			t.Fatalf("%s: save copy: %v", owners[index], err)
		}
	}
	// Each mailbox may only hold its own operator's copy, so an isolated append
	// count and login per server proves no copy crossed mailboxes.
	for index, owner := range owners {
		_, logins, appends, _ := mailboxes[index].snapshot()
		if appends != 1 || len(logins) != 1 || logins[0] != owner {
			t.Fatalf("%s: appends=%d logins=%v, want one copy as %s", owner, appends, logins, owner)
		}
	}
}
