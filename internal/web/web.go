package web

import (
	"bytes"
	"embed"
	"net/http"
	"time"

	"github.com/a-h/templ"
	"github.com/pocketbase/pocketbase/core"
	"github.com/yash91989201/bachtel-email-automation/internal/web/views"
)

//go:generate go tool templ generate -path views

//go:embed static
var assets embed.FS

// NewHandler serves the application UI. PocketBase owns server lifecycle,
// authentication, its API, and the dashboard on the same listener.
func NewHandler(app core.App) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.FileServerFS(assets))
	mux.HandleFunc("GET /robots.txt", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, assets, "static/robots.txt")
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		render(w, r, app, views.Home())
	})
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		if _, err := app.DB().NewQuery("SELECT 1").WithContext(r.Context()).Execute(); err != nil {
			app.Logger().Error("database connectivity check failed", "error", err)
			http.Error(w, "Database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		render(w, r, app, views.DatabaseStatus(time.Now().UTC().Format(time.RFC3339)))
	})
	return mux
}

func render(w http.ResponseWriter, r *http.Request, app core.App, component templ.Component) {
	// Buffer first so rendering errors don't leave a successful, partial response.
	var buf bytes.Buffer
	if err := component.Render(r.Context(), &buf); err != nil {
		app.Logger().Error("render page", "error", err)
		http.Error(w, "Unable to render page", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
}
