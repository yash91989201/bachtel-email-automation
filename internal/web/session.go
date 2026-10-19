package web

import (
	"context"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
	"github.com/yash91989201/bachtel-email-automation/internal/operator"
)

// sessionCookieName holds the PocketBase operator auth token.
const sessionCookieName = "operator_session"

type recordContextKey struct{}

// sessionRecord returns the authenticated operator record or nil. Expired,
// forged, or foreign-collection tokens (e.g. a PocketBase superuser token)
// behave exactly like no session: no error page, no disclosure.
func (s *server) sessionRecord(r *http.Request) *core.Record {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return nil
	}
	record, err := s.app.FindAuthRecordByToken(cookie.Value, core.TokenTypeAuth)
	if err != nil || record == nil {
		return nil
	}
	// Only the operator collection grants access to the UI.
	if record.Collection().Name != operator.CollectionName {
		return nil
	}
	return record
}

// setSessionCookie installs the operator session cookie with the collection's
// own token lifetime.
func (s *server) setSessionCookie(w http.ResponseWriter, record *core.Record) error {
	token, err := record.NewAuthToken()
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cfg.SecureCookies(),
		MaxAge:   int(record.Collection().AuthToken.Duration),
	})
	return nil
}

// clearSessionCookie expires the session cookie.
func (s *server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cfg.SecureCookies(),
		MaxAge:   -1,
	})
}

// requireOperator gates a handler on a valid operator session. Safe methods
// redirect to the login page; other methods get a plain 401 so scripted
// clients can distinguish the failure without an HTML detour.
func (s *server) requireOperator(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		record := s.sessionRecord(r)
		if record == nil {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				http.Redirect(w, r, "/login", http.StatusSeeOther)
			default:
				http.Error(w, "authentication required", http.StatusUnauthorized)
			}
			return
		}
		ctx := context.WithValue(r.Context(), recordContextKey{}, record)
		next(w, r.WithContext(ctx))
	}
}

// recordFromContext returns the operator record stored by requireOperator.
// Handlers behind requireOperator can rely on it being present.
func recordFromContext(r *http.Request) *core.Record {
	record, ok := r.Context().Value(recordContextKey{}).(*core.Record)
	if !ok {
		return nil
	}
	return record
}
