// Package config loads application configuration from the process environment.
//
// Secrets live in the environment only. Nothing here writes configuration to
// disk, logs, HTML, or database records, and validation errors name the
// missing settings without printing their values.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/yash91989201/bachtel-email-automation/internal/intake"
)

// Environment variable names.
const (
	// EnvOrigin is the public origin of this application, for example
	// "https://mail.example.com". It anchors cookie security, the derived
	// Microsoft callback URL, and trusted-origin checks.
	EnvOrigin = "APP_ORIGIN"
	// EnvEncryptionKey is the base64-encoded 32 byte key that encrypts stored
	// Microsoft credentials. It is a deployment secret, separate from the
	// Microsoft client secret, and must be backed up independently.
	EnvEncryptionKey = "APP_ENCRYPTION_KEY"
	// EnvTenantID is the Microsoft Entra tenant (directory) id.
	EnvTenantID = "MS_TENANT_ID"
	// EnvClientID is the Microsoft application (client) id.
	EnvClientID = "MS_CLIENT_ID"
	// EnvClientSecret is the Microsoft application client secret.
	EnvClientSecret = "MS_CLIENT_SECRET"
	// EnvRedirectURL overrides the Microsoft redirect URI. It defaults to the
	// callback path appended to EnvOrigin and must match the registration.
	EnvRedirectURL = "MS_REDIRECT_URL"
	// EnvSenderAddress overrides the single mailbox allowed to connect. It
	// exists so a test or staging tenant can point at another address.
	EnvSenderAddress = "MS_SENDER_ADDRESS"
	// EnvAuthorityURL overrides the Microsoft authority origin, used by
	// simulated authorization servers in tests.
	EnvAuthorityURL = "MS_AUTHORITY_URL"
	// EnvGraphBaseURL overrides the Microsoft Graph API base URL, used by
	// simulated Graph servers in tests.
	EnvGraphBaseURL = "MS_GRAPH_BASE_URL"
	// EnvRestoreReview disables every send-like action after a backup restore
	// until the operator reconciles the submission gap.
	EnvRestoreReview = "APP_RESTORE_REVIEW"
	// EnvPandocPath overrides the DOCX converter executable. It defaults to
	// "pandoc" on PATH, which is what the application image packages.
	EnvPandocPath = "PANDOC_PATH"
	// EnvIntakeMaxUploadBytes bounds one uploaded task workbook, in bytes.
	EnvIntakeMaxUploadBytes = "INTAKE_MAX_UPLOAD_BYTES"
	// EnvIntakeUnzipSizeLimit bounds the total expanded size of an uploaded
	// workbook archive, in bytes.
	EnvIntakeUnzipSizeLimit = "INTAKE_UNZIP_SIZE_LIMIT"
	// EnvIntakeUnzipXMLSizeLimit bounds one expanded XML part of an uploaded
	// workbook archive, in bytes.
	EnvIntakeUnzipXMLSizeLimit = "INTAKE_UNZIP_XML_SIZE_LIMIT"
)

const (
	// DefaultOrigin is used when EnvOrigin is unset (local development).
	DefaultOrigin = "http://127.0.0.1:8090"
	// DefaultSenderAddress is the fixed HR mailbox the application connects to.
	DefaultSenderAddress = "hr@bachtelcorporation.com"
	// DefaultAuthorityURL is the public Microsoft Entra authority origin.
	DefaultAuthorityURL = "https://login.microsoftonline.com"
	// DefaultGraphBaseURL is the Microsoft Graph v1.0 base URL.
	DefaultGraphBaseURL = "https://graph.microsoft.com/v1.0"
	// CallbackPath is the Microsoft redirect path served by internal/web.
	CallbackPath = "/connection/callback"
	// EncryptionKeySize is the required AES-256 key length in bytes.
	EncryptionKeySize = 32
)

// Errors reported by Load and EncryptionKey.
var (
	// ErrOriginInvalid reports a malformed EnvOrigin value.
	ErrOriginInvalid = errors.New("APP_ORIGIN must be an absolute http(s) origin such as https://mail.example.com")
	// ErrRedirectInsecure reports an http callback under an https origin.
	ErrRedirectInsecure = errors.New("MS_REDIRECT_URL must use https when APP_ORIGIN uses https")
	// ErrEncryptionKeyMissing reports an unset EnvEncryptionKey.
	ErrEncryptionKeyMissing = errors.New("APP_ENCRYPTION_KEY is not set")
	// ErrEncryptionKeyInvalid reports an EnvEncryptionKey that is not base64
	// for exactly 32 bytes.
	ErrEncryptionKeyInvalid = errors.New("APP_ENCRYPTION_KEY must be base64 for exactly 32 bytes")
	// ErrRestoreReviewInvalid reports an unparseable restore-review switch.
	ErrRestoreReviewInvalid = errors.New("APP_RESTORE_REVIEW must be true or false")
	// ErrIntakeLimitInvalid reports an unparseable or out-of-range intake
	// resource limit.
	ErrIntakeLimitInvalid = errors.New("intake resource limits must be whole byte counts: INTAKE_MAX_UPLOAD_BYTES must be positive and no larger than the supported ceiling, and INTAKE_UNZIP_XML_SIZE_LIMIT must not exceed INTAKE_UNZIP_SIZE_LIMIT")
)

// Config is the application configuration.
type Config struct {
	// Origin is the normalized public origin without a trailing slash.
	Origin string
	// Microsoft holds the Microsoft connection settings. Incomplete settings
	// disable the connection without preventing the server from starting.
	Microsoft Microsoft
	// PandocPath is the DOCX converter executable. Empty means "pandoc" on
	// PATH; a missing converter is reported where a body document is needed.
	PandocPath string
	// Intake bounds one uploaded task workbook: upload size and expanded
	// archive limits. Values are always resolved, never zero.
	Intake intake.Limits
	// RestoreReview keeps candidate dispatch disabled after a backup restore
	// until the operator reconciles submissions that postdate the backup.
	RestoreReview bool
}

