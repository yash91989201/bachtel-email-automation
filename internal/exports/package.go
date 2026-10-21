package exports

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"regexp"

	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

// safeEntry allows only PocketBase-style identifiers in archive paths, so no
// stored value can introduce a path separator or traversal segment.
var safeEntry = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// CampaignPackage builds the campaign ZIP from one snapshot: the XLSX report,
// one static HTML file per recorded attempt and a CSV index that maps every
// file to its attempt and candidate.
func CampaignPackage(snapshot *campaignSnapshot, report []byte) (data []byte, err error) {
	out := &limitedBuffer{limit: MaxArtifactBytes}
	archive := zip.NewWriter(out)

	reportFile, err := archive.Create("report.xlsx")
	if err != nil {
		return nil, err
	}
	if _, err = reportFile.Write(report); err != nil {
		return nil, err
	}

	indexes := make([][]string, 0, len(snapshot.Attempts))
	for _, attempt := range snapshot.Attempts {
		id := attempt.Id
		if !safeEntry.MatchString(id) {
			return nil, fmt.Errorf("attempt id %q is not a safe archive entry", id)
		}
		entry := "emails/" + id + ".html"
		file, err := archive.Create(entry)
		if err != nil {
			return nil, err
		}
		html, err := attemptHTML(attempt)
		if err != nil {
			return nil, err
		}
		if _, err = file.Write(html); err != nil {
			return nil, err
		}
		resolution := snapshot.Resolutions[attempt.GetString("member")]
		basis, resolvedAt := "", ""
		if resolution != nil {
			basis = resolution.GetString("basis")
			resolvedAt = sending.DisplayTime(resolution.GetDateTime("resolved_at").Time().UTC())
		}
		indexes = append(indexes, []string{
			attempt.Id,
			attempt.GetString("kind"),
			attempt.GetString("task"),
			attempt.GetString("candidate"),
			attempt.GetString("recipient"),
			attempt.GetString("subject"),
			attempt.GetString("state"),
			attempt.GetString("reason"),
			attempt.GetString("sender"),
			resolvedAt,
			basis,
			entry,
		})
	}

	indexFile, err := archive.Create("index.csv")
	if err != nil {
		return nil, err
	}
	index := csv.NewWriter(indexFile)
	header := []string{"attempt_id", "kind", "task_id", "candidate_id", "recipient", "subject", "state", "reason", "sender", "resolved_at", "resolution_basis", "file"}
	if err = index.Write(header); err != nil {
		return nil, err
	}
	for _, row := range indexes {
		if err = index.Write(row); err != nil {
			return nil, err
		}
	}
	index.Flush()
	if err = index.Error(); err != nil {
		return nil, err
	}
	if err = archive.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// limitedBuffer bounds an in-memory archive before it reaches storage.
type limitedBuffer struct {
	bytes.Buffer
	limit int64
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if int64(b.Len())+int64(len(p)) > b.limit {
		return 0, errors.New("the campaign package exceeds the artifact size bound")
	}
	return b.Buffer.Write(p)
}
