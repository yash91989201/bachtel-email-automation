package campaign

import (
	"errors"
	"testing"
)

func TestNormalize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		format Format
		raw    string
		want   string
	}{
		{name: "text preserves capitalization", format: FormatText, raw: "iOS Engineer", want: "iOS Engineer"},
		{name: "text collapses whitespace", format: FormatText, raw: "  iOS \t Engineer  ", want: "iOS Engineer"},
		{name: "unset format preserves text", format: "", raw: "  Mixed CASE  ", want: "Mixed CASE"},
		{name: "name capitalizes words", format: FormatName, raw: "john doe", want: "John Doe"},
		{name: "name keeps existing capitals", format: FormatName, raw: "JOHN doe", want: "JOHN Doe"},
		{name: "name does not promote acronyms", format: FormatName, raw: "head of hr", want: "Head Of Hr"},
		{name: "designation promotes acronyms", format: FormatDesignation, raw: "head of hr", want: "Head Of HR"},
		{name: "designation promotes several acronyms", format: FormatDesignation, raw: "qa lead for it and ceo office", want: "QA Lead For IT And CEO Office"},
		{name: "designation leaves ordinary words", format: FormatDesignation, raw: "senior developer", want: "Senior Developer"},
		{name: "email is trimmed and lowercased", format: FormatEmail, raw: "  Ada.Lovelace@Example.COM ", want: "ada.lovelace@example.com"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := Normalize(test.format, test.raw); got != test.want {
				t.Fatalf("Normalize(%q, %q) = %q, want %q", test.format, test.raw, got, test.want)
			}
		})
	}
}

func TestAcronymsAreDocumented(t *testing.T) {
	t.Parallel()
	got := map[string]bool{}
	for _, acronym := range Acronyms() {
		got[acronym] = true
	}
	for _, want := range []string{"HR", "IT", "CEO", "CTO", "CFO", "COO", "QA"} {
		if !got[want] {
			t.Fatalf("acronym %q missing from the documented set %v", want, Acronyms())
		}
	}
}

func TestNormalizeRecipient(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "valid", raw: "ada@example.com", want: "ada@example.com"},
		{name: "case and surrounding whitespace", raw: "  Ada.Lovelace@Example.COM  ", want: "ada.lovelace@example.com"},
		{name: "address list", raw: "ada@example.com, bob@example.com", wantErr: true},
		{name: "semicolon list", raw: "ada@example.com; bob@example.com", wantErr: true},
		{name: "display name wrapper", raw: "Ada Lovelace <ada@example.com>", wantErr: true},
		{name: "two addresses in angle brackets", raw: "<ada@example.com>", wantErr: true},
		{name: "no domain dot", raw: "ada@localhost", wantErr: true},
		{name: "missing local part", raw: "@example.com", wantErr: true},
		{name: "carriage return injection", raw: "ada@example.com\r\nBcc: bob@example.com", wantErr: true},
		{name: "empty", raw: "   ", wantErr: true},
		{name: "internal space", raw: "ada lovelace@example.com", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeRecipient(test.raw)
			if test.wantErr {
				if err == nil {
					t.Fatalf("NormalizeRecipient(%q) = %q, want an error", test.raw, got)
				}
				if !errors.Is(err, ErrRecipientInvalid) {
					t.Fatalf("error = %v, want ErrRecipientInvalid", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("NormalizeRecipient(%q) = %q, want %q", test.raw, got, test.want)
			}
		})
	}
}
