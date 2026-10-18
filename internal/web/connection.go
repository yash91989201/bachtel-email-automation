package web

import (
	"errors"
	"net/http"
	"time"

	"github.com/yash91989201/bachtel-email-automation/internal/microsoft"
	"github.com/yash91989201/bachtel-email-automation/internal/web/views"
)

// Only these fixed notices may be selected by a connection-page URL. Raw
// provider errors and callback parameters must never be forwarded to it.
var connectionNotices = map[string]string{
	"not-configured": "The Microsoft connection is not configured. Set the missing settings and try again.",
	"in-flight":      "Email submissions are in flight. The connection was not changed.",
	"identity":       "The signed-in Microsoft account does not match the connected mailbox.",
	"state":          "This Microsoft sign-in attempt expired, was already used, or was cleared by a server restart. Click Connect or Reconnect to start a new sign-in.",
	"credentials":    "The stored connection credentials could not be read. Reconnect the mailbox.",
	"incomplete":     "The sign-in response was incomplete. Start the connection again.",
	"failed":         "The connection could not be completed. Try again.",
}

func connectionErrorCode(err error) string {
	switch {
	case errors.Is(err, microsoft.ErrNotConfigured):
		return "not-configured"
	case errors.Is(err, microsoft.ErrInFlight):
		return "in-flight"
	case errors.Is(err, microsoft.ErrIdentityMismatch):
		return "identity"
	case errors.Is(err, microsoft.ErrAuthorizationState):
		return "state"
	case errors.Is(err, microsoft.ErrCredentialsUnreadable):
		return "credentials"
	default:
		return "failed"
	}
}

func (s *server) connectionMessage(err error) string {
	return connectionNotices[connectionErrorCode(err)]
}

// connectionData builds the safe view model. Nothing here includes tokens,
// ciphertext, verifiers, or the client secret.
func (s *server) connectionData(r *http.Request, notice string) views.ConnectionData {
	data := views.ConnectionData{
		Status:        string(microsoft.StatusDisconnected),
		SenderAddress: s.cfg.Microsoft.SenderAddress,
		Notice:        notice,
	}
	if missing := s.cfg.Microsoft.Missing(); len(missing) > 0 {
		data.MissingSettings = missing
	}
	conn, err := s.connections.Status(r.Context())
	if err != nil {
		s.app.Logger().Warn("read microsoft connection status", "error", err)
		data.LastError = s.connectionMessage(err)
		return data
	}
	data.Status = string(conn.Status)
	data.AccountEmail = conn.AccountEmail
	if conn.SenderAddress != "" {
		data.SenderAddress = conn.SenderAddress
	}
	if !conn.ConnectedAt.IsZero() {
		data.ConnectedAt = conn.ConnectedAt.Format(time.RFC3339)
	}
	if !conn.ExpiresAt.IsZero() {
		data.CredentialExpiresAt = conn.ExpiresAt.Format(time.RFC3339)
	}
	data.LastError = conn.LastError
	return data
}

func (s *server) handleConnection(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, views.ConnectionPage(s.connectionData(r, connectionNotices[r.URL.Query().Get("notice")])))
}

func (s *server) handleConnect(w http.ResponseWriter, r *http.Request) {
	record := recordFromContext(r)
	authURL, err := s.connections.BeginAuth(r.Context(), record.Id)
	if err != nil {
		s.app.Logger().Error("begin microsoft authorization", "error", err)
		s.render(w, r, views.ConnectionPage(s.connectionData(r, s.connectionMessage(err))))
		return
	}
	http.Redirect(w, r, authURL, http.StatusSeeOther)
}

func (s *server) handleCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	record := recordFromContext(r)
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	if state == "" || code == "" {
		http.Redirect(w, r, "/connection?notice=incomplete", http.StatusSeeOther)
		return
	}
	if _, err := s.connections.CompleteAuth(r.Context(), record.Id, state, code); err != nil {
		s.app.Logger().Error("complete microsoft authorization", "error", err)
		// Leave the single-use callback URL so refreshing the error page cannot
		// replay it and obscure the original failure.
		http.Redirect(w, r, "/connection?notice="+connectionErrorCode(err), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/connection", http.StatusSeeOther)
}

func (s *server) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	if err := s.connections.Disconnect(r.Context()); err != nil {
		if errors.Is(err, microsoft.ErrInFlight) {
			s.render(w, r, views.ConnectionPage(s.connectionData(r, s.connectionMessage(err))))
			return
		}
		s.app.Logger().Error("disconnect microsoft connection", "error", err)
		s.render(w, r, views.ConnectionPage(s.connectionData(r, s.connectionMessage(err))))
		return
	}
	http.Redirect(w, r, "/connection", http.StatusSeeOther)
}
