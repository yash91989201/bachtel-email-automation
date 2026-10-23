package web

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/yash91989201/bachtel-email-automation/internal/accountlock"
	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
	"github.com/yash91989201/bachtel-email-automation/internal/microsoft"
	"github.com/yash91989201/bachtel-email-automation/internal/reset"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
	"github.com/yash91989201/bachtel-email-automation/internal/web/views"
)

// Only these fixed notices may be selected by a connection-page URL. Raw
// provider errors and form values must never be forwarded to it.
var connectionNotices = map[string]string{
	"not-configured":    "Save your Microsoft Tenant ID, Client ID and Client Secret before signing in.",
	"settings-saved":    "Provider settings saved and encrypted. Sign in with Microsoft 365 to activate them.",
	"settings-stale":    "The saved application settings changed during that sign-in. Start a new sign-in.",
	"settings-invalid":  "The provider settings were not saved. Check the Tenant ID, Client ID and Client Secret.",
	"in-flight":         "Email submissions are in flight. The connection and application settings were not changed; try again once they settle.",
	"state":             "This Microsoft sign-in attempt expired, was already used, or was cleared by a server restart. Start a new sign-in.",
	"credentials":       "The stored connection credentials could not be read. Reconnect the mailbox.",
	"incomplete":        "The sign-in response was incomplete. Start the connection again.",
	"failed":            "The connection could not be completed. Try again.",
	"claimed":           "That mailbox is already connected to another workspace.",
	"titan-auth":        "Titan rejected those mailbox credentials. Check the address and email password.",
	"titan-tls":         "Titan's server certificate could not be verified; the connection was not saved.",
	"titan-unreachable": "Titan could not be reached over encrypted SMTP; the connection was not saved.",
	"titan-address":     "Enter the full mailbox address exactly as it is used to sign in.",
	"titan-password":    "Enter the email password for this Titan mailbox.",
	"reset-blocked":     "The mailbox replacement did not finish. Retry it before using this workspace.",
	"reset-started":     "The mailbox replacement is running. Sending and new uploads stay blocked until it completes.",
	"no-mailbox":        "The signed-in Microsoft account has no usable mailbox address; sign in with the mailbox account itself.",
	"locked":            accountlock.Message,
}

func connectionErrorCode(err error) string {
	if len(accountlock.Blockers(err)) > 0 {
		return "locked"
	}
	if _, ok := errors.AsType[*microsoft.ReplacementRequired](err); ok {
		// The replacement confirmation page is reached from the callback.
		return ""
	}
	switch {
	case errors.Is(err, microsoft.ErrNotConfigured), errors.Is(err, mailbox.ErrSettingsMissing):
		return "not-configured"
	case errors.Is(err, microsoft.ErrRotationInFlight):
		return "in-flight"
	case errors.Is(err, mailbox.ErrSettingsStale):
		return "settings-stale"
	case errors.Is(err, mailbox.ErrSettingsInvalid):
		return "settings-invalid"
	case errors.Is(err, mailbox.ErrCredentialsUnreadable), errors.Is(err, microsoft.ErrRefreshRejected):
		return "credentials"
	case errors.Is(err, microsoft.ErrNoMailbox):
		return "no-mailbox"
	case errors.Is(err, mailbox.ErrClaimed):
		return "claimed"
	case errors.Is(err, mailbox.ErrNotConnected):
		return "incomplete"
	case errors.Is(err, microsoft.ErrAuthorizationState):
		return "state"
	case errors.Is(err, sending.ErrTitanAuth):
		return "titan-auth"
	case errors.Is(err, sending.ErrTitanCertificate):
		return "titan-tls"
	case errors.Is(err, sending.ErrTitanUnreachable):
		return "titan-unreachable"
	case errors.Is(err, reset.ErrDraining):
		return "in-flight"
	case errors.Is(err, mailbox.ErrResetInProgress):
		return "reset-started"
	default:
		return "failed"
	}
}

func (s *server) connectionMessage(err error) string {
	return connectionNotices[connectionErrorCode(err)]
}

