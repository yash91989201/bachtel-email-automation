package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/yash91989201/bachtel-email-automation/internal/accountlock"
	"github.com/yash91989201/bachtel-email-automation/internal/ai"
	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
	"github.com/yash91989201/bachtel-email-automation/internal/presentation"
	"github.com/yash91989201/bachtel-email-automation/internal/web/views"
)

// Account sections. The mailbox and AI connection are independently reachable.
const (
	accountSectionMailbox = "mailbox"
	accountSectionAI      = "ai"
)

// Only these fixed notices may be selected by an Account URL.
var accountNotices = map[string]string{
	"validated":         "The exact provider, endpoint, API format and model returned the required normalization output. Validation may have consumed provider usage. Review the proposal and confirm it to activate it.",
	"confirmed":         "AI connection confirmed. Future normalization runs use exactly this provider, endpoint, API format and model.",
	"repaired":          "The API key was replaced for the same confirmed configuration. Existing tasks, output and approvals are unchanged.",
	"settings-saved":    connectionNotices["settings-saved"],
	"reset-started":     connectionNotices["reset-started"],
	"in-flight":         connectionNotices["in-flight"],
	"credentials":       connectionNotices["credentials"],
	"titan-auth":        connectionNotices["titan-auth"],
	"titan-tls":         connectionNotices["titan-tls"],
	"titan-unreachable": connectionNotices["titan-unreachable"],
	"titan-address":     connectionNotices["titan-address"],
	"titan-password":    connectionNotices["titan-password"],
	"claimed":           connectionNotices["claimed"],
	"reset-blocked":     connectionNotices["reset-blocked"],
	"state":             connectionNotices["state"],
	"incomplete":        connectionNotices["incomplete"],
	"failed":            connectionNotices["failed"],
	"not-configured":    connectionNotices["not-configured"],
	"settings-stale":    connectionNotices["settings-stale"],
	"settings-invalid":  connectionNotices["settings-invalid"],
	"no-mailbox":        connectionNotices["no-mailbox"],
}

// aiFailureMessages maps fixed AI failures to operator-safe text. Raw provider
// bodies, requests and keys never reach the page.
var aiFailureMessages = []struct {
	err     error
	message string
}{
	{ai.ErrEndpointUnsafe, "Use a publicly reachable HTTPS API root. Loopback, private, link-local and metadata destinations are refused, and redirects may not leave the validated origin."},
	{ai.ErrAuthentication, "The endpoint rejected the API key. Check the key and that it belongs to the selected provider."},
	{ai.ErrModelUnavailable, "The selected model is not available at this endpoint. Check the exact model identifier and API format."},
	{ai.ErrThrottled, "The endpoint is throttling requests. Try validation again later."},
	{ai.ErrEndpointTimeout, "The endpoint did not answer within the allowed time. Try again later."},
	{ai.ErrResponseTooLarge, "The endpoint response exceeded the allowed size. The model or endpoint is not suitable for normalization."},
	{ai.ErrOutputContract, "The model did not return the required normalization output. This exact model, API format and endpoint combination cannot be activated."},
	{ai.ErrProviderFailure, "The endpoint returned an error. Try again later."},
	{ai.ErrCredentialUnreadable, ai.ErrCredentialUnreadable.Error()},
	{ai.ErrDraftStale, "The validated proposal is no longer current. Validate the settings again; the existing confirmed connection is unchanged."},
}

func aiFailureMessage(err error) string {
	if _, ok := errors.AsType[*accountlock.BlockedError](err); ok {
		return accountlock.Message
	}
	if errors.Is(err, ai.ErrInvalidSetup) {
		// Field-name diagnostics only; never a submitted value.
		return err.Error()
	}
	for _, entry := range aiFailureMessages {
		if errors.Is(err, entry.err) {
			return entry.message
		}
	}
	return "The AI connection settings were not accepted. Check the values and try again. The existing confirmed connection is unchanged."
}

// aiNotices selects a fixed notice from the Account URL.
func aiNotice(code string) string {
	return accountNotices[code]
}

// accountLock derives the shared connection-change warning from stored task
// facts on every request; cached page state is never authority.
func (s *server) accountLock(r *http.Request) views.AccountLock {
	ownerID := recordFromContext(r).Id
	lock := views.AccountLock{Message: accountlock.Message}
	blockers, err := accountlock.Pending(r.Context(), s.app, ownerID)
	if err != nil {
		s.app.Logger().Error("read pending task blockers", "error", err)
		return lock
	}
	lock.Blocked = len(blockers) > 0
	for _, blocker := range blockers {
		lock.Tasks = append(lock.Tasks, views.AccountBlocker{
			CampaignName: blocker.CampaignName,
			Filename:     blocker.Filename,
			Reference:    blocker.Reference,
			Status:       blocker.Status,
			Closing:      blocker.Closing,
			Href:         "/campaigns/" + blocker.CampaignID + "/tasks/" + blocker.TaskID,
		})
	}
	return lock
}

