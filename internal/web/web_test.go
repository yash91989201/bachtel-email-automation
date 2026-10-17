package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/ui"
	_ "github.com/yash91989201/bachtel-email-automation/internal/migrations"
)

func TestAppRoutes(t *testing.T) {
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.ClearBootstrap(); err != nil {
			t.Error(err)
		}
	})
	if err := app.RunAppMigrations(); err != nil {
		t.Fatal(err)
	}
	if got := app.Settings().Meta.AppName; got != "Bachtel Email Automation" {
		t.Fatalf("initial settings migration did not run: %q", got)
	}
	if !app.Settings().RateLimits.Enabled {
		t.Fatal("rate limits should be enabled by the migration")
	}
	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	// apis.Serve adds this dashboard route in the running application.
	router.GET("/_/{path...}", apis.Static(ui.DistDirFS, false))
	router.Any("/{path...}", apis.WrapStdHandler(NewHandler(app)))
	handler, err := router.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path        string
		status      int
		contentType string
		contains    string
	}{
		{"/", http.StatusOK, "text/html", "Your app starts here."},
		{"/status", http.StatusOK, "text/html", "PocketBase is connected."},
		{"/static/app.css", http.StatusOK, "text/css", ".btn"},
		{"/static/alpine.min.js", http.StatusOK, "javascript", "Alpine"},
		{"/static/htmx.min.js", http.StatusOK, "javascript", "htmx"},
		{"/robots.txt", http.StatusOK, "text/plain", "Disallow: /_/"},
		{"/api/health", http.StatusOK, "application/json", "200"},
		{"/_/", http.StatusOK, "text/html", "<html"},
		{"/missing", http.StatusNotFound, "text/plain", "404"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rec.Code != tc.status || !strings.Contains(rec.Header().Get("Content-Type"), tc.contentType) || !strings.Contains(rec.Body.String(), tc.contains) {
				t.Fatalf("%s: status=%d type=%q body=%.200s", tc.path, rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
			}
		})
	}
	// This fragment is deliberately a read-only operation.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/status", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /status: got %d, want 405", rec.Code)
	}
}