// connectionData builds the safe view model. Nothing here includes tokens,
// passwords, ciphertext, verifiers, or the client secret.
func (s *server) connectionData(r *http.Request, notice string) views.ConnectionData {
	ownerID := recordFromContext(r).Id
	data := views.ConnectionData{
		Status:              string(mailbox.StatusDisconnected),
		Notice:              notice,
		TitanAddress:        s.cfg.Titan.Address,
		CallbackURL:         s.cfg.CallbackURL(),
		ShowProviderChooser: r.URL.Query().Get("change_provider") == "1",
	}
	if settings, err := s.mailboxes.Settings(r.Context(), ownerID); err == nil {
		data.MicrosoftSaved = settings.Saved()
		data.MicrosoftGen = settings.Generation
		data.MicrosoftTenantID = settings.TenantID
		data.MicrosoftClientID = settings.ClientID
		data.MicrosoftPending = settings.HasPending()
		data.MicrosoftPendingTenantID = settings.PendingTenantID
		data.MicrosoftPendingClientID = settings.PendingClientID
		data.MicrosoftPendingGen = settings.PendingGeneration
		data.MicrosoftReady = settings.Saved() || settings.HasPending()
	} else if !errors.Is(err, mailbox.ErrSettingsMissing) {
		s.app.Logger().Warn("read saved provider settings", "error", err)
		data.LastError = "The saved provider settings could not be read."
	}

	connection, err := s.mailboxes.Current(r.Context(), ownerID)
	switch {
	case errors.Is(err, mailbox.ErrNotConnected):
	case err != nil:
		s.app.Logger().Warn("read mailbox connection status", "error", err)
		data.LastError = "The mailbox connection could not be read."
	default:
		data.Provider = string(connection.Provider)
		data.SenderAddress = connection.Sender
		data.AccountEmail = connection.Identity
		data.Status = string(connection.Status)
		if connection.LastError != "" {
			data.LastError = connection.LastError
		}
		if !connection.ExpiresAt.IsZero() {
			data.CredentialExpiresAt = connection.ExpiresAt.Format(time.RFC3339)
		}
	}

	if blocked, err := reset.Blocking(s.app, ownerID); err != nil {
		s.app.Logger().Error("read mailbox replacement state", "error", err)
	} else if blocked {
		data.ResetBlocked = true
		data.ResetBlockedReason = "Sending is blocked while the previous replacement is completed."
	}

	if operation, err := s.mailboxes.ActiveReset(r.Context(), ownerID); err == nil && operation.Status == mailbox.ResetStaging {
		data.Replacement = s.replacementData(r.Context(), ownerID, operation)
	}
	return data
}

// replacementData describes the validated proposal without exposing secrets.
func (s *server) replacementData(ctx context.Context, ownerID string, operation *mailbox.Reset) *views.ReplacementData {
	data := &views.ReplacementData{ResetID: operation.ID}
	old, err := s.mailboxes.ByID(ctx, operation.FromConnectionID)
	if err == nil {
		data.OldProvider = string(old.Provider)
		data.OldSender = old.Sender
	}
	target, err := s.mailboxes.ByID(ctx, operation.ToConnectionID)
	if err != nil {
		return nil
	}
	data.NewProvider = string(target.Provider)
	data.NewSender = target.Sender
	data.Retained = []string{
		"Campaign definitions and result-column settings",
		"Source-header mapping suggestions",
		"Immutable configuration revisions and original template DOCX files",
	}
	data.Deleted = []string{
		"Candidates, task snapshots and reservations",
		"Uploaded candidate workbooks and exclusion/duplicate reports",
		"Tasks and pending schedules",
		"Formatting test approvals",
		"Submission attempts, manual resolutions and history",
		"Generated workbooks, reports and ZIP downloads",
	}
	return data
}

func (s *server) handleConnection(w http.ResponseWriter, r *http.Request) {
	// The legacy path renders the same Account mailbox section: there is no
	// second mailbox management surface.
	data := s.accountMailboxData(r, accountNotices[r.URL.Query().Get("notice")])
	if provider := r.URL.Query().Get("provider"); mailbox.Provider(provider).Valid() {
		data.Connection.SelectedProvider = provider
	}
	s.render(w, r, views.AccountPage(data))
}

