package preparation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ParseResponse validates one complete assistant response against the captured
// input. It rejects unknown, omitted, repeated or mistyped rows and fields,
// invalid finding scopes and any changed, fabricated or dropped recipient
// occurrence. No part of an invalid response is usable.
func ParseResponse(text string, input Input) ([]RowResult, error) {
	normalized := stripFence(text)
	if normalized == "" {
		return nil, invalid("the response was empty")
	}
	if err := checkDuplicateKeys([]byte(normalized)); err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return nil, invalid("the response repeats a key: " + err.Error())
		}
		return nil, invalid("the response is not valid JSON")
	}
	top, err := decodeObject([]byte(normalized))
	if err != nil {
		return nil, invalid("the response is not one JSON object")
	}
	if len(top) != 1 {
		return nil, invalid("the response must contain only the rows property")
	}
	rawRows, ok := top["rows"]
	if !ok {
		return nil, invalid("the response has no rows property")
	}
	var encoded []json.RawMessage
	if err := json.Unmarshal(rawRows, &encoded); err != nil {
		return nil, invalid("the rows property is not an array")
	}
	byID := make(map[string]RowResult, len(input.Rows))
	for _, raw := range encoded {
		result, err := parseRow(raw, input)
		if err != nil {
			return nil, err
		}
		id := strconv.Itoa(result.SourceRow)
		if _, duplicate := byID[id]; duplicate {
			return nil, invalid("the response repeats row " + id)
		}
		byID[id] = result
	}
	rows := make([]RowResult, 0, len(input.Rows))
	for _, row := range input.Rows {
		result, ok := byID[row.ID]
		if !ok {
			return nil, invalid("the response omits row " + row.ID)
		}
		rows = append(rows, result)
	}
	return rows, nil
}

// parseRow validates one response row against the input row with the same id.
func parseRow(raw json.RawMessage, input Input) (RowResult, error) {
	object, err := decodeObject(raw)
	if err != nil {
		return RowResult{}, invalid("a row is not an object")
	}
	for key := range object {
		switch key {
		case "id", "values", "corrections", "findings":
		default:
			return RowResult{}, invalid("a row has an unknown property " + key)
		}
	}
	idText, err := decodeString(object["id"], "row id")
	if err != nil {
		return RowResult{}, err
	}
	sourceRow, err := strconv.Atoi(idText)
	if err != nil || sourceRow < 1 {
		return RowResult{}, invalid("a row id is not a source row number")
	}
	inputRow, ok := findInputRow(input, idText)
	if !ok {
		return RowResult{}, invalid("the response has an unknown row " + idText)
	}
	values, err := decodeValues(object["values"], input)
	if err != nil {
		return RowResult{}, err
	}
	corrections, err := decodeCorrections(object["corrections"], input, sourceRow)
	if err != nil {
		return RowResult{}, err
	}
	findings, err := decodeFindings(object["findings"], input, inputRow, sourceRow)
	if err != nil {
		return RowResult{}, err
	}
	recipient, ok := input.RecipientField()
	if !ok {
		return RowResult{}, invalid("the captured input has no recipient field")
	}
	if !occurrencesPreserved(inputRow.Values[recipient.ID], values[recipient.ID]) {
		return RowResult{}, invalid("row " + idText + " changed, fabricated or dropped a recipient occurrence")
	}
	return RowResult{SourceRow: sourceRow, Values: values, Corrections: corrections, Findings: findings}, nil
}

func findInputRow(input Input, id string) (InputRow, bool) {
	for _, row := range input.Rows {
		if row.ID == id {
			return row, true
		}
	}
	return InputRow{}, false
}

// decodeValues validates complete, exact field coverage with string values.
func decodeValues(raw json.RawMessage, input Input) (map[string]string, error) {
	object, err := decodeObject(raw)
	if err != nil {
		return nil, invalid("a row has no values object")
	}
	for key := range object {
		if _, known := input.FieldByID(key); !known {
			return nil, invalid("a row returns an unknown field " + key)
		}
	}
	if len(object) != len(input.Fields) {
		return nil, invalid("a row does not return every mapped field")
	}
	values := make(map[string]string, len(object))
	for key, value := range object {
		text, err := decodeString(value, "field "+key)
		if err != nil {
			return nil, err
		}
		if len(text) > MaxValueLength {
			return nil, invalid("a returned value is longer than the supported cell bound")
		}
		values[key] = text
	}
	for _, field := range input.Fields {
		if _, ok := values[field.ID]; !ok {
			return nil, invalid("a row omits field " + field.ID)
		}
	}
	return values, nil
}

func decodeCorrections(raw json.RawMessage, input Input, row int) ([]Correction, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var notes []json.RawMessage
	if err := json.Unmarshal(raw, &notes); err != nil {
		return nil, invalid("corrections is not an array")
	}
	if len(notes) > MaxNotesPerRow {
		return nil, invalid("a row returns too many corrections")
	}
	corrections := make([]Correction, 0, len(notes))
	for _, note := range notes {
		field, text, err := decodeNote(note, input, "correction", false)
		if err != nil {
			return nil, err
		}
		corrections = append(corrections, Correction{Row: row, Field: field.ID, Header: field.Header, Note: text})
	}
	return corrections, nil
}

