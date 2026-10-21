package sending_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/yash91989201/bachtel-email-automation/internal/config"
	"github.com/yash91989201/bachtel-email-automation/internal/exports"
	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
	"github.com/yash91989201/bachtel-email-automation/internal/microsoft"
	"github.com/yash91989201/bachtel-email-automation/internal/reset"
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

// browserDispatcher exposes the sole dispatcher to the web handler's operator
// barrier, including the per-owner in-flight query used by mailbox replacement.
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
func (d *browserDispatcher) InFlightFor(ctx context.Context, ownerID string) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.d.InFlightFor(ctx, ownerID)
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
	// The fixture provisions the operator and installs one connected mailbox,
	// so the browser only needs the fixture's credentials to sign in.
	f := newAcceptanceFixture(t)
	task := f.task(t, 12)
	overlap := f.task(t, 12)
	const email = "operator@example.invalid"
	const password = "correct-horse-battery"

	cfg := config.Config{
		Origin:     "http://127.0.0.1",
		PandocPath: f.pandocPath,
		Microsoft:  config.Microsoft{AuthorityURL: "http://127.0.0.1", GraphBaseURL: "http://127.0.0.1"},
		Titan:      config.Titan{Address: "smtp.example.invalid"},
	}
	mailboxes := mailbox.NewStore(f.app, func() ([]byte, error) { return testKey, nil })
	connections := microsoft.NewService(f.app, cfg, mailboxes)
	exported := exports.NewService(f.app, cfg)
	resets := reset.NewService(f.app, mailboxes)

	proxy := &browserDispatcher{}
	start := func() {
		proxy.d = sending.NewDispatcher(f.store, fixedResolver(browserMailbox{control}), sending.WorkerOptions{DataDir: f.app.DataDir(), Now: f.clock.Now, After: f.clock.After, RequestTimeout: 30 * time.Second})
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
	resets.SetDrainer(proxy.d)
	server := httptest.NewServer(web.NewHandler(f.app, cfg, web.Services{
		Connections: connections,
		Mailboxes:   mailboxes,
		Sending:     f.store,
		Dispatcher:  proxy,
		Exports:     exported,
		Resets:      resets,
	}))
	defer server.Close()
	metadata := map[string]any{"url": server.URL, "task_url": server.URL + "/campaigns/" + f.campaignID + "/tasks/" + task + "/sending", "overlap_task_url": server.URL + "/campaigns/" + f.campaignID + "/tasks/" + overlap + "/sending", "email": email, "password": password, "control": control, "data_dir": f.app.DataDir()}
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "harness.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = fmt.Printf("BROWSER_HARNESS %s\n", data); err != nil {
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
			f.store = sending.NewStore(f.app, sending.Options{Now: f.clock.Now})
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
