package web

// Test helpers for the final-sending-dataset boundary. They build a sending
// sheet from the task's own source workbook exactly as an operator would after
// downloading the prepared sheet.

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/xuri/excelize/v2"

	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/finalization"
)

// finalizeTask accepts an edited final sheet built from the staged task's own
// source values. It replaces the selection with a deterministic snapshot so a
// later test can be bound to it.
func finalizeTask(t *testing.T, app core.App, handler http.Handler, cookie *http.Cookie, campaignID, taskID string) {
	t.Helper()
	sheet, err := finalSheetFromTask(app, taskID)
	if err != nil {
		t.Fatalf("build final sheet: %v", err)
	}
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("action_id", "finalize-"+taskID+"-123456"); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("workbook", "final.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(sheet); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/campaigns/"+campaignID+"/tasks/"+taskID+"/finalization/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("finalize task: status=%d body=%.600s", rec.Code, rec.Body.String())
	}
}

// readTaskFile reads one private file field through the PocketBase filesystem.
func readTaskFile(app core.App, record *core.Record, field string) (data []byte, err error) {
	fsys, err := app.NewFilesystem()
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, fsys.Close()) }()
	reader, err := fsys.GetReader(record.BaseFilesPath() + "/" + record.GetString(field))
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, reader.Close()) }()
	return io.ReadAll(reader)
}

// firstEligibleMember returns one accepted final-snapshot member so a test can
// submit a snapshot-bound formatting test.
func firstEligibleMember(t *testing.T, app core.App, taskID string) string {
	t.Helper()
	record, err := app.FindFirstRecordByFilter("task_candidates", "task = {:task} && state = 'eligible'", map[string]any{"task": taskID})
	if err != nil {
		t.Fatalf("find eligible member: %v", err)
	}
	return record.Id
}

// finalSheetFromTask maps the task's source workbook into the deterministic
// sending-sheet layout.
func finalSheetFromTask(app core.App, taskID string) (data []byte, err error) {
	task, err := app.FindRecordById("send_tasks", taskID)
	if err != nil {
		return nil, err
	}
	configuration, err := app.FindRecordById(campaign.ConfigurationsCollection, task.GetString("configuration"))
	if err != nil {
		return nil, err
	}
	var mappings []campaign.Mapping
	if err := configuration.UnmarshalJSONField("mappings", &mappings); err != nil {
		return nil, err
	}
	source, err := readTaskFile(app, task, "source")
	if err != nil {
		return nil, err
	}
	book, err := excelize.OpenReader(bytes.NewReader(source))
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, book.Close()) }()
	grid, err := book.GetRows(task.GetString("sheet"))
	if err != nil {
		return nil, err
	}
	headerRow := task.GetInt("header_row") - 1
	header := []string{}
	if headerRow >= 0 && headerRow < len(grid) {
		header = grid[headerRow]
	}
	columns := make(map[string]int, len(mappings))
	for _, mapping := range mappings {
		for index, label := range header {
			if strings.TrimSpace(label) == strings.TrimSpace(mapping.Header) && columns[mapping.Variable] == 0 {
				columns[mapping.Variable] = index
				break
			}
		}
	}
	out := excelize.NewFile()
	defer func() { err = errors.Join(err, out.Close()) }()
	if err := out.SetSheetName("Sheet1", finalization.SendingSheetName); err != nil {
		return nil, err
	}
	labels := campaign.MappingLabels(mappings)
	for index, label := range labels {
		cell, err := excelize.CoordinatesToCellName(index+1, 1)
		if err != nil {
			return nil, err
		}
		if err := out.SetCellStr(finalization.SendingSheetName, cell, label); err != nil {
			return nil, err
		}
	}
	row := 2
	for _, sourceRow := range grid[headerRow+1:] {
		blank := true
		values := make([]string, 0, len(mappings))
		for _, mapping := range mappings {
			value := ""
			if column := columns[mapping.Variable]; column < len(sourceRow) {
				value = sourceRow[column]
			}
			if strings.TrimSpace(value) != "" {
				blank = false
			}
			values = append(values, value)
		}
		if blank {
			continue
		}
		for index, value := range values {
			cell, err := excelize.CoordinatesToCellName(index+1, row)
			if err != nil {
				return nil, err
			}
			if err := out.SetCellStr(finalization.SendingSheetName, cell, value); err != nil {
				return nil, err
			}
		}
		row++
	}
	buffer, err := out.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