func (s *server) handleReplacement(w http.ResponseWriter, r *http.Request) {
	ownerID := recordFromContext(r).Id
	operation, err := s.mailboxes.ActiveReset(r.Context(), ownerID)
	if err != nil || operation.Status != mailbox.ResetStaging {
		http.Redirect(w, r, "/connection", http.StatusSeeOther)
		return
	}
	data := s.connectionData(r, "")
	if data.Replacement == nil {
		http.Redirect(w, r, "/connection", http.StatusSeeOther)
		return
	}
	data.LastError = data.Notice
	data.Notice = ""
	s.render(w, r, views.ReplacementPage(data))
}

func (s *server) handleMicrosoftSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ownerID := recordFromContext(r).Id
	// The secret is read from the form only: it is sealed immediately and
	// never rendered, logged, or echoed back.
	_, err := s.mailboxes.SaveSettings(r.Context(), ownerID, mailbox.ProviderMicrosoft,
		r.FormValue("tenant_id"), r.FormValue("client_id"), r.FormValue("client_secret"))
	if err != nil {
		if errors.Is(err, mailbox.ErrSettingsInvalid) {
			// Validation text names fields only, never values.
			s.app.Logger().Info("rejected microsoft provider settings")
		} else {
			s.app.Logger().Warn("save microsoft provider settings", "error", err)
		}
		data := s.connectionData(r, "")
		data.SelectedProvider = string(mailbox.ProviderMicrosoft)
		data.LastError = s.settingsMessage(err)
		s.render(w, r, views.ConnectionPage(data))
		return
	}
	http.Redirect(w, r, "/connection?provider=microsoft365&notice=settings-saved", http.StatusSeeOther)
}

// settingsMessage maps a rejected settings save to operator text. Only field
// names are forwarded, never submitted values.
func (s *server) settingsMessage(err error) string {
	switch {
	case errors.Is(err, mailbox.ErrSettingsInvalid):
		return err.Error()
	case errors.Is(err, mailbox.ErrResetInProgress):
		return connectionNotices["reset-started"]
	case errors.Is(err, mailbox.ErrCredentialsUnreadable):
		return connectionNotices["credentials"]
	default:
		return connectionNotices["settings-invalid"]
	}
}

