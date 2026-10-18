// Package campaign owns reusable campaign email configuration: immutable
// content revisions, the mapping table, field normalization and the single
// rendering path shared by previews, test emails and candidate submissions.
//
// Nothing in this package sends email, and no configuration becomes usable for
// sending until it validates. Candidate data is never stored here: values enter
// only as arguments to Render.
package campaign

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

const (
	// CampaignsCollection holds campaigns and their output settings.
	CampaignsCollection = "campaigns"
	// ConfigurationsCollection holds immutable content revisions.
	ConfigurationsCollection = "campaign_configurations"
	// DocxMimeType is the only accepted body document type.
	DocxMimeType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	// MaxDocumentBytes bounds an uploaded body document.
	MaxDocumentBytes = 10 << 20
	// MaxSubjectLength is the practical limit for a single-line subject.
	MaxSubjectLength = 998
)

// DefaultResultLabels are the downloadable column labels a task snapshots when
// the operator names none. They are output settings: changing them never
// invalidates content approval.
var DefaultResultLabels = []string{"Email Status", "Submitted At", "Email Error"}

// Format selects the replacement-value normalization for one mapping.
type Format string

const (
	// FormatText preserves capitalization and only normalizes whitespace.
	FormatText Format = "text"
	// FormatName applies word capitalization without acronym promotion.
	FormatName Format = "name"
	// FormatDesignation applies word capitalization plus known acronyms.
	FormatDesignation Format = "designation"
	// FormatEmail trims and lowercases an address.
	FormatEmail Format = "email"
)

// Formats lists every supported normalization choice.
var Formats = []Format{FormatText, FormatName, FormatDesignation, FormatEmail}

// Valid reports whether the format is supported.
func (f Format) Valid() bool { return slices.Contains(Formats, f) }

// Mapping binds one user-facing variable to a source column and its
// replacement policy. Variables default to required; an optional variable may
// declare a fallback.
type Mapping struct {
	Variable string `json:"variable"`
	Header   string `json:"header"`
	Required bool   `json:"required"`
	Fallback string `json:"fallback"`
	Format   Format `json:"format"`
}

// Variable describes one compiled placeholder and where it is used.
type Variable struct {
	Name    string `json:"name"`
	Key     string `json:"key"`
	Subject bool   `json:"subject"`
	Body    bool   `json:"body"`
}

// Campaign is a named email configuration with output settings.
type Campaign struct {
	ID                string
	Name              string
	RecipientHeader   string
	ResultLabels      []string
	CurrentRevisionID string
	Revision          int
}

// Configuration is one immutable content revision.
type Configuration struct {
	ID           string
	CampaignID   string
	Revision     int
	Subject      string
	BodyHTML     string
	Mappings     []Mapping
	Variables    []Variable
	ContentHash  string
	Converter    string
	ApprovedHash string

	// Compiled is the rendering template built from Subject, BodyHTML and
	// Mappings. It is rebuilt whenever the revision is loaded.
	Compiled *Template
}

// ApprovalCurrent reports whether the stored content approval covers exactly
// this revision's content. A content edit always produces a new revision, so a
// false result means the revision has never been approved or was invalidated.
func (c *Configuration) ApprovalCurrent() bool {
	return c.ApprovedHash != "" && c.ApprovedHash == c.ContentHash
}

// Validation errors.
var (
	// ErrNoMappings reports a configuration with no mapped variables.
	ErrNoMappings = errors.New("at least one variable mapping is required")
	// ErrNoBody reports a configuration without a body document.
	ErrNoBody = errors.New("a DOCX body document is required")
	// ErrSubjectEmpty reports a subject that is empty after trimming.
	ErrSubjectEmpty = errors.New("a subject is required")
)

var (
	variableNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)
	// controlPattern matches characters that must never reach a mail header.
	controlPattern = regexp.MustCompile(`[\x00-\x1f\x7f]`)
)

// ValidateSubject rejects an empty subject and any header control character so
// a candidate value can never inject a header. The subject is a plain text
// field: a carriage return or newline is always an error, never a folding hint.
func ValidateSubject(subject string) error {
	if strings.TrimSpace(subject) == "" {
		return ErrSubjectEmpty
	}
	if len(subject) > MaxSubjectLength {
		return fmt.Errorf("subject must be at most %d characters", MaxSubjectLength)
	}
	if controlPattern.MatchString(subject) {
		return errors.New("subject must not contain control characters or line breaks")
	}
	return nil
}

