package sending_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yash91989201/bachtel-email-automation/internal/config"
	"github.com/yash91989201/bachtel-email-automation/internal/microsoft"
	"github.com/yash91989201/bachtel-email-automation/internal/operator"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
	"github.com/yash91989201/bachtel-email-automation/internal/web"
)

// This harness is opt-in and lives entirely in a test binary. Its provider and
// candidate addresses are simulated; there are no production debug routes.
// Write control.json atomically to hold/release sends, reject or make one address
// uncertain, restart the worker, or stop the harness. Restart keeps the same SQLite.
type browserControl struct {
	Hold      bool   `json:"hold"`
	Reject    string `json:"reject"`
	Uncertain string `json:"uncertain"`
	Restart   int    `json:"restart"`
	Done      bool   `json:"done"`
}

func readBrowserControl(path string) (browserControl, error) {
	var c browserControl
	data, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal(data, &c); err != nil {
		return c, err
	}
	return c, nil
}

type browserMailbox struct{ path string }

func (m browserMailbox) Send(ctx context.Context, message sending.Message) sending.SendResult {
	for {
		c, err := readBrowserControl(m.path)
		if err != nil {
			return sending.SendResult{Kind: sending.OutcomeUncertain, Error: "simulated control state unavailable", PauseMailbox: true}
		}
		if !c.Hold {
			break
		}
		select {
		case <-ctx.Done():
			return sending.SendResult{Kind: sending.OutcomeUncertain, Error: "simulated held request timed out", PauseMailbox: true}
		case <-time.After(20 * time.Millisecond):
		}
	}
	c, err := readBrowserControl(m.path)
	if err != nil {
		return sending.SendResult{Kind: sending.OutcomeUncertain, Error: "simulated control state unavailable", PauseMailbox: true}
	}
	if message.Recipient == c.Uncertain {
		return sending.SendResult{Kind: sending.OutcomeUncertain, Error: "simulated disconnect after transmission", PauseMailbox: true}
	}
	if message.Recipient == c.Reject {
		return sending.SendResult{Kind: sending.OutcomeDefiniteFailure, Error: "simulated definite recipient rejection"}
	}
	return sending.SendResult{Kind: sending.OutcomeSubmitted}
}

type browserDispatcher struct {
	mu sync.Mutex
	d  *sending.Dispatcher
}

func (d *browserDispatcher) Wake() { d.mu.Lock(); defer d.mu.Unlock(); d.d.Wake() }
func (d *browserDispatcher) Synchronize(fn func() error) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.d.Synchronize(fn)
}
func (d *browserDispatcher) InFlight(ctx context.Context) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.d.InFlight(ctx)
}

func TestAcceptanceBrowserHarness(t *testing.T) {
	dir := os.Getenv("BACHTEL_BROWSER_HARNESS")
	if dir == "" {
		t.Skip("set BACHTEL_BROWSER_HARNESS to a control/output directory")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	control := filepath.Join(dir, "control.json")
	if err := os.WriteFile(control, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	f := newAcceptanceFixture(t, true)
	task := f.task(t, 12)
	overlap := f.task(t, 12)
	const email = "operator@example.invalid"
	const password = "Browser-harness-password-2026!"
	if err := operator.Provision(f.app, email, password); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.EnvEncryptionKey, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)))
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/token"):
			if _, err := fmt.Fprint(w, `{"access_token":"simulated-access","refresh_token":"simulated-refresh","token_type":"Bearer","expires_in":86400}`); err != nil {
				return
			}
		case strings.HasSuffix(r.URL.Path, "/me"):
			if _, err := fmt.Fprint(w, `{"id":"simulated-mailbox","mail":"hr@bachtelcorporation.com","userPrincipalName":"hr@bachtelcorporation.com"}`); err != nil {
				return
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	cfg := config.Config{Origin: "http://127.0.0.1", Microsoft: config.Microsoft{TenantID: "simulated-tenant", ClientID: "simulated-client", ClientSecret: "simulated-secret", RedirectURL: "http://127.0.0.1/connection/callback", SenderAddress: "hr@bachtelcorporation.com", AuthorityURL: provider.URL, GraphBaseURL: provider.URL}}
	connection := microsoft.NewService(f.app, cfg)
	auth, err := connection.BeginAuth(context.Background(), "simulated-operator")
	if err != nil {
		t.Fatal(err)
	}
	authURL, err := url.Parse(auth)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = connection.CompleteAuth(context.Background(), "simulated-operator", authURL.Query().Get("state"), "simulated-code"); err != nil {
		t.Fatal(err)
	}
	proxy := &browserDispatcher{}
	start := func() {
		proxy.d = sending.NewDispatcher(f.store, browserMailbox{control}, sending.WorkerOptions{DataDir: f.app.DataDir(), Now: f.clock.Now, After: f.clock.After, RequestTimeout: 30 * time.Second})
		if err := proxy.d.Start(); err != nil {
			t.Fatal(err)
		}
	}
	start()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
		defer cancel()
		if err := proxy.d.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	}()
	connection.SetDispatchGuard(proxy)
	server := httptest.NewServer(web.NewHandlerWithDispatcher(f.app, cfg, connection, f.store, proxy))
	defer server.Close()
	gated := httptest.NewServer(web.NewHandlerWithDispatcher(f.app, cfg, connection, sending.NewStore(f.app, sending.Options{}), proxy))
	defer gated.Close()
	metadata := map[string]any{"url": server.URL, "task_url": server.URL + "/campaigns/" + f.campaignID + "/tasks/" + task + "/sending", "overlap_task_url": server.URL + "/campaigns/" + f.campaignID + "/tasks/" + overlap + "/sending", "gated_url": gated.URL, "gated_task_url": gated.URL + "/campaigns/" + f.campaignID + "/tasks/" + task + "/sending", "email": email, "password": password, "control": control, "data_dir": f.app.DataDir()}
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "harness.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Printf("BROWSER_HARNESS %s\n", data); err != nil {
		t.Fatal(err)
	}
	restart := 0
	for {
		c, err := readBrowserControl(control)
		if err != nil {
			t.Fatal(err)
		}
		if c.Done {
			return
		}
		if c.Restart != restart {
			proxy.mu.Lock()
			ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
			err := proxy.d.Shutdown(ctx)
			cancel()
			if err != nil {
				proxy.mu.Unlock()
				t.Fatal(err)
			}
			// New store and coordinator recover persisted history; tasks need manual resume.
			f.store = sending.NewStore(f.app, sending.Options{AllowCandidateSending: true, Now: f.clock.Now})
			start()
			proxy.mu.Unlock()
			restart = c.Restart
			if _, err := fmt.Printf("BROWSER_HARNESS_RESTARTED %d\n", restart); err != nil {
				t.Fatal(err)
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
}