func (s *server) handleMicrosoftConnect(w http.ResponseWriter, r *http.Request) {
	record := recordFromContext(r)
	authURL, err := s.connections.BeginAuth(r.Context(), record.Id)
	if err != nil {
		s.app.Logger().Error("begin microsoft authorization", "error", err)
		data := s.connectionData(r, s.connectionMessage(err))
		data.SelectedProvider = string(mailbox.ProviderMicrosoft)
		s.render(w, r, views.ConnectionPage(data))
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
		http.Redirect(w, r, "/connection?provider=microsoft365&notice=incomplete", http.StatusSeeOther)
		return
	}
	_, err := s.connections.CompleteAuth(r.Context(), record.Id, state, code)
	if err != nil {
		if _, ok := errors.AsType[*microsoft.ReplacementRequired](err); ok {
			// The mailbox is validated and claimed but not installed; the
			// operator must confirm the destructive reset explicitly.
			http.Redirect(w, r, "/connection/replace", http.StatusSeeOther)
			return
		}
		s.app.Logger().Error("complete microsoft authorization", "error", err)
		// Leave the single-use callback URL so refreshing the error page cannot
		// replay it and obscure the original failure.
		http.Redirect(w, r, "/connection?provider=microsoft365&notice="+connectionErrorCode(err), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/connection", http.StatusSeeOther)
}

func (s *server) handleTitanConnect(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ownerID := recordFromContext(r).Id
	renderError := func(message string) {
		data := s.connectionData(r, message)
		data.SelectedProvider = string(mailbox.ProviderTitan)
		s.render(w, r, views.ConnectionPage(data))
	}
	address := mailbox.CanonicalSender(r.FormValue("address"))
	password := r.FormValue("password")
	parsed, err := mail.ParseAddress(address)
	if err != nil || parsed.Address != address {
		renderError(connectionNotices["titan-address"])
		return
	}
	if password == "" {
		renderError(connectionNotices["titan-password"])
		return
	}
	if err := sending.ValidateTitan(r.Context(), s.cfg.Titan.Address, address, password, sending.TitanOptions{Timeout: 20 * time.Second}); err != nil {
		s.app.Logger().Warn("validate titan mailbox", "error", err)
		renderError(connectionNotices[connectionErrorCode(err)])
		return
	}

	candidate := mailbox.Candidate{
		OwnerID:    ownerID,
		Provider:   mailbox.ProviderTitan,
		Sender:     address,
		Credential: mailbox.Credential{Provider: mailbox.ProviderTitan, Password: password},
	}
	current, err := s.mailboxes.Current(r.Context(), ownerID)
	switch {
	case errors.Is(err, mailbox.ErrNotConnected):
		if _, err := s.mailboxes.Install(r.Context(), candidate); err != nil {
			renderError(s.connectionMessage(err))
			return
		}
	case err != nil:
		s.app.Logger().Error("read mailbox connection", "error", err)
		renderError(s.connectionMessage(err))
		return
	case current.Provider == mailbox.ProviderTitan && current.Sender == address:
		// Same mailbox: reconnect it, keeping history and approvals.
		if err := s.mailboxes.Reconnect(r.Context(), current, candidate.Credential); err != nil {
			renderError(s.connectionMessage(err))
			return
		}
	default:
		if _, err := s.mailboxes.StageReplacement(r.Context(), candidate); err != nil {
			renderError(s.connectionMessage(err))
			return
		}
		http.Redirect(w, r, "/connection/replace", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/connection", http.StatusSeeOther)
}

func (s *server) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	ownerID := recordFromContext(r).Id
	if blockers, err := accountlock.Pending(r.Context(), s.app, ownerID); err == nil && len(blockers) > 0 {
		// Checked here for the named warning; the mailbox store rechecks the
		// lock inside its own transaction, which is what actually refuses it.
		data := s.accountMailboxData(r, "")
		data.Connection.LastError = accountlock.Message
		s.renderWithStatus(w, r, http.StatusConflict, views.AccountPage(data))
		return
	}
	connection, err := s.mailboxes.Current(r.Context(), ownerID)
	if err != nil {
		s.render(w, r, views.ConnectionPage(s.connectionData(r, s.connectionMessage(err))))
		return
	}
	// New launches stop as soon as the status changes; existing requests for
	// this mailbox settle before usable credentials are removed.
	if s.dispatcher != nil {
		if n, err := s.dispatcher.InFlightFor(r.Context(), ownerID); err == nil && n > 0 {
			s.render(w, r, views.ConnectionPage(s.connectionData(r, connectionNotices["in-flight"])))
			return
		}
	}
	if err := s.mailboxes.Disconnect(r.Context(), connection); err != nil {
		s.app.Logger().Error("disconnect mailbox", "error", err)
		s.render(w, r, views.ConnectionPage(s.connectionData(r, s.connectionMessage(err))))
		return
	}
	http.Redirect(w, r, "/connection", http.StatusSeeOther)
}

func (s *server) handleReplaceConfirm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ownerID := recordFromContext(r).Id
	if err := s.resets.Confirm(r.Context(), ownerID, strings.TrimSpace(r.FormValue("reset"))); err != nil {
		s.app.Logger().Error("confirm mailbox replacement", "error", err)
		http.Redirect(w, r, "/connection?notice="+connectionErrorCode(err), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/connection?notice=reset-started", http.StatusSeeOther)
}

func (s *server) handleReplaceCancel(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ownerID := recordFromContext(r).Id
	if err := s.mailboxes.CancelReset(r.Context(), ownerID, strings.TrimSpace(r.FormValue("reset"))); err != nil {
		s.app.Logger().Error("cancel mailbox replacement", "error", err)
	}
	http.Redirect(w, r, "/connection", http.StatusSeeOther)
}
