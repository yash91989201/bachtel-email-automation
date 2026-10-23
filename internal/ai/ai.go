// Package ai owns each operator's private AI connection: at most one
// confirmed provider, endpoint, API format and exact model per operator,
// staged proposals that only become active after a synthetic validation and
// an explicit confirmation, and separately versioned credentials sealed under
// the deployment key.
//
// Nothing here prepares a workbook or sends email. The package exposes a small
// validation boundary over existing net/http and encoding/json; provider
// presets are data, protocol handling is one adapter per API format.
package ai

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/pocketbase/pocketbase/core"
)

// Collection names owned by this package.
const (
	// ConnectionsCollection holds the single confirmed connection per owner.
	ConnectionsCollection = "ai_connections"
	// DraftsCollection holds one staged proposal per owner. A proposal never
	// affects a run until it is confirmed.
	DraftsCollection = "ai_connection_drafts"
)

// Provider identifies a supported AI provider preset.
type Provider string

const (
	// ProviderOpenCodeGo is the OpenCode Go preset.
	ProviderOpenCodeGo Provider = "opencode-go"
	// ProviderXiaomiMiMo is the Xiaomi MiMo standard API preset.
	ProviderXiaomiMiMo Provider = "xiaomi-mimo"
	// ProviderCustom is any endpoint speaking a supported API format.
	ProviderCustom Provider = "custom"
)

// Valid reports whether the provider preset is supported.
func (p Provider) Valid() bool {
	return p == ProviderOpenCodeGo || p == ProviderXiaomiMiMo || p == ProviderCustom
}

// Label is the operator-facing provider name.
func (p Provider) Label() string {
	switch p {
	case ProviderOpenCodeGo:
		return "OpenCode Go"
	case ProviderXiaomiMiMo:
		return "Xiaomi MiMo"
	case ProviderCustom:
		return "Custom compatible endpoint"
	default:
		return "Unknown provider"
	}
}

// APIFormat identifies the request/response contract of a connection.
type APIFormat string

const (
	// FormatResponses is the OpenAI Responses API.
	FormatResponses APIFormat = "responses"
	// FormatChatCompletions is the OpenAI Chat Completions API.
	FormatChatCompletions APIFormat = "chat-completions"
)

// Valid reports whether the protocol is supported.
func (f APIFormat) Valid() bool {
	return f == FormatResponses || f == FormatChatCompletions
}

// Label is the operator-facing protocol name.
func (f APIFormat) Label() string {
	switch f {
	case FormatResponses:
		return "Responses API"
	case FormatChatCompletions:
		return "Chat Completions API"
	default:
		return "Unknown API format"
	}
}

// Path is the request path appended to the configured API root.
func (f APIFormat) Path() string {
	switch f {
	case FormatResponses:
		return "/responses"
	default:
		return "/chat/completions"
	}
}

// ModelContract is one verified model identifier and the single API format
// that serves it. A model outside this table is never assigned a guessed
// format: it is listed as unavailable with a remedy instead.
type ModelContract struct {
	ID     string
	Format APIFormat
}

// Preset is provider reference data: labels, default endpoint, supported API
// formats, verified model contracts and setup guidance. It carries no
// protocol behavior. Every default is a starting value the operator may
// override; nothing here pins a submitted endpoint.
type Preset struct {
	Provider    Provider
	Label       string
	DefaultRoot string
	Formats     []APIFormat
	Models      []ModelContract
	Guidance    []string
}

const (
	// OpenCodeRoot is the documented OpenCode Go OpenAI-compatible root,
	// verified on 2026-10-23.
	OpenCodeRoot = "https://opencode.ai/inference/go/openai/v1"
	// XiaomiRoot is the documented Xiaomi MiMo standard API root. The Token
	// Plan host is deliberately not used.
	XiaomiRoot = "https://api.xiaomimimo.com/v1"
)

