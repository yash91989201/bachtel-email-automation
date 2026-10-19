package sending

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"
)

// Message is the immutable content saved with the submission intent.
type Message struct {
	Recipient string
	Subject   string
	HTML      string
}

type OutcomeKind string

const (
	OutcomeSubmitted       OutcomeKind = "submitted"
	OutcomeDefiniteFailure OutcomeKind = "definite_failure"
	OutcomeSafeRetry       OutcomeKind = "safe_retry"
	OutcomeAuthFailure     OutcomeKind = "auth_failure"
	OutcomeUncertain       OutcomeKind = "uncertain"
)

// SendResult contains only application-owned reasons, never provider diagnostics.
// Submitted means accepted for processing, not delivered.
type SendResult struct {
	Kind               OutcomeKind
	RetryAfter         time.Time
	Error              string
	PauseMailbox       bool
	RequiresCorrection bool
}

type Sender interface {
	Send(context.Context, Message) SendResult
}

// PreparingSender resolves potentially blocking authorization before the dispatcher
// grants a paced start. The returned sender must not refresh authorization.
type PreparingSender interface {
	Prepare(context.Context) Sender
}

type preparedGraph struct {
	graph   *Graph
	token   string
	failure *SendResult
}

func (p preparedGraph) Send(ctx context.Context, message Message) SendResult {
	if p.failure != nil {
		return *p.failure
	}
	return p.graph.send(ctx, message, p.token)
}

type TokenSource interface {
	AccessToken(context.Context) (string, error)
}

type GraphOptions struct {
	HTTPClient *http.Client
	BaseURL    string
	Timeout    time.Duration
}

// Graph submits one HTML message per request. It never retries a send POST.
type Graph struct {
	tokens  TokenSource
	client  *http.Client
	url     string
	timeout time.Duration
}

func NewGraph(tokens TokenSource, options GraphOptions) *Graph {
	client := http.Client{}
	if options.HTTPClient != nil {
		client = *options.HTTPClient
	}
	// Keep the caller's transport, but prevent redirects from replaying a POST
	// or forwarding its bearer token to another endpoint.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	timeout := options.Timeout
	if timeout <= 0 || timeout > 30*time.Second {
		timeout = 20 * time.Second
	}
	if client.Timeout == 0 || client.Timeout > timeout {
		client.Timeout = timeout
	}
	base := options.BaseURL
	if base == "" {
		base = "https://graph.microsoft.com/v1.0"
	}
	return &Graph{tokens: tokens, client: &client, url: strings.TrimRight(base, "/") + "/me/sendMail", timeout: timeout}
}

// Prepare performs authorization without holding the dispatcher start barrier.
func (g *Graph) Prepare(ctx context.Context) Sender {
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()
	if g.tokens != nil {
		token, err := g.tokens.AccessToken(ctx)
		if err == nil && strings.TrimSpace(token) != "" {
			return preparedGraph{graph: g, token: token}
		}
	}
	failure := authorizationFailure()
	return preparedGraph{failure: &failure}
}

func (g *Graph) Send(ctx context.Context, message Message) SendResult {
	return g.Prepare(ctx).Send(ctx, message)
}