// ValidateVariableName checks a user-facing placeholder name. Names are plain
// identifiers so a placeholder can never be read as a template expression.
func ValidateVariableName(name string) error {
	if !variableNamePattern.MatchString(name) {
		return fmt.Errorf("variable %q must start with a letter and contain only letters, digits and underscores", name)
	}
	return nil
}

// ValidateMappings checks the mapping table: one row per variable, unique
// variables, exact header names and a supported format. A fallback is only
// meaningful for an optional variable, so declaring one on a required variable
// is rejected instead of being silently ignored.
func ValidateMappings(mappings []Mapping) error {
	if len(mappings) == 0 {
		return ErrNoMappings
	}
	seen := make(map[string]struct{}, len(mappings))
	for i, mapping := range mappings {
		variable := strings.TrimSpace(mapping.Variable)
		if err := ValidateVariableName(variable); err != nil {
			return err
		}
		if _, ok := seen[variable]; ok {
			return fmt.Errorf("variable %q is mapped more than once", variable)
		}
		seen[variable] = struct{}{}
		if strings.TrimSpace(mapping.Header) == "" {
			return fmt.Errorf("variable %q needs a source header", variable)
		}
		if !mapping.Format.Valid() {
			return fmt.Errorf("variable %q has unsupported formatting %q", variable, mapping.Format)
		}
		if mapping.Required && strings.TrimSpace(mapping.Fallback) != "" {
			return fmt.Errorf("variable %q is required, so it cannot have a fallback", mapping.Variable)
		}
		mappings[i].Variable = variable
		mappings[i].Header = strings.TrimSpace(mapping.Header)
	}
	return nil
}

// ValidateResultLabels checks the output column labels: at least one, none
// empty, and no duplicates, so a task export always has unambiguous columns.
func ValidateResultLabels(labels []string) error {
	if len(labels) == 0 {
		return errors.New("at least one result column label is required")
	}
	seen := make(map[string]struct{}, len(labels))
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label == "" {
			return errors.New("result column labels must not be empty")
		}
		if len(label) > 100 {
			return fmt.Errorf("result column label %q is longer than 100 characters", label)
		}
		if _, ok := seen[label]; ok {
			return fmt.Errorf("result column label %q is used more than once", label)
		}
		seen[label] = struct{}{}
	}
	return nil
}

// NormalizeResultLabels trims the labels and substitutes the defaults when the
// operator names none.
func NormalizeResultLabels(labels []string) []string {
	normalized := make([]string, 0, len(labels))
	for _, label := range labels {
		if label = strings.TrimSpace(label); label != "" {
			normalized = append(normalized, label)
		}
	}
	if len(normalized) == 0 {
		return slices.Clone(DefaultResultLabels)
	}
	return normalized
}

// ContentHash identifies exactly the personalized content of a revision: the
// subject, the body document bytes, the mapping table and the converter that
// produced the body. Output labels, names and timestamps are deliberately
// excluded, so a presentation-only change keeps the approval valid.
func ContentHash(subject string, document []byte, mappings []Mapping, converter string) string {
	type canonical struct {
		Subject     string    `json:"subject"`
		Document    string    `json:"document_sha256"`
		Mappings    []Mapping `json:"mappings"`
		Converter   string    `json:"converter"`
		HashVersion string    `json:"hash_version"`
	}
	ordered := slices.Clone(mappings)
	slices.SortFunc(ordered, func(a, b Mapping) int { return strings.Compare(a.Variable, b.Variable) })
	documentSum := sha256.Sum256(document)
	payload, err := json.Marshal(canonical{
		Subject:     subject,
		Document:    hex.EncodeToString(documentSum[:]),
		Mappings:    ordered,
		Converter:   converter,
		HashVersion: "1",
	})
	if err != nil {
		// Mapping values are plain strings and structs; marshaling cannot fail.
		panic(fmt.Sprintf("campaign: marshal content hash input: %v", err))
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
