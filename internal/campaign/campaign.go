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
	"unicode/utf8"
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
	// MaxCampaignNameLength is the stored campaign-name bound, measured in
	// Unicode code points like the collection field it mirrors.
	MaxCampaignNameLength = 200
)

// DefaultResultLabels are the downloadable column labels a task snapshots when
// the operator names none. They are output settings: changing them never
// invalidates content approval.
var DefaultResultLabels = []string{"Email Status", "Submitted At", "Email Error"}

// Mapping versions. Legacy revisions stored an explicit format choice; current
// authoring stores no format, so a stored format string can never be misread as
// a fallback.
const (
	// MappingVersionLegacy selects the format-bearing mapping table.
	MappingVersionLegacy = 1
	// MappingVersionRole selects the current role/fallback mapping table.
	MappingVersionRole = 2
	// CurrentMappingVersion is the version every new revision records.
	CurrentMappingVersion = MappingVersionRole
)

// Format selects the replacement-value normalization for one legacy mapping.
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
// replacement policy. Exactly one mapping selects the required recipient;
// ordinary optional variables may declare a fallback.
type Mapping struct {
	Variable  string `json:"variable"`
	Header    string `json:"header"`
	Required  bool   `json:"required"`
	Recipient bool   `json:"recipient"`
	Fallback  string `json:"fallback"`
	// Format is the legacy normalization choice. Current mapping tables leave
	// it empty: values are prepared by AI and then preserved verbatim.
	Format Format `json:"format,omitempty"`
}

// Variable describes one compiled placeholder and where it is used.
type Variable struct {
	Name    string `json:"name"`
	Key     string `json:"key"`
	Subject bool   `json:"subject"`
	Body    bool   `json:"body"`
}

// ErrNotFound reports a campaign or revision that does not exist or belongs to
// another operator. Both refusals are identical so an id can never be probed.
var ErrNotFound = errors.New("campaign not found")

// Campaign is a named email configuration with output settings.
type Campaign struct {
	ID string
	// OwnerID is the operator record that owns this campaign. Revisions,
	// tasks, candidates and attempts inherit the owner through it.
	OwnerID           string
	Name              string
	SourceHeaders     []string
	ResultLabels      []string
	CurrentRevisionID string
	Revision          int
}

// Configuration is one immutable content revision.
type Configuration struct {
	ID         string
	CampaignID string
	Revision   int
	// MappingVersion records how Mappings must be read. Zero is a record
	// written before versions existed and means the legacy format rules.
	MappingVersion int
	Subject        string
	BodyHTML       string
	Mappings       []Mapping
	Variables      []Variable
	ContentHash    string
	Converter      string
	ApprovedHash   string

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
	// ErrCampaignNameRequired reports a campaign name that is empty after
	// trimming. Like a task, a campaign has no default name.
	ErrCampaignNameRequired = errors.New("a campaign name is required")
	// ErrCampaignNameTooLong reports a campaign name beyond the stored bound.
	ErrCampaignNameTooLong = fmt.Errorf("a campaign name can be at most %d characters", MaxCampaignNameLength)
)

var (
	variableNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)
	// controlPattern matches characters that must never reach a mail header.
	controlPattern = regexp.MustCompile(`[\x00-\x1f\x7f]`)
)

// NormalizeCampaignName returns the trimmed campaign name or a fixed
// validation error. The bound counts Unicode code points so a name the browser
// measured in UTF-16 units still fits; duplicates stay valid because campaign
// identity is the record id.
func NormalizeCampaignName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", ErrCampaignNameRequired
	}
	if utf8.RuneCountInString(name) > MaxCampaignNameLength {
		return "", ErrCampaignNameTooLong
	}
	return name, nil
}

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

// ValidateMappings checks a legacy mapping table. It is kept for stored
// revisions and tests that predate mapping versions.
func ValidateMappings(mappings []Mapping) error {
	return ValidateMappingsForVersion(MappingVersionLegacy, mappings)
}

// ValidateMappingsForVersion checks the mapping table for one version: one row
// per variable, unique variables, exactly one required recipient, exact header
// names and a valid fallback. Legacy tables also require a supported format;
// current tables reject a format outright.
func ValidateMappingsForVersion(version int, mappings []Mapping) error {
	if len(mappings) == 0 {
		return ErrNoMappings
	}
	legacy := version < MappingVersionRole
	seen := make(map[string]struct{}, len(mappings))
	recipients := 0
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
		if mapping.Recipient {
			recipients++
			mapping.Required = true
			if legacy {
				if mapping.Format == "" {
					mapping.Format = FormatEmail
				}
				if mapping.Format != FormatEmail {
					return fmt.Errorf("recipient variable %q must use email formatting", variable)
				}
			}
		}
		if legacy {
			if !mapping.Format.Valid() {
				return fmt.Errorf("variable %q has unsupported formatting %q", variable, mapping.Format)
			}
		} else if mapping.Format != "" {
			return fmt.Errorf("variable %q selects formatting %q; mapping formats are no longer supported, so remove that column", variable, mapping.Format)
		}
		if mapping.Required && strings.TrimSpace(mapping.Fallback) != "" {
			return fmt.Errorf("variable %q is required, so it cannot have a fallback", mapping.Variable)
		}
		if mapping.Recipient && strings.TrimSpace(mapping.Fallback) != "" {
			return fmt.Errorf("recipient variable %q cannot have a fallback", variable)
		}
		mapping.Variable = variable
		mapping.Header = strings.TrimSpace(mapping.Header)
		mappings[i] = mapping
	}
	if recipients != 1 {
		return errors.New("mark exactly one mapping row as recipient to select the sending address")
	}
	return nil
}