// Presets lists every supported provider in display order.
func Presets() []Preset {
	return []Preset{
		{
			Provider:    ProviderOpenCodeGo,
			Label:       ProviderOpenCodeGo.Label(),
			DefaultRoot: OpenCodeRoot,
			Formats:     []APIFormat{FormatResponses, FormatChatCompletions},
			Models: []ModelContract{
				{ID: "grok-4.7", Format: FormatResponses},
				{ID: "gpt-6-luna", Format: FormatResponses},
				{ID: "gpt-5.6-luna", Format: FormatResponses},
				{ID: "muse-spark-1.3", Format: FormatResponses},
				{ID: "glm-5.3-flash", Format: FormatChatCompletions},
				{ID: "glm-5.3", Format: FormatChatCompletions},
				{ID: "glm-5.2", Format: FormatChatCompletions},
				{ID: "kimi-k3", Format: FormatChatCompletions},
				{ID: "kimi-k2.7-code", Format: FormatChatCompletions},
				{ID: "longcat-2.0", Format: FormatChatCompletions},
				{ID: "deepseek-v4.1-flash", Format: FormatChatCompletions},
			},
			Guidance: []string{
				"Use the API key from your OpenCode Console workspace. The Go plan covers time-limited usage for a single-member workspace.",
				"Each model is served by one API format; the format follows the selected model and is not editable.",
				"Only the mapped workbook values and preparation metadata are sent to this endpoint. OpenCode documents traffic monitoring for abuse; the Muse Contributor option may allow training on prompts and completions.",
			},
		},
		{
			Provider:    ProviderXiaomiMiMo,
			Label:       ProviderXiaomiMiMo.Label(),
			DefaultRoot: XiaomiRoot,
			Formats:     []APIFormat{FormatChatCompletions},
			Models: []ModelContract{
				{ID: "mimo-v2.6-flash", Format: FormatChatCompletions},
				{ID: "mimo-v2.6-pro", Format: FormatChatCompletions},
				{ID: "mimo-v2.6-pro-ultraspeed", Format: FormatChatCompletions},
			},
			Guidance: []string{
				"Use a Xiaomi MiMo standard API key. The Token Plan host and its coding-oriented usage limits are not used by this preset.",
				"The documented MiMo text models speak Chat Completions; the Anthropic-compatible and audio endpoints are not supported here.",
				"Only the mapped workbook values and preparation metadata are sent to this endpoint. Review Xiaomi's data-handling terms before sending real workbook data.",
			},
		},
		{
			Provider:    ProviderCustom,
			Label:       ProviderCustom.Label(),
			DefaultRoot: "",
			Formats:     []APIFormat{FormatResponses, FormatChatCompletions},
			Guidance: []string{
				"Enter the HTTPS API root that comes before /responses or /chat/completions.",
				"A successful validation proves only that this endpoint, format and exact model return the normalization contract. It does not claim every model there supports it.",
			},
		},
	}
}

// ModelFormat resolves the verified API format of one preset model. It never
// guesses from a model name suffix.
func ModelFormat(provider Provider, model string) (APIFormat, bool) {
	preset, ok := PresetFor(provider)
	if !ok {
		return "", false
	}
	model = strings.TrimSpace(model)
	for _, contract := range preset.Models {
		if contract.ID == model {
			return contract.Format, true
		}
	}
	return "", false
}

// PresetFor returns the preset for a provider.
func PresetFor(provider Provider) (Preset, bool) {
	for _, preset := range Presets() {
		if preset.Provider == provider {
			return preset, true
		}
	}
	return Preset{}, false
}

// Config is the immutable non-secret configuration of a connection. Every run
// records and uses exactly this selection; no fallback model exists.
type Config struct {
	Provider  Provider
	APIRoot   string
	APIFormat APIFormat
	Model     string
}

// Fingerprint is the normalized configuration identity used to distinguish a
// switch from a same-configuration credential repair.
func (c Config) Fingerprint() string {
	return strings.Join([]string{
		string(c.Provider),
		strings.TrimRight(strings.TrimSpace(c.APIRoot), "/"),
		string(c.APIFormat),
		strings.TrimSpace(c.Model),
	}, "|")
}

// Equal reports whether two configurations select the same endpoint, format
// and model.
func (c Config) Equal(other Config) bool { return c.Fingerprint() == other.Fingerprint() }

