package config

import (
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/yash91989201/bachtel-email-automation/internal/intake"
)

// clearEnvironment removes ambient connection settings so tests are hermetic.
func clearEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		EnvOrigin, EnvEncryptionKey, EnvTitanSMTPAddress, EnvAuthorityURL,
		EnvGraphBaseURL, EnvIntakeMaxUploadBytes, EnvIntakeUnzipSizeLimit,
		EnvIntakeUnzipXMLSizeLimit, EnvRestoreReview,
	} {
		t.Setenv(name, "")
	}
}

// TestLegacySharedMicrosoftSettingsAreIgnored guards the removal of the shared
// deployment credentials: setting them must not configure anything.
func TestLegacySharedMicrosoftSettingsAreIgnored(t *testing.T) {
	clearEnvironment(t)
	for name, value := range map[string]string{
		"MS_TENANT_ID":     "00000000-0000-0000-0000-000000000001",
		"MS_CLIENT_ID":     "00000000-0000-0000-0000-000000000002",
		"MS_CLIENT_SECRET": "shared-secret",
		"MS_REDIRECT_URL":  "https://elsewhere.example.test/callback",
	} {
		t.Setenv(name, value)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if want := DefaultOrigin + CallbackPath; cfg.CallbackURL() != want {
		t.Fatalf("callback = %q, want the origin-derived %q", cfg.CallbackURL(), want)
	}
}

func TestLoadDefaults(t *testing.T) {
	clearEnvironment(t)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Origin != DefaultOrigin {
		t.Fatalf("origin = %q, want %q", cfg.Origin, DefaultOrigin)
	}
	if cfg.SecureCookies() {
		t.Fatal("loopback development origin must not force secure cookies")
	}
	if got, want := cfg.CallbackURL(), DefaultOrigin+CallbackPath; got != want {
		t.Fatalf("callback = %q, want %q", got, want)
	}
	if got := cfg.Titan.Address; got != DefaultTitanSMTPAddress {
		t.Fatalf("titan endpoint = %q, want %q", got, DefaultTitanSMTPAddress)
	}
}

func TestLoadTitanEndpointOverride(t *testing.T) {
	clearEnvironment(t)
	t.Setenv(EnvTitanSMTPAddress, "127.0.0.1:2525")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Titan.Address != "127.0.0.1:2525" {
		t.Fatalf("titan endpoint = %q, want the simulated override", cfg.Titan.Address)
	}
}

func TestLoadRestoreReview(t *testing.T) {
	clearEnvironment(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RestoreReview {
		t.Fatal("restore review must default to disabled")
	}
	t.Setenv(EnvRestoreReview, "true")
	cfg, err = Load()
	if err != nil || !cfg.RestoreReview {
		t.Fatalf("restore review was not enabled: %+v %v", cfg, err)
	}
	t.Setenv(EnvRestoreReview, "not-a-switch")
	if _, err = Load(); !errors.Is(err, ErrRestoreReviewInvalid) {
		t.Fatalf("malformed restore review accepted: %v", err)
	}
}

func TestLoadIntakeLimits(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		clearEnvironment(t)
		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Intake != intake.DefaultLimits() {
			t.Fatalf("intake limits = %+v, want the documented defaults", cfg.Intake)
		}
	})

	t.Run("configured", func(t *testing.T) {
		clearEnvironment(t)
		t.Setenv(EnvIntakeMaxUploadBytes, "2048")
		t.Setenv(EnvIntakeUnzipSizeLimit, "4096")
		t.Setenv(EnvIntakeUnzipXMLSizeLimit, "1024")
		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		want := intake.Limits{MaxUploadBytes: 2048, UnzipSizeLimit: 4096, UnzipXMLSizeLimit: 1024}
		if cfg.Intake != want {
			t.Fatalf("intake limits = %+v, want %+v", cfg.Intake, want)
		}
	})

	t.Run("rejected", func(t *testing.T) {
		cases := map[string]map[string]string{
			"not a number":           {EnvIntakeMaxUploadBytes: "10MiB"},
			"zero":                   {EnvIntakeMaxUploadBytes: "0"},
			"negative":               {EnvIntakeMaxUploadBytes: "-1"},
			"above the ceiling":      {EnvIntakeMaxUploadBytes: strconv.Itoa(intake.MaxUploadBytes + 1)},
			"xml part above archive": {EnvIntakeUnzipSizeLimit: "1024", EnvIntakeUnzipXMLSizeLimit: "2048"},
		}
		for name, env := range cases {
			t.Run(name, func(t *testing.T) {
				clearEnvironment(t)
				for key, value := range env {
					t.Setenv(key, value)
				}
				if _, err := Load(); !errors.Is(err, ErrIntakeLimitInvalid) {
					t.Fatalf("got %v, want ErrIntakeLimitInvalid", err)
				}
			})
		}
	})
}