// DefaultMappings derives the draft mapping table for a campaign that has no
// revision yet: one required mapping per source header. The variable name is
// the header lowercased with every run of non-alphanumeric characters replaced
// by one underscore. A header that cannot form a variable name, and a name that
// repeats an earlier one, is omitted. The operator must explicitly mark one row
// as recipient.
func DefaultMappings(sourceHeaders []string) []Mapping {
	seen := make(map[string]struct{})
	var mappings []Mapping
	for _, header := range sourceHeaders {
		header = strings.TrimSpace(header)
		if header == "" {
			continue
		}
		variable := deriveVariableName(header)
		if variable == "" {
			continue
		}
		if _, ok := seen[variable]; ok {
			continue
		}
		seen[variable] = struct{}{}
		mappings = append(mappings, Mapping{
			Variable: variable,
			Header:   header,
			Required: true,
		})
	}
	return mappings
}

// deriveVariableName spells a header as a placeholder name, or "" when the
// header cannot produce one. Only ASCII letters and digits survive, because
// that is the alphabet a variable name is allowed to use.
func deriveVariableName(header string) string {
	var name strings.Builder
	separator := false
	for _, r := range strings.ToLower(header) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if separator && name.Len() > 0 {
				name.WriteByte('_')
			}
			separator = false
			name.WriteRune(r)
			continue
		}
		separator = true
	}
	derived := name.String()
	if !variableNamePattern.MatchString(derived) {
		return ""
	}
	return derived
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

// MappingLabels returns the deterministic sending-sheet column label of every
// mapping, in mapping order. The source header is used when exactly one
// mapping references it; a shared header is qualified with its variable name.
// Remaining collisions, including a literal header that already has the same
// qualified shape, receive the documented numbered suffix. Export and final
// import use this one table, so no fuzzy matching or parenthesis parsing is
// needed on either side.
func MappingLabels(mappings []Mapping) []string {
	counts := make(map[string]int, len(mappings))
	for _, mapping := range mappings {
		counts[strings.TrimSpace(mapping.Header)]++
	}
	occupied := make(map[string]struct{}, len(mappings))
	labels := make([]string, 0, len(mappings))
	for _, mapping := range mappings {
		header := strings.TrimSpace(mapping.Header)
		base := header
		if counts[header] > 1 || header == "" {
			base = header + " (" + mapping.Variable + ")"
		}
		candidate := base
		for suffix := 2; ; suffix++ {
			if _, taken := occupied[candidate]; !taken {
				break
			}
			candidate = fmt.Sprintf("%s (%d)", base, suffix)
		}
		occupied[candidate] = struct{}{}
		labels = append(labels, candidate)
	}
	return labels
}

// MappingIndex resolves a sending-sheet label to its mapping position. It
// reports whether the label belongs to exactly one mapping; a missing or
// ambiguous label is refused instead of guessing a column.
func MappingIndex(label string, labels []string) (int, bool) {
	found := -1
	for index, candidate := range labels {
		if candidate != label {
			continue
		}
		if found >= 0 {
			return -1, false
		}
		found = index
	}
	return found, found >= 0
}

// ContentHash identifies exactly the personalized content of one legacy
// revision: the subject, the body document bytes, the mapping table and the
// converter that produced the body. Output labels, names and timestamps are
// deliberately excluded, so a presentation-only change keeps the approval
// valid.
func ContentHash(subject string, document []byte, mappings []Mapping, converter string) string {
	return ContentHashForVersion(MappingVersionLegacy, subject, document, mappings, converter)
}

// ContentHashForVersion is ContentHash plus the mapping-table version. A
// versioned table is a different interpretation of the same text, so it must
// never share an identity with a legacy table. The legacy hash omits the
// version field so existing stored hashes keep their historical form.
func ContentHashForVersion(version int, subject string, document []byte, mappings []Mapping, converter string) string {
	type canonical struct {
		Subject        string    `json:"subject"`
		Document       string    `json:"document_sha256"`
		Mappings       []Mapping `json:"mappings"`
		Converter      string    `json:"converter"`
		HashVersion    string    `json:"hash_version"`
		MappingVersion int       `json:"mapping_version,omitempty"`
	}
	ordered := slices.Clone(mappings)
	slices.SortFunc(ordered, func(a, b Mapping) int { return strings.Compare(a.Variable, b.Variable) })
	documentSum := sha256.Sum256(document)
	payload, err := json.Marshal(canonical{
		Subject:        subject,
		Document:       hex.EncodeToString(documentSum[:]),
		Mappings:       ordered,
		Converter:      converter,
		HashVersion:    "1",
		MappingVersion: version,
	})
	if err != nil {
		// Mapping values are plain strings and structs; marshaling cannot fail.
		panic(fmt.Sprintf("campaign: marshal content hash input: %v", err))
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
