package sending

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

// ErrTitanSentCopy reports that the Sent copy of an accepted message could not
// be confirmed. It never claims the copy is absent: a lost response after a
// complete APPEND is indistinguishable from a failure. Its messages carry fixed
// stage names only, so provider responses, credentials and message content
// never reach operator-visible text or logs.
var ErrTitanSentCopy = errors.New("the Sent copy could not be confirmed")

// sentCopy returns the post-submission hook for one accepted payload. The
// captured bytes are the exact MIME already transmitted, so the copy preserves
// the submitted recipient, subject and HTML without rebuilding the message.
func (t *Titan) sentCopy(payload []byte) SaveCopy {
	return func(ctx context.Context, acceptedAt time.Time) error {
		return t.saveSentCopy(ctx, payload, acceptedAt)
	}
}

// saveSentCopy appends the accepted message once to the mailbox's existing Sent
// folder over a fresh, certificate-validated IMAP connection. It never retries,
// never creates a folder and never writes another mailbox: the connection is
// the same one that submitted the message, so the copy cannot be redirected.
func (t *Titan) saveSentCopy(ctx context.Context, payload []byte, acceptedAt time.Time) error {
	host, _, err := net.SplitHostPort(t.imapAddress)
	if err != nil {
		// An endpoint that cannot be parsed is a deployment mistake, not a
		// provider failure. It still only warns on the accepted submission.
		return copyStage("endpoint")
	}
	config := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if t.tls != nil {
		config = t.tls.Clone()
		if config.ServerName == "" {
			config.ServerName = host
		}
	}
	deadline := time.Now().Add(t.timeout)
	dialCtx, cancelDial := context.WithTimeout(ctx, t.timeout)
	conn, err := (&tls.Dialer{NetDialer: &net.Dialer{Timeout: t.timeout}, Config: config}).
		DialContext(dialCtx, "tcp", t.imapAddress)
	cancelDial()
	if err != nil {
		return copyStage("connection")
	}
	// Cancellation must unblock a pending read; the absolute deadline covers
	// everything from the greeting to logout.
	stop := context.AfterFunc(ctx, func() { closeConnection(conn) })
	defer stop()
	defer closeConnection(conn)
	if err := conn.SetDeadline(deadline); err != nil {
		return copyStage("connection")
	}
	session, err := client.New(conn)
	if err != nil {
		return copyStage("greeting")
	}
	// The library logs protocol errors with provider text by default; this
	// operation reports only its own fixed stages and never a provider response.
	session.ErrorLog = log.New(io.Discard, "", 0)
	// The library rewrites the connection deadline for every command, so each
	// stage receives only what is left of the owning operation's budget.
	bound := func() {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			remaining = time.Nanosecond
		}
		session.Timeout = remaining
	}
	bound()
	if err := session.Login(t.username, t.password); err != nil {
		return copyStage("authentication")
	}
	folder, err := sentFolder(session, bound)
	if err != nil {
		return err
	}
	bound()
	if err := session.Append(folder, []string{imap.SeenFlag}, acceptedAt.UTC(), bytes.NewReader(payload)); err != nil {
		// Exactly one append attempt: a lost response is an unconfirmed copy.
		return copyStage("append")
	}
	// The copy is confirmed. Session cleanup cannot invalidate it, so a failed
	// logout is not reported as a copy failure.
	session.Logout() //nolint:errcheck // a confirmed copy is not undone by logout failure
	return nil
}

// sentFolder resolves the mailbox's existing Sent folder. The advertised
// special-use attribute wins, because the folder name is provider-specific; the
// exact "Sent" name is only the documented fallback. An ambiguous or missing
// destination is a warning, never a guess: no folder is created and no other
// mailbox is used.
func sentFolder(session *client.Client, bound func()) (string, error) {
	bound()
	listed := make(chan *imap.MailboxInfo, 8)
	done := make(chan error, 1)
	go func() {
		// List closes the channel, which ends the drain below even on error.
		done <- session.List("", "*", listed)
	}()
	var boxes []*imap.MailboxInfo
	for info := range listed {
		boxes = append(boxes, info)
	}
	if err := <-done; err != nil {
		return "", copyStage("discovery")
	}
	var advertised []string
	for _, info := range boxes {
		for _, attribute := range info.Attributes {
			if strings.EqualFold(attribute, "\\Sent") && info.Name != "" {
				advertised = append(advertised, info.Name)
			}
		}
	}
	if len(advertised) > 1 {
		return "", copyStage("folder")
	}
	if len(advertised) == 1 {
		return advertised[0], nil
	}
	for _, info := range boxes {
		if info.Name == "Sent" {
			return info.Name, nil
		}
	}
	return "", copyStage("folder")
}

func copyStage(stage string) error {
	return fmt.Errorf("%w (%s)", ErrTitanSentCopy, stage)
}
