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
	// EnvAuthorityURL overrides the Microsoft authority origin. It exists only
	// for simulated authorization servers in tests; operators never set it.
	EnvAuthorityURL = "MS_AUTHORITY_URL"
	// EnvGraphBaseURL overrides the Microsoft Graph API base URL. It exists
	// only for simulated Graph servers in tests; operators never set it.
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
	// DefaultAuthorityURL is the public Microsoft Entra authority origin.
	DefaultAuthorityURL = "https://login.microsoftonline.com"
	// DefaultGraphBaseURL is the Microsoft Graph v1.0 base URL.
	DefaultGraphBaseURL = "https://graph.microsoft.com/v1.0"
	// CallbackPath is the Microsoft redirect path served by internal/web.
	CallbackPath = "/connection/callback"
	// EnvTitanSMTPAddress overrides the Titan SMTP endpoint. The UI never
	// accepts a host, and this setting exists for simulated test servers.
	EnvTitanSMTPAddress = "TITAN_SMTP_ADDRESS"
	// DefaultTitanSMTPAddress is GoDaddy's documented outgoing SMTP endpoint
	// for Titan mailboxes: implicit TLS on port 465.
	DefaultTitanSMTPAddress = "smtpout.secureserver.net:465"
	// EncryptionKeySize is the required AES-256 key length in bytes.
	EncryptionKeySize = 32
)

// Errors reported by Load and EncryptionKey.
var (
	// ErrOriginInvalid reports a malformed EnvOrigin value.
	ErrOriginInvalid = errors.New("APP_ORIGIN must be an absolute http(s) origin such as https://mail.example.com")
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
	// Titan holds the GoDaddy Titan SMTP endpoint. It is fixed by the provider;
	// only a test can override it.
	Titan Titan
}

// Microsoft holds the endpoint overrides for the Microsoft connection.
// Tenant, application and secret settings belong to each operator's saved
// provider setup, not to the deployment: the same deployment serves operators
// in different tenants with separate application registrations.
type Microsoft struct {
	// AuthorityURL and GraphBaseURL are simulated-test overrides of the public
	// endpoints. Operators never configure them.
	AuthorityURL string
	GraphBaseURL string
}

// Titan holds the GoDaddy Titan SMTP endpoint shared by every Titan connection.
type Titan struct {
	Address string
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
		AuthorityURL: strings.TrimSuffix(strings.TrimSpace(env(EnvAuthorityURL, DefaultAuthorityURL)), "/"),
		GraphBaseURL: strings.TrimSuffix(strings.TrimSpace(env(EnvGraphBaseURL, DefaultGraphBaseURL)), "/"),
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
		Titan: Titan{
			Address: strings.TrimSpace(env(EnvTitanSMTPAddress, DefaultTitanSMTPAddress)),
		},
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

// CallbackURL is the fixed Microsoft redirect URI every operator registers.
// It is derived from the trusted deployment origin, so no operator-supplied
// redirect can redirect the authorization response elsewhere.
func (c Config) CallbackURL() string {
	return c.Origin + CallbackPath
}

// Endpoints returns the tenant-specific Microsoft authorization-code
// endpoints (consent URL and token URL) for an operator-saved tenant.
func (m Microsoft) Endpoints(tenantID string) (authorizeURL, tokenURL string) {
	base := m.AuthorityURL + "/" + url.PathEscape(tenantID) + "/oauth2/v2.0"
	return base + "/authorize", base + "/token"
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