func decodeFindings(raw json.RawMessage, input Input, inputRow InputRow, row int) ([]Finding, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var notes []json.RawMessage
	if err := json.Unmarshal(raw, &notes); err != nil {
		return nil, invalid("findings is not an array")
	}
	if len(notes) > MaxNotesPerRow {
		return nil, invalid("a row returns too many findings")
	}
	recipient, hasRecipient := input.RecipientField()
	occurrences := []string(nil)
	if hasRecipient {
		occurrences = RecipientOccurrences(inputRow.Values[recipient.ID])
	}
	findings := make([]Finding, 0, len(notes))
	seen := make(map[string]struct{}, len(notes))
	for _, note := range notes {
		object, err := decodeObject(note)
		if err != nil {
			return nil, invalid("a finding is not an object")
		}
		for key := range object {
			switch key {
			case "field", "scope", "position", "note":
			default:
				return nil, invalid("a finding has an unknown property " + key)
			}
		}
		field, text, err := decodeNote(note, input, "finding", true)
		if err != nil {
			return nil, err
		}
		scope, err := decodeString(object["scope"], "finding scope")
		if err != nil {
			return nil, err
		}
		finding := Finding{Row: row, Field: field.ID, Header: field.Header, Scope: scope, Note: text}
		switch scope {
		case ScopeShared:
			if _, present := object["position"]; present {
				return nil, invalid("a shared finding must not name an address position")
			}
		case ScopeRecipient:
			if !hasRecipient || field.ID != recipient.ID {
				return nil, invalid("a recipient finding must name the recipient field")
			}
			position, err := decodePosition(object["position"])
			if err != nil {
				return nil, err
			}
			if position < 1 || position > len(occurrences) {
				return nil, invalid("a recipient finding names a position outside the recipient cell")
			}
			finding.Position = position
		default:
			return nil, invalid("a finding has an unsupported scope")
		}
		key := field.ID + "|" + finding.Scope + "|" + strconv.Itoa(finding.Position)
		if _, duplicate := seen[key]; duplicate {
			return nil, invalid("a row repeats a finding")
		}
		seen[key] = struct{}{}
		findings = append(findings, finding)
	}
	return findings, nil
}

// decodeNote reads the shared field/note members of one correction or finding.
func decodeNote(raw json.RawMessage, input Input, kind string, finding bool) (Field, string, error) {
	object, err := decodeObject(raw)
	if err != nil {
		return Field{}, "", invalid("a " + kind + " is not an object")
	}
	for key := range object {
		switch key {
		case "field", "note":
		case "scope", "position":
			if !finding {
				return Field{}, "", invalid("a correction has an unknown property " + key)
			}
		default:
			return Field{}, "", invalid("a " + kind + " has an unknown property " + key)
		}
	}
	fieldID, err := decodeString(object["field"], kind+" field")
	if err != nil {
		return Field{}, "", err
	}
	field, known := input.FieldByID(fieldID)
	if !known {
		return Field{}, "", invalid("a " + kind + " names an unknown field")
	}
	note, err := decodeString(object["note"], kind+" note")
	if err != nil {
		return Field{}, "", err
	}
	if strings.TrimSpace(note) == "" || len(note) > MaxNoteLength {
		return Field{}, "", invalid("a " + kind + " note is empty or longer than the supported bound")
	}
	return field, note, nil
}

func decodePosition(raw json.RawMessage) (int, error) {
	if len(raw) == 0 {
		return 0, invalid("a recipient finding needs an address position")
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err != nil {
		return 0, invalid("a finding position is not a number")
	}
	position, err := strconv.Atoi(number.String())
	if err != nil {
		return 0, invalid("a finding position is not a whole number")
	}
	return position, nil
}

// decodeObject requires a JSON object and returns its members.
func decodeObject(raw []byte) (map[string]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, invalid("the value is not an object")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &object); err != nil {
		return nil, invalid("the object could not be read")
	}
	return object, nil
}

// decodeString requires a JSON string; null and mistyped values are refused.
func decodeString(raw json.RawMessage, what string) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return "", invalid(what + " is missing or null")
	}
	var text string
	if err := json.Unmarshal(trimmed, &text); err != nil {
		return "", invalid(what + " is not a string")
	}
	return text, nil
}

// checkDuplicateKeys rejects repeated object keys at every depth. encoding/json
// silently keeps the last member of a repeated key, which would let a response
// claim complete coverage while repeating a field.
func checkDuplicateKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := scanValue(decoder); err != nil {
		return err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err == nil {
		return fmt.Errorf("trailing content")
	}
	return nil
}

func scanValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("an object key is not a string")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate key %q", key)
			}
			seen[key] = struct{}{}
			if err := scanValue(decoder); err != nil {
				return err
			}
		}
		_, err := decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := scanValue(decoder); err != nil {
				return err
			}
		}
		_, err := decoder.Token()
		return err
	default:
		return fmt.Errorf("unexpected delimiter")
	}
}

// stripFence removes one surrounding Markdown code fence, which some models
// add despite the instruction to return JSON only.
func stripFence(text string) string {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}
	if newline := strings.IndexByte(trimmed, '\n'); newline >= 0 {
		trimmed = trimmed[newline+1:]
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(trimmed), "```"))
}

type invalidResponse struct{ reason string }

func (e *invalidResponse) Error() string { return e.reason }

// Unwrap exposes ErrInvalidResponse so callers can classify the failure
// without matching text.
func (e *invalidResponse) Unwrap() error { return ErrInvalidResponse }

func invalid(reason string) error { return &invalidResponse{reason: reason} }

// InvalidReason reports the fixed diagnostic reason of an invalid response for
// server logs. Operator-facing text always uses ErrInvalidResponse.
func InvalidReason(err error) string {
	if invalidErr, ok := errors.AsType[*invalidResponse](err); ok {
		return invalidErr.reason
	}
	return ""
}
