package preparation

import "strconv"

// Derive builds the prepared dataset and its exclusion evidence from validated
// row results. A shared finding holds every recipient occurrence of its source
// row; a recipient-scoped finding holds only the named address. Resolved
// corrections never change an outcome.
func Derive(input Input, results []RowResult) Output {
	output := Output{Version: OutputVersion, Rows: make([]RowResult, 0, len(results)), Entries: []Entry{}}
	recipient, hasRecipient := input.RecipientField()
	for _, result := range results {
		output.Rows = append(output.Rows, result)
		output.Counts.SourceRows++
		output.Counts.Corrections += len(result.Corrections)
		output.Counts.Findings += len(result.Findings)
		if !hasRecipient {
			continue
		}
		occurrences := preparedOccurrences(result.Values[recipient.ID])
		shared, perPosition := findingsByScope(result.Findings)
		for index, address := range occurrences {
			entry := Entry{
				SourceRow: result.SourceRow,
				Position:  index + 1,
				Recipient: address,
				Outcome:   OutcomeAccepted,
			}
			reasons := append([]string(nil), shared...)
			reasons = append(reasons, perPosition[index+1]...)
			if len(reasons) > 0 {
				entry.Outcome = OutcomeHeld
				entry.Reasons = reasons
				output.Counts.Held++
			} else {
				output.Counts.Accepted++
			}
			output.Counts.Entries++
			output.Entries = append(output.Entries, entry)
		}
	}
	return output
}

// FindingReason is the fixed operator-facing explanation of one finding.
func FindingReason(finding Finding) string { return findingReason(finding) }

// findingsByScope splits one row's findings into row-wide reasons and
// per-address reasons, both already rendered for operators.
func findingsByScope(findings []Finding) ([]string, map[int][]string) {
	var shared []string
	perPosition := make(map[int][]string)
	for _, finding := range findings {
		reason := findingReason(finding)
		if finding.Scope == ScopeRecipient {
			perPosition[finding.Position] = append(perPosition[finding.Position], reason)
			continue
		}
		shared = append(shared, reason)
	}
	return shared, perPosition
}

// findingReason is the fixed operator-facing explanation of one finding.
func findingReason(finding Finding) string {
	header := finding.Header
	if header == "" {
		header = finding.Field
	}
	if finding.Scope == ScopeRecipient {
		return header + " (address " + strconv.Itoa(finding.Position) + "): " + finding.Note
	}
	return header + ": " + finding.Note
}