// accountMailboxData builds the mailbox section model with the shared lock.
func (s *server) accountMailboxData(r *http.Request, notice string) views.AccountPageData {
	lock := s.accountLock(r)
	connection := s.connectionData(r, notice)
	connection.Locked = lock.Blocked
	connection.LockMessage = lock.Message
	return views.AccountPageData{Section: accountSectionMailbox, Lock: lock, Connection: connection}
}

// accountAIData builds the AI section model: confirmed configuration,
// non-secret saved values, staged proposal and the shared lock.
func (s *server) accountAIData(r *http.Request, notice, problem string) views.AccountPageData {
	data := views.AccountPageData{
		Section: accountSectionAI,
		Lock:    s.accountLock(r),
		AI: views.AccountAIData{
			Notice:               notice,
			Error:                problem,
			SelectedProvider:     string(ai.ProviderOpenCodeGo),
			APIFormat:            string(ai.FormatResponses),
			CanSwitch:            true,
			ValidationDisclosure: "Validation sends one synthetic normalization request to the selected provider. It contains no workbook, recipient or candidate value, and it may consume provider usage.",
		},
	}
	data.AI.CanSwitch = !data.Lock.Blocked
	ownerID := recordFromContext(r).Id
	if connection, err := s.aiConnection(r.Context(), ownerID); err == nil && connection != nil {
		data.AI.Connection = aiConnectionView(connection)
	}
	if staged, err := s.aiDraft(r.Context(), ownerID); err == nil && staged != nil {
		data.AI.Draft = aiDraftView(staged)
	}
	data.AI.Providers = aiProviderOptions(data.AI.SelectedProvider)
	data.AI.SuggestedModels = aiSuggestModels(ai.ProviderOpenCodeGo)
	data.AI.Guidance = aiGuidance(ai.ProviderOpenCodeGo)
	data.AI.APIFormats = aiFormatOptions(ai.ProviderOpenCodeGo, string(ai.FormatResponses))
	data.AI.APIRoot = ai.OpenCodeRoot
	return data
}

// aiConnection reads the confirmed connection, treating an absent one as an
// unconfigured setup rather than an error.
func (s *server) aiConnection(ctx context.Context, ownerID string) (*ai.Connection, error) {
	if s.ai == nil {
		return nil, nil
	}
	connection, err := s.ai.Connection(ctx, ownerID)
	if errors.Is(err, ai.ErrNotConfigured) {
		return nil, nil
	}
	return connection, err
}

func (s *server) aiDraft(ctx context.Context, ownerID string) (*ai.Draft, error) {
	if s.ai == nil {
		return nil, nil
	}
	return s.ai.Draft(ctx, ownerID)
}

func aiConnectionView(connection *ai.Connection) *views.AIConnectionView {
	return &views.AIConnectionView{
		ProviderLabel:           connection.Config.Provider.Label(),
		APIRoot:                 connection.Config.APIRoot,
		APIFormatLabel:          connection.Config.APIFormat.Label(),
		Model:                   connection.Config.Model,
		ConfigurationGeneration: connection.ConfigurationGeneration,
		CredentialGeneration:    connection.CredentialGeneration,
		ValidatedAt:             presentation.Timestamp(connection.ValidatedAt),
		APIKeyConfigured:        connection.APIKeyConfigured,
	}
}

func aiDraftView(staged *ai.Draft) *views.AIDraftView {
	return &views.AIDraftView{
		ID:             staged.ID,
		ProviderLabel:  staged.Config.Provider.Label(),
		APIRoot:        staged.Config.APIRoot,
		APIFormatLabel: staged.Config.APIFormat.Label(),
		Model:          staged.Config.Model,
		ValidatedAt:    presentation.Timestamp(staged.ValidatedAt),
		BaseGeneration: staged.BaseConfigurationGeneration,
	}
}

func aiProviderOptions(selected string) []views.AIProviderOption {
	var options []views.AIProviderOption
	for _, preset := range ai.Presets() {
		options = append(options, views.AIProviderOption{
			Value:    string(preset.Provider),
			Label:    preset.Label,
			Selected: string(preset.Provider) == selected,
			Href:     "/account/ai?provider=" + string(preset.Provider),
		})
	}
	return options
}

func aiFormatOptions(provider ai.Provider, selected string) []views.AIFormatOption {
	preset, ok := ai.PresetFor(provider)
	if !ok {
		return nil
	}
	var options []views.AIFormatOption
	for _, format := range preset.Formats {
		options = append(options, views.AIFormatOption{Value: string(format), Label: format.Label(), Selected: string(format) == selected})
	}
	return options
}

func aiSuggestModels(provider ai.Provider) []string {
	preset, ok := ai.PresetFor(provider)
	if !ok {
		return nil
	}
	return preset.SuggestedModels
}

func aiGuidance(provider ai.Provider) []string {
	preset, ok := ai.PresetFor(provider)
	if !ok {
		return nil
	}
	return preset.Guidance
}