// Microsoft holds the Microsoft Entra application and mailbox settings.
type Microsoft struct {
	TenantID      string
	ClientID      string
	ClientSecret  string
	RedirectURL   string
	SenderAddress string
	AuthorityURL  string
	GraphBaseURL  string
}

// Load reads configuration from the environment. Values needed only by the
// Microsoft connection may be absent: the connection reports an actionable
// configuration error instead of blocking startup. Malformed values fail.
func Load() (Config, error) {
	origin, err := normalizeOrigin(env(EnvOrigin, DefaultOrigin))
	if err != nil {
		return Config{}, err
	}

	microsoft := Microsoft{
		TenantID:      strings.TrimSpace(os.Getenv(EnvTenantID)),
		ClientID:      strings.TrimSpace(os.Getenv(EnvClientID)),
		ClientSecret:  os.Getenv(EnvClientSecret),
		RedirectURL:   strings.TrimSpace(env(EnvRedirectURL, origin+CallbackPath)),
		SenderAddress: strings.ToLower(strings.TrimSpace(env(EnvSenderAddress, DefaultSenderAddress))),
		AuthorityURL:  strings.TrimSuffix(strings.TrimSpace(env(EnvAuthorityURL, DefaultAuthorityURL)), "/"),
		GraphBaseURL:  strings.TrimSuffix(strings.TrimSpace(env(EnvGraphBaseURL, DefaultGraphBaseURL)), "/"),
	}
	if microsoft.RedirectURL == "" {
		return Config{}, fmt.Errorf("%s must not be empty", EnvRedirectURL)
	}
	if parsed, err := url.Parse(microsoft.RedirectURL); err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return Config{}, fmt.Errorf("%s must be an absolute http(s) URL", EnvRedirectURL)
	}
	if strings.HasPrefix(origin, "https://") && !strings.HasPrefix(microsoft.RedirectURL, "https://") {
		return Config{}, ErrRedirectInsecure
	}

	intakeLimits, err := loadIntakeLimits()
	if err != nil {
		return Config{}, err
	}
	restoreReview, err := strconv.ParseBool(strings.TrimSpace(os.Getenv(EnvRestoreReview)))
	if err != nil && strings.TrimSpace(os.Getenv(EnvRestoreReview)) != "" {
		return Config{}, ErrRestoreReviewInvalid
	}

	return Config{
		Origin:        origin,
		Microsoft:     microsoft,
		PandocPath:    strings.TrimSpace(os.Getenv(EnvPandocPath)),
		Intake:        intakeLimits,
		RestoreReview: restoreReview,
	}, nil
}

// loadIntakeLimits reads the workbook resource limits. Empty values keep the
// documented defaults; a malformed value fails startup instead of silently
// running with a bound the operator did not choose.
func loadIntakeLimits() (intake.Limits, error) {
	limits := intake.DefaultLimits()
	for _, setting := range []struct {
		name  string
		field *int64
	}{
		{EnvIntakeMaxUploadBytes, &limits.MaxUploadBytes},
		{EnvIntakeUnzipSizeLimit, &limits.UnzipSizeLimit},
		{EnvIntakeUnzipXMLSizeLimit, &limits.UnzipXMLSizeLimit},
	} {
		raw := strings.TrimSpace(os.Getenv(setting.name))
		if raw == "" {
			continue
		}
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			return intake.Limits{}, fmt.Errorf("%s: %w", setting.name, ErrIntakeLimitInvalid)
		}
		*setting.field = value
	}
	resolved, err := intake.NormalizeLimits(limits)
	if err != nil {
		return intake.Limits{}, fmt.Errorf("%w: %v", ErrIntakeLimitInvalid, err)
	}
	return resolved, nil
}

// SecureCookies reports whether browser cookies must be restricted to secure
// transport. It follows the configured origin scheme.
func (c Config) SecureCookies() bool {
	return strings.HasPrefix(c.Origin, "https://")
}

// EncryptionKey decodes EnvEncryptionKey. A missing or malformed key is an
// error rather than a crash so stored ciphertext is preserved and the operator
// receives an actionable message.
func (c Config) EncryptionKey() ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv(EnvEncryptionKey))
	if raw == "" {
		return nil, ErrEncryptionKeyMissing
	}
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if key, err := encoding.DecodeString(raw); err == nil && len(key) == EncryptionKeySize {
			return key, nil
		}
	}
	return nil, ErrEncryptionKeyInvalid
}

// Endpoints returns the tenant-specific Microsoft authorization-code
// endpoints (consent URL and token URL).
func (m Microsoft) Endpoints() (authorizeURL, tokenURL string) {
	base := m.AuthorityURL + "/" + url.PathEscape(m.TenantID) + "/oauth2/v2.0"
	return base + "/authorize", base + "/token"
}

// Missing returns the names of the required settings that are absent. An empty
// result means the connection can be configured.
func (m Microsoft) Missing() []string {
	var missing []string
	for name, value := range map[string]string{
		EnvTenantID:      m.TenantID,
		EnvClientID:      m.ClientID,
		EnvClientSecret:  m.ClientSecret,
		EnvRedirectURL:   m.RedirectURL,
		EnvSenderAddress: m.SenderAddress,
	} {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, name)
		}
	}
	slices.Sort(missing)
	return missing
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func normalizeOrigin(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", ErrOriginInvalid
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", ErrOriginInvalid
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return "", ErrOriginInvalid
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}
