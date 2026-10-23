// Package web serves the operator UI on PocketBase's listener. Every page
// except /health, /login, /static/* and /robots.txt requires the operator
// session; unsafe requests are additionally guarded by Origin checks.
package web

import (
	"bytes"
	"context"
	"embed"
	"net/http"
	"time"

	"github.com/a-h/templ"
	"github.com/pocketbase/pocketbase/core"
	"github.com/yash91989201/bachtel-email-automation/internal/accountlock"
	"github.com/yash91989201/bachtel-email-automation/internal/ai"
	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/config"
	"github.com/yash91989201/bachtel-email-automation/internal/exports"
	"github.com/yash91989201/bachtel-email-automation/internal/finalization"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
	"github.com/yash91989201/bachtel-email-automation/internal/microsoft"
	"github.com/yash91989201/bachtel-email-automation/internal/preparation"
	"github.com/yash91989201/bachtel-email-automation/internal/reset"
	"github.com/yash91989201/bachtel-email-automation/internal/retention"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
	"github.com/yash91989201/bachtel-email-automation/internal/web/views"
)

//go:generate go tool templ generate -path .

//go:embed static
var assets embed.FS

// Services are the process-owned dependencies the UI drives. The dispatcher
// interface is satisfied by the sending dispatcher; a nil value disables only
// the launch barrier, not the durable store.
type Services struct {
	Connections *microsoft.Service
	Mailboxes   *mailbox.Store
	Sending     *sending.Store
	Dispatcher  Dispatcher
	Exports     *exports.Service
	Resets      *reset.Service
	// AI is the operator-owned AI connection store.
	AI *ai.Store
	// Preparation owns AI data-preparation runs and their durable worker.
	Preparation *preparation.Service
	// Finalization owns immutable final sending snapshots.
	Finalization *finalization.Service
}

type server struct {
	app          core.App
	cfg          config.Config
	connections  *microsoft.Service
	mailboxes    *mailbox.Store
	resets       *reset.Service
	campaigns    *campaign.Service
	tasks        *intake.Service
	logins       *loginLimiter
	sending      *sending.Store
	dispatcher   Dispatcher
	exports      *exports.Service
	retention    *retention.Service
	ai           *ai.Store
	preparation  *preparation.Service
	finalization *finalization.Service
}

// NewHandler serves the application UI. PocketBase owns the server lifecycle,
// its API, and the dashboard on the same listener.
func NewHandler(app core.App, cfg config.Config, services Services) http.Handler {
	campaigns := campaign.NewService(app, cfg.PandocPath)
	tasks := intake.NewService(app, campaigns, cfg.Intake)
	tasks.SetWriteGuard(reset.Guard(app))
	s := &server{
		app:          app,
		sending:      services.Sending,
		dispatcher:   services.Dispatcher,
		cfg:          cfg,
		connections:  services.Connections,
		mailboxes:    services.Mailboxes,
		resets:       services.Resets,
		campaigns:    campaigns,
		tasks:        tasks,
		exports:      services.Exports,
		retention:    retention.NewService(app),
		logins:       newLoginLimiter(10, 5*time.Minute),
		ai:           services.AI,
		preparation:  services.Preparation,
		finalization: services.Finalization,
	}
	// Connection changes share one account-wide lock. The stores recheck it
	// inside their own transactions; a stale page is never authority.
	if services.AI != nil {
		services.AI.SetLockCheck(accountlock.Check)
	}
	if services.Mailboxes != nil {
		services.Mailboxes.SetChangeGuard(func(ctx context.Context, checkApp core.App, ownerID string) error {
			return accountlock.Check(ctx, checkApp, ownerID)
		})
	}
	if services.Resets != nil {
		services.Resets.SetChangeGuard(accountlock.Guard(app))
	}

	mux := http.NewServeMux()
	// Embedded assets carry no modification time, so browsers get no validator
	// and can keep serving a stale app.css after a rebuild. Revalidate instead.
	staticFiles := http.FileServerFS(assets)
	mux.Handle("GET /static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		staticFiles.ServeHTTP(w, r)
	}))
	mux.HandleFunc("GET /robots.txt", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, assets, "static/robots.txt")
	})
	// Health reports liveness only: no database, filesystem, or mailbox detail.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if _, err := w.Write([]byte(`{"status":"ok"}`)); err != nil {
			s.app.Logger().Warn("write health response", "error", err)
		}
	})
	mux.HandleFunc("GET /login", s.handleLoginPage)
	mux.HandleFunc("POST /login", s.handleLoginSubmit)
	mux.HandleFunc("POST /logout", s.requireOperator(s.handleLogout))
	mux.HandleFunc("GET /{$}", s.requireOperator(s.handleHome))
	mux.HandleFunc("GET /status", s.requireOperator(s.handleStatus))
	mux.HandleFunc("GET /account/mailbox", s.requireOperator(s.handleAccountMailbox))
	mux.HandleFunc("GET /account/ai", s.requireOperator(s.handleAccountAI))
	mux.HandleFunc("POST /account/ai/models", s.requireOperator(s.handleAIModels))
	mux.HandleFunc("POST /account/ai/validate", s.requireOperator(s.handleAIValidate))
	mux.HandleFunc("POST /account/ai/confirm", s.requireOperator(s.handleAIConfirm))
	mux.HandleFunc("GET /connection/replace", s.requireOperator(s.handleReplacement))
	mux.HandleFunc("POST /connection/microsoft/settings", s.requireOperator(s.handleMicrosoftSettings))
	mux.HandleFunc("POST /connection/microsoft/connect", s.requireOperator(s.handleMicrosoftConnect))
	mux.HandleFunc("GET /connection/callback", s.requireOperator(s.handleCallback))
	mux.HandleFunc("POST /connection/titan", s.requireOperator(s.handleTitanConnect))
	mux.HandleFunc("POST /connection/disconnect", s.requireOperator(s.handleDisconnect))
	mux.HandleFunc("POST /connection/replace/confirm", s.requireOperator(s.handleReplaceConfirm))
	mux.HandleFunc("POST /connection/replace/cancel", s.requireOperator(s.handleReplaceCancel))
	s.registerCampaignRoutes(mux)
	s.registerTaskRoutes(mux)
	s.registerPreparationRoutes(mux)
	s.registerFinalizationRoutes(mux)
	s.registerSendingRoutes(mux)

	// Cross-origin writes (forms) are rejected before any handler mutates
	// state; no bespoke CSRF token protocol is needed.
	protection := http.NewCrossOriginProtection()
	if err := protection.AddTrustedOrigin(cfg.Origin); err != nil {
		app.Logger().Error("add trusted origin", "origin", cfg.Origin, "error", err)
	}
	return protection.Handler(mux)
}

func (s *server) handleHome(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, views.Home())
}

func (s *server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if _, err := s.app.DB().NewQuery("SELECT 1").WithContext(r.Context()).Execute(); err != nil {
		s.app.Logger().Error("database connectivity check failed", "error", err)
		http.Error(w, "Database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	s.render(w, r, views.DatabaseStatus(time.Now().UTC().Format(time.RFC3339)))
}

// render buffers the component so a rendering error never leaves a partial
// successful response.
func (s *server) render(w http.ResponseWriter, r *http.Request, component templ.Component) {
	var buf bytes.Buffer
	if err := component.Render(r.Context(), &buf); err != nil {
		s.app.Logger().Error("render page", "error", err)
		http.Error(w, "Unable to render page", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := w.Write(buf.Bytes()); err != nil {
		s.app.Logger().Warn("write page response", "error", err)
	}
}
