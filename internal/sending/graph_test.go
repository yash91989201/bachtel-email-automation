package sending

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type graphTokenSource struct {
	token string
	err   error
}

func (s graphTokenSource) AccessToken(context.Context) (string, error) { return s.token, s.err }

func graphMessage() Message {
	return Message{Recipient: "recipient@example.com", Subject: "Personal subject", HTML: "<p>Hello &amp; welcome</p>"}
}

func TestGraphHTMLSubmission(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.URL.Path != "/v1.0/me/sendMail" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer private-token" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("missing request headers")
		}
		for name := range r.Header {
			if strings.Contains(strings.ToLower(name), "idempotency") {
				t.Error("idempotency header present")
			}
		}
		var payload map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if len(payload) != 1 || payload["message"] == nil {
			t.Errorf("unexpected envelope fields: %v", payload)
		}
		var message struct {
			Subject      string                                            `json:"subject"`
			Body         struct{ ContentType, Content string }             `json:"body"`
			ToRecipients []struct{ EmailAddress struct{ Address string } } `json:"toRecipients"`
			Attachments  json.RawMessage                                   `json:"attachments"`
		}
		if err := json.Unmarshal(payload["message"], &message); err != nil {
			t.Fatal(err)
		}
		want := graphMessage()
		if message.Subject != want.Subject || message.Body.ContentType != "HTML" || message.Body.Content != want.HTML || len(message.ToRecipients) != 1 || message.ToRecipients[0].EmailAddress.Address != want.Recipient || message.Attachments != nil {
			t.Errorf("unexpected message: %+v", message)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	result := NewGraph(graphTokenSource{token: "private-token"}, GraphOptions{BaseURL: server.URL + "/v1.0"}).Send(context.Background(), graphMessage())
	if result.Kind != OutcomeSubmitted || result.Error != "" || result.PauseMailbox || calls != 1 {
		t.Fatalf("result = %+v; calls = %d", result, calls)
	}
}

func TestGraphClassifiesResponsesWithoutLeakingDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		kind   OutcomeKind
		pause  bool
	}{
		{"recipient", 400, `{"error":{"code":"ErrorInvalidRecipients","message":"private-token recipient@example.com private-body"}}`, OutcomeDefiniteFailure, false},
		{"recipient422", 422, `{"error":{"code":"ErrorRecipientNotFound"}}`, OutcomeDefiniteFailure, false},
		{"unauthorized", 401, "private-token", OutcomeAuthFailure, true},
		{"forbidden", 403, "private-token", OutcomeAuthFailure, true},
		{"throttled", 429, "private-token", OutcomeSafeRetry, false},
		{"genericBadRequest", 400, `{"error":{"code":"Unknown","message":"private-token"}}`, OutcomeDefiniteFailure, true},
		{"invalidJSON", 400, "private-token", OutcomeDefiniteFailure, true},
		{"serviceError", 500, `{"error":{"code":"ErrorInvalidRecipients"}}`, OutcomeUncertain, true},
		{"unavailable", 503, "private-token", OutcomeUncertain, true},
		{"requestTimeout", 408, "private-token", OutcomeUncertain, true},
		{"unexpectedSuccess", 200, "private-token", OutcomeUncertain, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(test.status)
				if _, err := io.WriteString(w, test.body); err != nil {
					t.Errorf("write response body: %v", err)
				}
			}))
			defer server.Close()
			result := NewGraph(graphTokenSource{token: "private-token"}, GraphOptions{BaseURL: server.URL}).Send(context.Background(), graphMessage())
			if result.Kind != test.kind || result.PauseMailbox != test.pause || calls != 1 {
				t.Fatalf("result = %+v, calls = %d", result, calls)
			}
			if result.RequiresCorrection != strings.HasPrefix(test.name, "recipient") {
				t.Fatalf("unexpected correction requirement: %+v", result)
			}
			for _, secret := range []string{"private-token", "recipient@example.com", "private-body", "ErrorInvalidRecipients"} {
				if strings.Contains(result.Error, secret) {
					t.Fatalf("diagnostic leaked: %+v", result)
				}
			}
		})
	}
}