// handleAccount renders the Account mailbox section. The legacy /connection
// route renders the same page, so bookmarks keep working without a second
// mailbox surface.
func (s *server) handleAccount(w http.ResponseWriter, r *http.Request) {
	data := s.accountMailboxData(r, accountNotices[r.URL.Query().Get("notice")])
	if provider := r.URL.Query().Get("provider"); mailbox.Provider(provider).Valid() {
		data.Connection.SelectedProvider = provider
	}
	w.Header().Set("Cache-Control", "no-store")
	s.render(w, r, views.AccountPage(data))
}

// handleAccountAI renders the AI connection section.
func (s *server) handleAccountAI(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	data := s.accountAIData(r, aiNotice(query.Get("notice")), "")
	if provider := ai.Provider(query.Get("provider")); provider.Valid() {
		data.AI.SelectedProvider = string(provider)
	}
	if format := ai.APIFormat(query.Get("format")); format.Valid() {
		data.AI.APIFormat = string(format)
	}
	data.AI.Providers = aiProviderOptions(data.AI.SelectedProvider)
	data.AI.APIFormats = aiFormatOptions(ai.Provider(data.AI.SelectedProvider), data.AI.APIFormat)
	data.AI.SuggestedModels = aiSuggestModels(ai.Provider(data.AI.SelectedProvider))
	data.AI.Guidance = aiGuidance(ai.Provider(data.AI.SelectedProvider))
	if data.AI.SelectedProvider == string(ai.ProviderOpenCodeGo) {
		data.AI.APIRoot = ai.OpenCodeRoot
	}
	w.Header().Set("Cache-Control", "no-store")
	s.render(w, r, views.AccountPage(data))
}

// handleAIValidate validates the exact submitted configuration with one
// synthetic request and stages it. Nothing becomes active here.
func (s *server) handleAIValidate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.ai == nil {
		http.Error(w, "AI settings are unavailable", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid AI connection form", http.StatusBadRequest)
		return
	}
	ownerID := recordFromContext(r).Id
	proposal := ai.Proposal{
		Config: ai.Config{
			Provider:  ai.Provider(r.FormValue("provider")),
			APIRoot:   r.FormValue("api_root"),
			APIFormat: ai.APIFormat(r.FormValue("api_format")),
			Model:     r.FormValue("model"),
		},
		APIKey: r.FormValue("api_key"),
	}
	if _, err := s.ai.StageValidation(r.Context(), ownerID, proposal); err != nil {
		s.app.Logger().Warn("AI connection validation rejected", "error", err)
		data := s.accountAIData(r, "", aiFailureMessage(err))
		data.AI.SelectedProvider = string(proposal.Config.Provider)
		data.AI.APIRoot = proposal.Config.APIRoot
		data.AI.APIFormat = string(proposal.Config.APIFormat)
		data.AI.Model = proposal.Config.Model
		data.AI.Providers = aiProviderOptions(data.AI.SelectedProvider)
		data.AI.APIFormats = aiFormatOptions(proposal.Config.Provider, string(proposal.Config.APIFormat))
		data.AI.SuggestedModels = aiSuggestModels(proposal.Config.Provider)
		data.AI.Guidance = aiGuidance(proposal.Config.Provider)
		s.renderWithStatus(w, r, http.StatusBadRequest, views.AccountPage(data))
		return
	}
	http.Redirect(w, r, "/account/ai?notice=validated", http.StatusSeeOther)
}

// handleAIConfirm activates the staged proposal after its explicit
// confirmation. A configuration switch is refused with the named blocking
// tasks; a same-configuration key repair is allowed.
func (s *server) handleAIConfirm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.ai == nil {
		http.Error(w, "AI settings are unavailable", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid AI connection form", http.StatusBadRequest)
		return
	}
	if r.FormValue("confirmed") != "yes" {
		http.Error(w, "Confirm the exact provider, endpoint, API format and model", http.StatusBadRequest)
		return
	}
	ownerID := recordFromContext(r).Id
	generation := 0
	if raw := strings.TrimSpace(r.FormValue("expected_configuration_generation")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			http.Error(w, "Invalid AI connection generation", http.StatusBadRequest)
			return
		}
		generation = parsed
	}
	draftID := strings.TrimSpace(r.FormValue("draft"))
	repair := r.FormValue("repair") == "yes"
	connection, err := s.ai.Confirm(r.Context(), ownerID, draftID, generation)
	if err != nil {
		s.app.Logger().Warn("AI connection confirmation rejected", "error", err)
		data := s.accountAIData(r, "", aiFailureMessage(err))
		s.renderWithStatus(w, r, http.StatusConflict, views.AccountPage(data))
		return
	}
	notice := "confirmed"
	if repair && connection != nil {
		notice = "repaired"
	}
	http.Redirect(w, r, "/account/ai?notice="+notice, http.StatusSeeOther)
}
