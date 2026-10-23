package preparation

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
)

// BuildInput captures the mapped fields and rows of one validated task. Only
// campaign-mapped columns are copied; unmapped workbook data never enters the
// request or the stored input artifact.
func BuildInput(configuration *campaign.Configuration, sourceDigest string, rows []intake.MappedRow) (Input, error) {
	if configuration == nil {
		return Input{}, fmt.Errorf("a campaign content revision is required")
	}
	fields := make([]Field, 0, len(configuration.Mappings))
	known := make(map[string]struct{}, len(configuration.Mappings))
	for _, mapping := range configuration.Mappings {
		if _, ok := known[mapping.Variable]; ok {
			return Input{}, fmt.Errorf("mapping variable %q is repeated", mapping.Variable)
		}
		known[mapping.Variable] = struct{}{}
		fields = append(fields, Field{ID: mapping.Variable, Header: mapping.Header, Recipient: mapping.Recipient})
	}
	input := Input{
		Version:         InputVersion,
		ConfigurationID: configuration.ID,
		ContentHash:     configuration.ContentHash,
		SourceDigest:    sourceDigest,
		Fields:          fields,
		Rows:            make([]InputRow, 0, len(rows)),
	}
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		id := strconv.Itoa(row.Number)
		if _, ok := seen[id]; ok {
			return Input{}, fmt.Errorf("source row %d is repeated", row.Number)
		}
		seen[id] = struct{}{}
		values := make(map[string]string, len(fields))
		for _, field := range fields {
			value, ok := row.Values[field.ID]
			if !ok {
				return Input{}, fmt.Errorf("source row %d has no value for %q", row.Number, field.ID)
			}
			values[field.ID] = value
		}
		input.Rows = append(input.Rows, InputRow{ID: id, Values: values})
	}
	return input, nil
}

// RequestPayload is the user-content payload of one provider request: the
// task-specific instructions, the mapped field metadata and the batch rows.
// Field values stay ordinary data under their ids.
type RequestPayload struct {
	Instructions string     `json:"instructions,omitempty"`
	Fields       []Field    `json:"fields"`
	Rows         []InputRow `json:"rows"`
}

// MarshalPayload encodes one batch request body.
func MarshalPayload(input Input, instructions string, rows []InputRow) (string, error) {
	payload, err := json.Marshal(RequestPayload{Instructions: instructions, Fields: input.Fields, Rows: rows})
	if err != nil {
		return "", fmt.Errorf("encode the preparation payload: %w", err)
	}
	return string(payload), nil
}

// RecipientField returns the single recipient mapping of the input.
func (in Input) RecipientField() (Field, bool) {
	for _, field := range in.Fields {
		if field.Recipient {
			return field, true
		}
	}
	return Field{}, false
}

// FieldByID returns one mapped field.
func (in Input) FieldByID(id string) (Field, bool) {
	for _, field := range in.Fields {
		if field.ID == id {
			return field, true
		}
	}
	return Field{}, false
}

// RecipientOccurrences splits a recipient cell on the recognized address
// separators while preserving every ordered occurrence, including repeated,
// invalid and empty entries. It defines the occurrence identity an AI response
// must retain; only trim and case may differ.
func RecipientOccurrences(cell string) []string {
	var tokens []string
	start := 0
	for i := 0; i < len(cell); i++ {
		if !isAddressSeparator(cell[i]) {
			continue
		}
		tokens = append(tokens, cell[start:i])
		start = i + 1
	}
	return append(tokens, cell[start:])
}

func isAddressSeparator(b byte) bool {
	switch b {
	case ',', ';', '/', '|', '\n', '\r':
		return true
	default:
		return false
	}
}

// normalizedOccurrences reads the canonical comma-separated form of a
// normalized recipient cell.
func normalizedOccurrences(cell string) []string {
	return strings.Split(cell, ",")
}

// occurrencesPreserved reports whether every normalized address maps to the
// original occurrence at the same position apart from surrounding whitespace
// and case. Changed, fabricated and dropped occurrences are all failures.
func occurrencesPreserved(original, normalized string) bool {
	before := RecipientOccurrences(original)
	after := normalizedOccurrences(normalized)
	if len(before) != len(after) {
		return false
	}
	for i := range before {
		if !strings.EqualFold(strings.TrimSpace(before[i]), strings.TrimSpace(after[i])) {
			return false
		}
	}
	return true
}

// preparedOccurrences returns the prepared addresses of one normalized
// recipient cell, one per canonical comma-separated occurrence.
func preparedOccurrences(cell string) []string {
	tokens := normalizedOccurrences(cell)
	prepared := make([]string, 0, len(tokens))
	for _, token := range tokens {
		prepared = append(prepared, strings.TrimSpace(token))
	}
	return prepared
}
