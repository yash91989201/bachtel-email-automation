package web

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yash91989201/bachtel-email-automation/internal/operator"
	"github.com/yash91989201/bachtel-email-automation/internal/web/views"
)

// loginFailedMessage is shown for every sign-in failure so the response never
// reveals whether an account exists.
const loginFailedMessage = "Email or password is incorrect."

func (s *server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if s.sessionRecord(r) != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.render(w, r, views.LoginPage(""))
}

func (s *server) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	key := loginKey(r)
	allowed, retryAfter := s.logins.allow(key, time.Now())
	if !allowed {
		w.Header().Set("Retry-After", formatSeconds(retryAfter))
		http.Error(w, "Too many sign-in attempts. Try again later.", http.StatusTooManyRequests)
		return
	}

	if err := r.ParseForm(); err != nil {
		s.render(w, r, views.LoginPage(loginFailedMessage))
		return
	}
	email := strings.TrimSpace(r.PostFormValue("email"))
	password := r.PostFormValue("password")

	record, err := s.app.FindAuthRecordByEmail(operator.CollectionName, email)
	if err != nil || !record.ValidatePassword(password) {
		// Log a safe code only; the submitted password never leaves this scope.
		s.app.Logger().Warn("operator sign-in failed", "reason", "invalid_credentials")
		s.render(w, r, views.LoginPage(loginFailedMessage))
		return
	}

	if err := s.setSessionCookie(w, record); err != nil {
		s.app.Logger().Error("issue operator session", "error", err)
		http.Error(w, "Unable to start a session. Try again.", http.StatusInternalServerError)
		return
	}
	s.logins.reset(key)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.clearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func formatSeconds(d time.Duration) string {
	seconds := int(d.Seconds())
	if seconds < 1 {
		seconds = 1
	}
	return strconv.Itoa(seconds)
}
