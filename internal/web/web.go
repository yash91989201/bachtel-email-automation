// Package web serves the operator UI on PocketBase's listener. Every page
// except /health, /login, /static/* and /robots.txt requires the operator
// session; unsafe requests are additionally guarded by Origin checks.
package web

import (
	"bytes"
	"embed"
	"net/http"
	"time"

	"github.com/a-h/templ"
	"github.com/pocketbase/pocketbase/core"
	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/config"
	"github.com/yash91989201/bachtel-email-automation/internal/exports"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/microsoft"
	"github.com/yash91989201/bachtel-email-automation/internal/retention"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
	"github.com/yash91989201/bachtel-email-automation/internal/web/views"
)

//go:generate go tool templ generate -path views

//go:embed static
var assets embed.FS

type server struct {
	app         core.App
	cfg         config.Config
	connections *microsoft.Service
	campaigns   *campaign.Service
	tasks       *intake.Service
	logins      *loginLimiter
	sending     *sending.Store
	dispatcher  Dispatcher
	exports     *exports.Service
	retention   *retention.Service
}

// NewHandler serves the application UI. PocketBase owns the server lifecycle,
// its API, and the dashboard on the same listener.
func NewHandler(app core.App, cfg config.Config, connections *microsoft.Service) http.Handler {
	store := sending.NewStore(app, sending.StoreOptions{
		SenderAddress: cfg.Microsoft.SenderAddress,
		RestoreReview: cfg.RestoreReview,
	})
	return NewHandlerWithDispatcher(app, cfg, connections, store, nil)
}

// NewHandlerWithDispatcher injects the process-owned sender and its durable store.
func NewHandlerWithDispatcher(app core.App, cfg config.Config, connections *microsoft.Service, store *sending.Store, dispatcher Dispatcher) http.Handler {
	return NewHandlerWithServices(app, cfg, connections, store, dispatcher, exports.NewService(app, cfg))
}

// NewHandlerWithServices shares the process-owned export worker with handlers.
func NewHandlerWithServices(app core.App, cfg config.Config, connections *microsoft.Service, store *sending.Store, dispatcher Dispatcher, exported *exports.Service) http.Handler {
	campaigns := campaign.NewService(app, cfg.PandocPath)
	s := &server{
		app:         app,
		sending:     store,
		dispatcher:  dispatcher,
		cfg:         cfg,
		connections: connections,
		campaigns:   campaigns,
		tasks:       intake.NewService(app, campaigns, cfg.Intake),
		exports:     exported,
		retention:   retention.NewService(app),
		logins:      newLoginLimiter(10, 5*time.Minute),
	}

	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.FileServerFS(assets))
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
	mux.HandleFunc("GET /connection", s.requireOperator(s.handleConnection))
	mux.HandleFunc("POST /connection/connect", s.requireOperator(s.handleConnect))
	mux.HandleFunc("GET /connection/callback", s.requireOperator(s.handleCallback))
	mux.HandleFunc("POST /connection/disconnect", s.requireOperator(s.handleDisconnect))
	s.registerCampaignRoutes(mux)
	s.registerTaskRoutes(mux)
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
