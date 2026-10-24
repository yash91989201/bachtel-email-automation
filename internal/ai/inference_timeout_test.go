package ai_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/yash91989201/bachtel-email-automation/internal/ai"
)

func TestInferenceTimeoutIsSeparateFromValidationTimeout(t *testing.T) {
	const responseDelay = 200 * time.Millisecond
	const probeText = `{"values":[{"id":"probe","value":"probe@example.test"}]}`
	fake := newFakeEndpoint(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("request path = %q, want the pinned Responses path", r.URL.Path)
		}
		select {
		case <-time.After(responseDelay):
			respondProbe(t, ai.FormatResponses, probeText)(w, r, nil)
		case <-r.Context().Done():
		}
	})
	options := ai.ValidationOptions{
		Timeout:          50 * time.Millisecond,
		InferenceTimeout: 2 * time.Second,
		MaxResponseBytes: 512 << 10,
		AllowPrivate:     true,
	}
	originalOptions := options
	app := newApp(t)
	owner := provision(t, app, "timeout-boundaries@example.test")
	store := ai.NewStore(app, func() ([]byte, error) { return testKey, nil })
	store.SetValidationOptions(options)
	proposal := ai.Proposal{Config: customConfig(fake.server.URL, "slow-model", ai.FormatResponses), APIKey: "test-key"}
	if _, err := store.StageValidation(context.Background(), owner, proposal); !errors.Is(err, ai.ErrEndpointTimeout) {
		t.Fatalf("StageValidation error = %v, want validation timeout", err)
	}
	if fake.requests.Load() != 1 {
		t.Fatalf("validation requests = %d; want one pinned-format request", fake.requests.Load())
	}

	text, err := ai.Complete(context.Background(), options, proposal.Config, proposal.APIKey, ai.InferenceRequest{
		System: "test system", User: "test payload", MaxTokens: 512,
	})
	if err != nil {
		t.Fatalf("Complete with inference timeout: %v", err)
	}
	if text != probeText {
		t.Fatalf("completion text = %q, want delayed response", text)
	}
	if fake.requests.Load() != 2 {
		t.Fatalf("total requests = %d; want one validation and one inference request", fake.requests.Load())
	}
	if options != originalOptions {
		t.Fatalf("Complete mutated caller options: before=%+v after=%+v", originalOptions, options)
	}
}

func TestInferenceTimeoutZeroUsesValidationTimeout(t *testing.T) {
	fake := newFakeEndpoint(t, func(_ http.ResponseWriter, r *http.Request, _ map[string]any) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("request path = %q, want the pinned Responses path", r.URL.Path)
		}
		<-r.Context().Done()
	})
	options := ai.ValidationOptions{
		Timeout:          50 * time.Millisecond,
		InferenceTimeout: 0,
		MaxResponseBytes: 512 << 10,
		AllowPrivate:     true,
	}
	started := time.Now()
	_, err := ai.Complete(context.Background(), options, customConfig(fake.server.URL, "slow-model", ai.FormatResponses), "test-key", ai.InferenceRequest{
		System: "test system", User: "test payload", MaxTokens: 512,
	})
	if !errors.Is(err, ai.ErrEndpointTimeout) {
		t.Fatalf("Complete error = %v, want validation timeout when inference timeout is zero", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("zero inference timeout took %s; want the 50ms validation bound", elapsed)
	}
	if fake.requests.Load() != 1 {
		t.Fatalf("requests = %d; want one request without retries or fallback", fake.requests.Load())
	}
}

func TestInferenceTimeoutHonorsCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	requestCancelled := make(chan struct{})
	fake := newFakeEndpoint(t, func(_ http.ResponseWriter, r *http.Request, _ map[string]any) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("request path = %q, want the pinned Responses path", r.URL.Path)
		}
		cancel()
		<-r.Context().Done()
		close(requestCancelled)
	})
	options := ai.ValidationOptions{
		Timeout:          50 * time.Millisecond,
		InferenceTimeout: 2 * time.Second,
		MaxResponseBytes: 512 << 10,
		AllowPrivate:     true,
	}
	_, err := ai.Complete(ctx, options, customConfig(fake.server.URL, "slow-model", ai.FormatResponses), "test-key", ai.InferenceRequest{
		System: "test system", User: "test payload", MaxTokens: 512,
	})
	if !errors.Is(err, ai.ErrEndpointTimeout) || !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("Complete error = %v, caller context = %v; want endpoint timeout with caller cancellation", err, ctx.Err())
	}
	select {
	case <-requestCancelled:
	case <-time.After(time.Second):
		t.Fatal("caller cancellation did not cancel the provider request")
	}
	if fake.requests.Load() != 1 {
		t.Fatalf("requests = %d; want one request without retries or fallback", fake.requests.Load())
	}
}

func TestDefaultInferenceTimeoutsAreBoundedSeparately(t *testing.T) {
	options := ai.DefaultValidationOptions()
	if options.Timeout != 30*time.Second || options.InferenceTimeout != 2*time.Minute {
		t.Fatalf("production timeouts = validation:%s inference:%s; want 30s and 2m", options.Timeout, options.InferenceTimeout)
	}
}