// SameSelection reports whether two configurations select the same provider,
// endpoint and model. Presets resolve their API format on the server, so a
// stored selection is repaired by comparing the operator's visible choice.
func (c Config) SameSelection(other Config) bool {
	return c.Provider == other.Provider &&
		strings.TrimRight(strings.TrimSpace(c.APIRoot), "/") == strings.TrimRight(strings.TrimSpace(other.APIRoot), "/") &&
		strings.TrimSpace(c.Model) == strings.TrimSpace(other.Model)
}

// Connection is the operator-visible confirmed connection. It never carries
// the API key.
type Connection struct {
	ID                      string
	Config                  Config
	ConfigurationGeneration int
	CredentialGeneration    int
	APIKeyConfigured        bool
	ValidatedAt             time.Time
	UpdatedAt               time.Time
}

// Draft is the operator-visible staged proposal. It never carries the API key.
type Draft struct {
	ID                          string
	Config                      Config
	BaseConfigurationGeneration int
	BaseCredentialGeneration    int
	ValidatedAt                 time.Time
}

// Repair reports whether the draft keeps the exact confirmed configuration and
// only replaces the API credential.
func (d *Draft) Repair(active *Connection) bool {
	return active != nil && d.Config.Equal(active.Config)
}

// Proposal is one submitted, not yet validated connection setup.
type Proposal struct {
	Config Config
	APIKey string
}

// ModelOption is one model a preset endpoint lists. Supported models carry the
// verified API format; every other listed model stays visible but unusable,
// with the reason. Discovery never invents availability.
type ModelOption struct {
	ID        string
	Format    APIFormat
	Supported bool
	Reason    string
}

// Bounded operator input. The API key ceiling is generous but finite; model
// identifiers are one non-empty token, never a path or a sentence.
const (
	maxModelIDLength = 200
	maxAPIKeyLength  = 4096
)

// validModelID reports whether a model identifier is one bounded token.
func validModelID(id string) bool {
	return id != "" && len(id) <= maxModelIDLength && strings.IndexFunc(id, func(r rune) bool {
		return unicode.IsControl(r) || unicode.IsSpace(r)
	}) < 0
}

// Errors reported by this package. Every message is fixed text safe to show
// to an operator: no request, response, header or secret value is included.
var (
	ErrNotConfigured        = errors.New("no AI connection is configured")
	ErrCredentialUnreadable = errors.New("the saved AI credential could not be read; check that APP_ENCRYPTION_KEY matches the key used to save it")
	ErrInvalidSetup         = errors.New("the AI connection setup is not valid")
	ErrDraftStale           = errors.New("the validated proposal is no longer current; validate the settings again")
	ErrEndpointUnsafe       = errors.New("the AI endpoint must be a publicly reachable HTTPS address")
	ErrAuthentication       = errors.New("the AI endpoint rejected the API key")
	ErrModelUnavailable     = errors.New("the selected model is not available at this endpoint")
	ErrThrottled            = errors.New("the AI endpoint is throttling requests; try again later")
	ErrEndpointTimeout      = errors.New("the AI endpoint did not answer within the allowed time")
	ErrResponseTooLarge     = errors.New("the AI response exceeded the allowed size")
	ErrOutputContract       = errors.New("the selected model did not return the required normalization output")
	ErrProviderFailure      = errors.New("the AI endpoint returned an error")
	ErrDiscoveryUnavailable = errors.New("this endpoint did not return a usable model list")
)

// LockCheck refuses a connection switch while the owning account still has
// pending task work. It is injected so this package does not depend on the
// task lifecycle implementation.
type LockCheck func(ctx context.Context, app core.App, ownerID string) error

// ValidationOptions bound the synthetic validation request and its response.
type ValidationOptions struct {
	Timeout          time.Duration
	MaxResponseBytes int64
	// AllowPrivate permits loopback/private destinations and plaintext HTTP.
	// It exists only for simulated endpoints in tests and is never enabled by
	// the running application.
	AllowPrivate bool
}

// DefaultValidationOptions are the production bounds.
func DefaultValidationOptions() ValidationOptions {
	return ValidationOptions{Timeout: 30 * time.Second, MaxResponseBytes: 512 << 10}
}