func TestLoadRejectsMalformedOrigin(t *testing.T) {
	for _, origin := range []string{
		"mail.example.com",
		"ftp://mail.example.com",
		"https://mail.example.com/app",
		"https://mail.example.com?x=1",
	} {
		t.Run(origin, func(t *testing.T) {
			t.Setenv(EnvOrigin, origin)
			if _, err := Load(); !errors.Is(err, ErrOriginInvalid) {
				t.Fatalf("got %v, want ErrOriginInvalid", err)
			}
		})
	}
}

func TestLoadHttpsOriginDerivesSecureCallback(t *testing.T) {
	t.Setenv(EnvOrigin, "https://mail.example.com")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.SecureCookies() {
		t.Fatal("https origin must force secure cookies")
	}
	if cfg.Origin != "https://mail.example.com" {
		t.Fatalf("origin = %q", cfg.Origin)
	}
	if want := "https://mail.example.com/connection/callback"; cfg.CallbackURL() != want {
		t.Fatalf("callback = %q, want %q", cfg.CallbackURL(), want)
	}
}

func TestLoadNormalizesTrailingSlash(t *testing.T) {
	t.Setenv(EnvOrigin, "https://mail.example.com/")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Origin != "https://mail.example.com" {
		t.Fatalf("origin = %q, want no trailing slash", cfg.Origin)
	}
	if want := "https://mail.example.com/connection/callback"; cfg.CallbackURL() != want {
		t.Fatalf("callback = %q, want %q", cfg.CallbackURL(), want)
	}
}

func TestEncryptionKey(t *testing.T) {
	key := strings.Repeat("k", EncryptionKeySize)

	t.Run("missing", func(t *testing.T) {
		t.Setenv(EnvEncryptionKey, "")
		if _, err := Load(); err != nil {
			t.Fatal(err)
		}
		if _, err := (Config{}).EncryptionKey(); !errors.Is(err, ErrEncryptionKeyMissing) {
			t.Fatalf("got %v, want ErrEncryptionKeyMissing", err)
		}
	})

	t.Run("invalid", func(t *testing.T) {
		for _, value := range []string{"not-base64!!", base64.StdEncoding.EncodeToString([]byte("too-short"))} {
			t.Setenv(EnvEncryptionKey, value)
			if _, err := (Config{}).EncryptionKey(); !errors.Is(err, ErrEncryptionKeyInvalid) {
				t.Fatalf("%q: got %v, want ErrEncryptionKeyInvalid", value, err)
			}
		}
	})

	t.Run("valid", func(t *testing.T) {
		t.Setenv(EnvEncryptionKey, base64.StdEncoding.EncodeToString([]byte(key)))
		got, err := (Config{}).EncryptionKey()
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != key {
			t.Fatalf("key round trip mismatch")
		}
	})
}

func TestMicrosoftEndpoints(t *testing.T) {
	authorize, token := Microsoft{AuthorityURL: DefaultAuthorityURL}.Endpoints("tenant-id")
	if want := DefaultAuthorityURL + "/tenant-id/oauth2/v2.0/authorize"; authorize != want {
		t.Fatalf("authorize = %q, want %q", authorize, want)
	}
	if want := DefaultAuthorityURL + "/tenant-id/oauth2/v2.0/token"; token != want {
		t.Fatalf("token = %q, want %q", token, want)
	}
}
