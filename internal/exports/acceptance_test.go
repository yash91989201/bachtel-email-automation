package exports_test

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/yash91989201/bachtel-email-automation/internal/exports"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

// TestExportThousandRowWorkflow is the release-acceptance download check: a
// 1,000-row task is submitted through the real durable store (no network), and
// every generated download is verified without candidate traffic.
func TestExportThousandRowWorkflow(t *testing.T) {
	f := newExportFixture(t)
	rows := make([][]string, 0, 1000)
	for i := range 1000 {
		rows = append(rows, []string{fmt.Sprintf("candidate%04d@example.invalid", i), fmt.Sprintf("Candidate %04d", i)})
	}
	task := f.stage(t, rows)
	f.approve(t, task)
	act := f.act(t, f.ownerID, task, "start", "thousand-start", 1000)
	if act.Claimed != 1000 {
		t.Fatalf("claimed %d, want 1000", act.Claimed)
	}
	if sent := f.drain(t, map[string]sending.Outcome{}); sent != 1000 {
		t.Fatalf("submitted %d, want 1000", sent)
	}
	if err := f.store.SettleTasks(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := f.service.GenerateTask(context.Background(), f.ownerID, task, false); err != nil {
		t.Fatal(err)
	}
	data, _, err := f.service.DownloadTask(context.Background(), f.ownerID, task)
	if err != nil {
		t.Fatal(err)
	}
	book, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	grid, err := book.GetRows("Candidates")
	if err != nil {
		t.Fatal(err)
	}
	if len(grid) != 1001 {
		t.Fatalf("original rows %d, want 1001", len(grid))
	}
	if grid[1000][1] != "Candidate 0999" {
		t.Fatalf("original source value changed: %v", grid[1000])
	}
	// The original worksheet is untouched; the added recipient-results sheet
	// carries one entry per recipient in source order.
	if width := len(grid[0]); width != 2 {
		t.Fatalf("original header gained columns: %v", grid[0])
	}
	results, err := book.GetRows("Recipient results")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1001 {
		t.Fatalf("results rows %d, want 1001", len(results))
	}
	status, err := book.GetCellValue("Recipient results", "G1001")
	if err != nil {
		t.Fatal(err)
	}
	if status != "Submitted" {
		t.Fatalf("last row status %q, want Submitted", status)
	}
	if err := book.Close(); err != nil {
		t.Error(err)
	}

	if err := f.service.GenerateCampaign(context.Background(), f.ownerID, f.campaignID, true); err != nil {
		t.Fatal(err)
	}
	report, _, err := f.service.DownloadCampaign(context.Background(), f.ownerID, f.campaignID, exports.KindCampaignReport)
	if err != nil {
		t.Fatal(err)
	}
	reportBook, err := excelize.OpenReader(bytes.NewReader(report))
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := reportBook.GetRows("Candidates")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1001 {
		t.Fatalf("report candidates %d, want 1000", len(candidates)-1)
	}
	attempts, err := reportBook.GetRows("Attempts")
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1002 {
		t.Fatalf("report attempts %d, want 1001", len(attempts)-1)
	}
	if err := reportBook.Close(); err != nil {
		t.Error(err)
	}

	pkg, _, err := f.service.DownloadCampaign(context.Background(), f.ownerID, f.campaignID, exports.KindCampaignPackage)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(pkg), int64(len(pkg)))
	if err != nil {
		t.Fatal(err)
	}
	emails, index := 0, 0
	for _, file := range archive.File {
		switch {
		case strings.HasPrefix(file.Name, "emails/"):
			emails++
		case file.Name == "index.csv":
			index++
		}
	}
	if emails != 1001 {
		t.Fatalf("package emails %d, want 1001", emails)
	}
	if index != 1 {
		t.Fatal("package is missing its index")
	}
	for _, file := range archive.File {
		if file.Name != "index.csv" {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		if lines := len(strings.Split(strings.TrimSpace(string(content)), "\n")); lines != 1002 {
			t.Fatalf("package index rows %d, want 1001", lines-1)
		}
	}
}
