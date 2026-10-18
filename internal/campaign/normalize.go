package campaign

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// acronyms is the documented set of designations that keep their capitals after
// word capitalization. It is intentionally small: an unknown short word is
// never uppercased automatically.
var acronyms = map[string]string{
	"hr":  "HR",
	"it":  "IT",
	"ceo": "CEO",
	"cto": "CTO",
	"cfo": "CFO",
	"coo": "COO",
	"qa":  "QA",
}

// Acronyms lists the recognized designations for documentation and tests.
func Acronyms() []string {
	values := make([]string, 0, len(acronyms))
	for _, value := range acronyms {
		values = append(values, value)
	}
	return values
}

// titleCase applies word capitalization without lowercasing existing capitals,
// so "iOS Engineer" keeps its spelling. A caser holds mutable state and is not
// safe for concurrent use, so one is built per call: rendering is concurrent.
func titleCase(value string) string {
	return cases.Title(language.English, cases.NoLower).String(value)
}

// CollapseWhitespace trims surrounding whitespace and collapses internal runs
// of whitespace to single spaces. Every supported replacement value is
// single-line, so this is applied before any format-specific transform.
func CollapseWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

// Normalize applies the format-specific replacement policy to one raw value.
// Formatting never invents a preferred spelling: it only applies the documented
// word capitalization, acronym set, or lowercase rule.
func Normalize(format Format, raw string) string {
	value := CollapseWhitespace(raw)
	switch format {
	case FormatEmail:
		return strings.ToLower(value)
	case FormatName:
		return titleCase(value)
	case FormatDesignation:
		words := strings.Fields(titleCase(value))
		for i, word := range words {
			if acronym, ok := acronyms[strings.ToLower(word)]; ok {
				words[i] = acronym
			}
		}
		return strings.Join(words, " ")
	default: // FormatText and any unset format preserve capitalization.
		return value
	}
}

// ErrRecipientInvalid reports a recipient cell that is not a single usable
// address.
var ErrRecipientInvalid = errors.New("recipient must be a single valid email address")

// NormalizeRecipient validates one recipient cell and returns the trimmed
// lowercase address used as campaign identity. Address lists, display-name
// wrappers and header controls are rejected; no mailbox existence is claimed.
func NormalizeRecipient(raw string) (string, error) {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return "", fmt.Errorf("%w: value is empty", ErrRecipientInvalid)
	}
	if controlPattern.MatchString(candidate) {
		return "", fmt.Errorf("%w: value contains control characters", ErrRecipientInvalid)
	}
	if strings.ContainsAny(candidate, ",;<>") || strings.Contains(candidate, " ") {
		return "", fmt.Errorf("%w: value contains more than one address or a display name", ErrRecipientInvalid)
	}
	address, err := mail.ParseAddress(candidate)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrRecipientInvalid, err)
	}
	if address.Name != "" || address.Address != candidate {
		return "", fmt.Errorf("%w: value contains more than one address or a display name", ErrRecipientInvalid)
	}
	normalized := strings.ToLower(address.Address)
	local, domain, found := strings.Cut(normalized, "@")
	if !found || local == "" || domain == "" || !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return "", fmt.Errorf("%w: %q is not a complete address", ErrRecipientInvalid, candidate)
	}
	return normalized, nil
}