func (g *Graph) send(ctx context.Context, message Message, token string) SendResult {
	address, err := mail.ParseAddress(message.Recipient)
	if err != nil || address.Address != message.Recipient || strings.ContainsAny(message.Recipient, "\r\n") {
		return SendResult{Kind: OutcomeDefiniteFailure, Error: "The recipient address is invalid.", RequiresCorrection: true}
	}
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()
	// Omitting saveToSentItems retains Graph's default Sent Items behavior.
	payload := struct {
		Message struct {
			Subject string `json:"subject"`
			Body    struct {
				ContentType string `json:"contentType"`
				Content     string `json:"content"`
			} `json:"body"`
			ToRecipients []struct {
				EmailAddress struct {
					Address string `json:"address"`
				} `json:"emailAddress"`
			} `json:"toRecipients"`
		} `json:"message"`
	}{}
	payload.Message.Subject = message.Subject
	payload.Message.Body.ContentType = "HTML"
	payload.Message.Body.Content = message.HTML
	payload.Message.ToRecipients = make([]struct {
		EmailAddress struct {
			Address string `json:"address"`
		} `json:"emailAddress"`
	}, 1)
	payload.Message.ToRecipients[0].EmailAddress.Address = message.Recipient
	body, err := json.Marshal(payload)
	if err != nil {
		return SendResult{Kind: OutcomeDefiniteFailure, Error: "The message could not be prepared for submission.", PauseMailbox: true}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, g.url, bytes.NewReader(body))
	if err != nil {
		return SendResult{Kind: OutcomeDefiniteFailure, Error: "The mail service endpoint is invalid.", PauseMailbox: true}
	}
	// A nil GetBody also prevents net/http's transport from replaying this
	// nonempty POST after an error on a reused connection.
	request.GetBody = nil
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := g.client.Do(request)
	if err != nil {
		// Only DNS and dial failures prove that this request never reached the
		// provider. Timeouts and disconnects otherwise remain uncertain.
		var op *net.OpError
		var dns *net.DNSError
		if errors.As(err, &op) && op.Op == "dial" || errors.As(err, &dns) {
			return SendResult{Kind: OutcomeSafeRetry, Error: "The mail service could not be reached before submission."}
		}
		return SendResult{Kind: OutcomeUncertain, Error: "The submission outcome is unknown; review is required.", PauseMailbox: true}
	}
	defer func() {
		// Once Graph returns 202, cleanup failure cannot undo its acceptance.
		if err := response.Body.Close(); err != nil {
			return
		}
	}()
	switch response.StatusCode {
	case http.StatusAccepted:
		return SendResult{Kind: OutcomeSubmitted}
	case http.StatusUnauthorized, http.StatusForbidden:
		return authorizationFailure()
	case http.StatusTooManyRequests:
		return SendResult{Kind: OutcomeSafeRetry, RetryAfter: parseRetryAfter(response.Header.Get("Retry-After"), time.Now()), Error: "Microsoft has temporarily throttled submissions."}
	case http.StatusRequestTimeout:
		return SendResult{Kind: OutcomeUncertain, Error: "The submission outcome is unknown; review is required.", PauseMailbox: true}
	}
	if response.StatusCode == http.StatusBadRequest || response.StatusCode == http.StatusUnprocessableEntity {
		var diagnostic struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if json.NewDecoder(io.LimitReader(response.Body, 64*1024)).Decode(&diagnostic) == nil {
			switch strings.ToLower(diagnostic.Error.Code) {
			case "errorinvalidrecipients", "errorinvalidrecipient", "errorrecipientnotfound":
				return SendResult{Kind: OutcomeDefiniteFailure, Error: "Microsoft rejected the recipient address.", RequiresCorrection: true}
			}
		}
	}
	if response.StatusCode >= 400 && response.StatusCode < 500 {
		return SendResult{Kind: OutcomeDefiniteFailure, Error: "Microsoft rejected the submission; operator action is required.", PauseMailbox: true}
	}
	return SendResult{Kind: OutcomeUncertain, Error: "The submission outcome is unknown; review is required.", PauseMailbox: true}
}

func authorizationFailure() SendResult {
	return SendResult{Kind: OutcomeAuthFailure, Error: "Microsoft authorization requires operator attention.", PauseMailbox: true}
}

func parseRetryAfter(value string, now time.Time) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	// Parse decimal seconds without allowing overflow into a negative delay.
	if seconds, err := strconv.ParseUint(value, 10, 64); err == nil {
		if seconds > uint64((1<<63-1)/int64(time.Second)) {
			return time.Time{}
		}
		return now.Add(time.Duration(seconds) * time.Second)
	}
	if at, err := http.ParseTime(value); err == nil && at.After(now) {
		return at
	}
	return time.Time{}
}
