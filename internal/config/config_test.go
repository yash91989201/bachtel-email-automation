package config

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

// clearEnvironment removes ambient connection settings so tests are hermetic.
func clearEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		EnvOrigin, EnvEncryptionKey, EnvTenantID, EnvClientID, EnvClientSecret,
		EnvRedirectURL, EnvSenderAddress, EnvAuthorityURL, EnvGraphBaseURL,
	} {
		t.Setenv(name, "")
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
	if got, want := cfg.Microsoft.RedirectURL, DefaultOrigin+CallbackPath; got != want {
		t.Fatalf("redirect url = %q, want %q", got, want)
	}
	if got := cfg.Microsoft.SenderAddress; got != DefaultSenderAddress {
		t.Fatalf("sender = %q, want %q", got, DefaultSenderAddress)
	}
	if missing := cfg.Microsoft.Missing(); len(missing) != 3 {
		t.Fatalf("expected tenant/client/secret to be reported missing, got %v", missing)
	}
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

func TestLoadHttpsOriginRequiresSecureCallback(t *testing.T) {
	t.Setenv(EnvOrigin, "https://mail.example.com")
	t.Setenv(EnvRedirectURL, "http://mail.example.com/connection/callback")
	if _, err := Load(); !errors.Is(err, ErrRedirectInsecure) {
		t.Fatalf("got %v, want ErrRedirectInsecure", err)
	}

	t.Setenv(EnvRedirectURL, "https://mail.example.com/connection/callback")
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
}

func TestLoadNormalizesTrailingSlash(t *testing.T) {
	t.Setenv(EnvOrigin, "https://mail.example.com/")
	t.Setenv(EnvRedirectURL, "https://mail.example.com/connection/callback")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Origin != "https://mail.example.com" {
		t.Fatalf("origin = %q, want no trailing slash", cfg.Origin)
	}
	if want := "https://mail.example.com/connection/callback"; cfg.Microsoft.RedirectURL != want {
		t.Fatalf("redirect url = %q, want %q", cfg.Microsoft.RedirectURL, want)
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
	authorize, token := Microsoft{TenantID: "tenant-id", AuthorityURL: DefaultAuthorityURL}.Endpoints()
	if want := DefaultAuthorityURL + "/tenant-id/oauth2/v2.0/authorize"; authorize != want {
		t.Fatalf("authorize = %q, want %q", authorize, want)
	}
	if want := DefaultAuthorityURL + "/tenant-id/oauth2/v2.0/token"; token != want {
		t.Fatalf("token = %q, want %q", token, want)
	}
}