func TestGraphNoRedirectReplay(t *testing.T) {
	var destinationCalls atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { destinationCalls.Add(1); w.WriteHeader(202) }))
	defer destination.Close()
	for _, code := range []int{301, 302, 303, 307, 308} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", destination.URL)
			w.WriteHeader(code)
		}))
		result := NewGraph(graphTokenSource{token: "private-token"}, GraphOptions{BaseURL: server.URL, HTTPClient: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
			t.Error("caller redirect handler should be replaced")
			return nil
		}}}).Send(context.Background(), graphMessage())
		server.Close()
		if result.Kind != OutcomeUncertain || !result.PauseMailbox {
			t.Errorf("redirect %d: %+v", code, result)
		}
	}
	if destinationCalls.Load() != 0 {
		t.Fatal("redirect replayed send")
	}
}

func TestGraphTimeoutAndDisconnectAreUncertain(t *testing.T) {
	for _, disconnect := range []bool{false, true} {
		t.Run(map[bool]string{false: "timeout", true: "disconnect"}[disconnect], func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if _, err := io.Copy(io.Discard, r.Body); err != nil {
					t.Errorf("read request body: %v", err)
				}
				if disconnect {
					hijacker, ok := w.(http.Hijacker)
					if !ok {
						t.Error("test server does not support hijacking")
						return
					}
					conn, _, err := hijacker.Hijack()
					if err != nil {
						t.Errorf("hijack test connection: %v", err)
						return
					}
					if err := conn.Close(); err != nil {
						t.Errorf("close test connection: %v", err)
					}
					return
				}
				<-r.Context().Done()
			}))
			defer server.Close()
			result := NewGraph(graphTokenSource{token: "secret"}, GraphOptions{BaseURL: server.URL, Timeout: 30 * time.Millisecond}).Send(context.Background(), graphMessage())
			if result.Kind != OutcomeUncertain || !result.PauseMailbox || calls.Load() != 1 {
				t.Fatalf("result = %+v, calls = %d", result, calls.Load())
			}
		})
	}
}

type graphRoundTripFunc func(*http.Request) (*http.Response, error)

func (f graphRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGraphProvenPreSubmissionAndTransportFailures(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		kind OutcomeKind
	}{
		{"dial", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("private-host")}, OutcomeSafeRetry},
		{"dns", &net.DNSError{Name: "private-host", Err: "private-token"}, OutcomeSafeRetry},
		{"unknown", errors.New("private-token recipient@example.com"), OutcomeUncertain},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls int
			client := &http.Client{Transport: graphRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.GetBody != nil {
					t.Error("POST body can be replayed")
				}
				return nil, test.err
			})}
			result := NewGraph(graphTokenSource{token: "secret"}, GraphOptions{HTTPClient: client}).Send(context.Background(), graphMessage())
			if result.Kind != test.kind || calls != 1 || strings.Contains(result.Error, "private-") {
				t.Fatalf("result = %+v, calls = %d", result, calls)
			}
		})
	}
}

func TestGraphTokenFailureAndInvalidRecipientNeverSend(t *testing.T) {
	client := &http.Client{Transport: graphRoundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("unexpected HTTP request")
		return nil, errors.New("unexpected")
	})}
	for _, tokens := range []TokenSource{nil, graphTokenSource{err: errors.New("secret token")}, graphTokenSource{}} {
		result := NewGraph(tokens, GraphOptions{HTTPClient: client}).Send(context.Background(), graphMessage())
		if result.Kind != OutcomeAuthFailure || !result.PauseMailbox || strings.Contains(result.Error, "secret") {
			t.Fatalf("result = %+v", result)
		}
	}
	for _, address := range []string{"", "a@example.com,b@example.com", "Name <a@example.com>", "a@example.com\r\nBcc:x@example.com"} {
		message := graphMessage()
		message.Recipient = address
		if result := NewGraph(graphTokenSource{token: "secret"}, GraphOptions{HTTPClient: client}).Send(context.Background(), message); result.Kind != OutcomeDefiniteFailure || result.PauseMailbox {
			t.Fatalf("result = %+v", result)
		}
	}
}

func TestGraphRetryAfter(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for _, test := range []struct {
		value string
		want  time.Time
	}{
		{"120", now.Add(120 * time.Second)}, {"0", now}, {now.Add(time.Minute).Format(http.TimeFormat), now.Add(time.Minute)},
		{"", time.Time{}}, {"bad", time.Time{}}, {"-1", time.Time{}}, {"18446744073709551615", time.Time{}}, {now.Add(-time.Minute).Format(http.TimeFormat), time.Time{}},
	} {
		if got := parseRetryAfter(test.value, now); !got.Equal(test.want) {
			t.Errorf("%q = %v, want %v", test.value, got, test.want)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Retry-After", "120"); w.WriteHeader(429) }))
	defer server.Close()
	before := time.Now()
	result := NewGraph(graphTokenSource{token: "secret"}, GraphOptions{BaseURL: server.URL}).Send(context.Background(), graphMessage())
	if result.Kind != OutcomeSafeRetry || result.RetryAfter.Before(before.Add(120*time.Second)) || result.RetryAfter.After(time.Now().Add(120*time.Second)) {
		t.Fatalf("result = %+v", result)
	}
}

func TestGraphTimeoutIsBounded(t *testing.T) {
	client := &http.Client{Timeout: time.Hour}
	graph := NewGraph(graphTokenSource{}, GraphOptions{HTTPClient: client, Timeout: time.Hour})
	if graph.timeout > 30*time.Second || graph.client.Timeout > 30*time.Second {
		t.Fatal("unbounded timeout")
	}
	if client.Timeout != time.Hour || client.CheckRedirect != nil {
		t.Fatal("caller client was mutated")
	}
}

type closeErrorBody struct{}

func (closeErrorBody) Read([]byte) (int, error) { return 0, io.EOF }
func (closeErrorBody) Close() error             { return errors.New("simulated close failure") }

func TestGraph202RemainsSubmittedWhenResponseCloseFails(t *testing.T) {
	client := &http.Client{Transport: graphRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusAccepted, Body: closeErrorBody{}, Header: make(http.Header)}, nil
	})}
	got := NewGraph(graphTokenSource{token: "token"}, GraphOptions{HTTPClient: client}).Send(context.Background(), graphMessage())
	if got.Kind != OutcomeSubmitted {
		t.Fatalf("202 acceptance changed after body close failure: %+v", got)
	}
}

// TestGraphKeepsNativeSentItemsSavingWithoutCopyWork proves Microsoft 365 needs
// no application-managed copy: the request keeps Graph's default Sent Items
// saving, exactly one send POST happens, no copy hook is returned, and no
// follow-up request polls the mailbox.
func TestGraphKeepsNativeSentItemsSavingWithoutCopyWork(t *testing.T) {
	var calls atomic.Int32
	envelopes := make(chan map[string]json.RawMessage, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/v1.0/me/sendMail" {
			t.Errorf("request = %s %s, want one sendMail POST", r.Method, r.URL.Path)
		}
		var payload map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		envelopes <- payload
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	graph := NewGraph(graphTokenSource{token: "private-token"}, GraphOptions{BaseURL: server.URL + "/v1.0"})
	for _, tc := range []struct {
		name   string
		sender Sender
	}{
		{name: "direct", sender: graph},
		{name: "prepared", sender: graph.Prepare(context.Background())},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := calls.Load()
			result := tc.sender.Send(context.Background(), graphMessage())
			if result.Kind != OutcomeSubmitted || result.SaveCopy != nil {
				t.Fatalf("result = %+v, want accepted with native saving and no copy hook", result)
			}
			if calls.Load() != before+1 {
				t.Fatalf("requests = %d, want exactly one send", calls.Load()-before)
			}
			envelope := <-envelopes
			if len(envelope) != 1 || envelope["message"] == nil {
				t.Fatalf("envelope = %v; omitting saveToSentItems must keep saving enabled", envelope)
			}
		})
	}
}
