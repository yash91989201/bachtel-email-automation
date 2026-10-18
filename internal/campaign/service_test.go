// Package campaign_test exercises the campaign service against a real
// PocketBase app on a temporary data directory.
package campaign_test

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"

	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/convert"
	_ "github.com/yash91989201/bachtel-email-automation/internal/migrations"
)

func newTestApp(t *testing.T) core.App {
	t.Helper()
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.ClearBootstrap(); err != nil {
			t.Error(err)
		}
	})
	if err := app.RunAppMigrations(); err != nil {
		t.Fatal(err)
	}
	return app
}

// newService returns a service whose converter path is unusable, proving the
// paths that must not need one (validation, labels, revision history).
func newService(t *testing.T, app core.App) *campaign.Service {
	t.Helper()
	return campaign.NewServiceWithConverter(app, convert.Converter{Path: "definitely-not-a-real-pandoc-binary"})
}

func TestCreateCampaignValidation(t *testing.T) {
	t.Parallel()
	service := newService(t, newTestApp(t))
	ctx := context.Background()

	for name, input := range map[string]campaign.CreateCampaignInput{
		"missing name":         {RecipientHeader: "Email"},
		"missing recipient":    {Name: "Engineers"},
		"duplicate labels":     {Name: "Engineers", RecipientHeader: "Email", ResultLabels: []string{"Status", "Status"}},
		"whitespace only name": {Name: "  ", RecipientHeader: "Email"},
	} {
		t.Run(name, func(t *testing.T) {
			if created, err := service.CreateCampaign(ctx, input); err == nil {
				t.Fatalf("CreateCampaign(%+v) = %+v, want an error", input, created)
			}
		})
	}
}

func TestCreateCampaignDefaultsAndListing(t *testing.T) {
	t.Parallel()
	service := newService(t, newTestApp(t))
	ctx := context.Background()

	created, err := service.CreateCampaign(ctx, campaign.CreateCampaignInput{
		Name:            " Software Engineers ",
		RecipientHeader: " Email ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "Software Engineers" || created.RecipientHeader != "Email" {
		t.Fatalf("campaign = %+v, want trimmed values", created)
	}
	if len(created.ResultLabels) != len(campaign.DefaultResultLabels) {
		t.Fatalf("result labels = %v, want the defaults", created.ResultLabels)
	}
	// A blank entry is dropped rather than becoming an empty column.
	withBlank, err := service.CreateCampaign(ctx, campaign.CreateCampaignInput{
		Name:            "Data",
		RecipientHeader: "Email",
		ResultLabels:    []string{"Status", "  "},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(withBlank.ResultLabels) != 1 || withBlank.ResultLabels[0] != "Status" {
		t.Fatalf("result labels = %v, want the blank entry dropped", withBlank.ResultLabels)
	}
	if created.CurrentRevisionID != "" {
		t.Fatal("a new campaign already points at a configuration")
	}
	if _, err := service.CurrentConfiguration(ctx, created.ID); err == nil {
		t.Fatal("a campaign without a configuration reported a current configuration")
	}

	listed, err := service.ListCampaigns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 {
		t.Fatalf("listed = %+v, want both created campaigns", listed)
	}
	found := false
	for _, listedCampaign := range listed {
		if listedCampaign.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("listed = %+v, want the created campaign", listed)
	}
}

func TestCreateRevisionRejectsUnsupportedUploads(t *testing.T) {
	t.Parallel()
	service := newService(t, newTestApp(t))
	ctx := context.Background()

	created, err := service.CreateCampaign(ctx, campaign.CreateCampaignInput{Name: "Engineers", RecipientHeader: "Email"})
	if err != nil {
		t.Fatal(err)
	}
	mappings := []campaign.Mapping{{Variable: "candidate_name", Header: "Applicant Full Name", Required: true, Format: campaign.FormatName}}

	for name, input := range map[string]campaign.RevisionInput{
		"no document": {
			Subject:  "Hello {{candidate_name}}",
			Mappings: mappings,
		},
		"not a docx": {
			Subject:      "Hello {{candidate_name}}",
			Document:     []byte("this is not a zip package"),
			DocumentName: "notes.txt",
			Mappings:     mappings,
		},
		"macro document": {
			Subject:      "Hello {{candidate_name}}",
			Document:     macroDocument(t),
			DocumentName: "body.docx",
			Mappings:     mappings,
		},
		"empty subject": {
			Subject:      "   ",
			Document:     nil,
			DocumentName: "body.docx",
			Mappings:     mappings,
		},
		"unmapped variable": {
			Subject:      "Hello {{candidate_name}}",
			Document:     []byte("this is not a zip package"),
			DocumentName: "body.docx",
			Mappings:     []campaign.Mapping{{Variable: "1nvalid", Header: "Header", Required: true, Format: campaign.FormatText}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			revision, warnings, err := service.CreateRevision(ctx, created.ID, input)
			if err == nil {
				t.Fatalf("CreateRevision accepted %s (warnings %v)", name, warnings)
			}
			if revision != nil {
				t.Fatalf("a rejected revision returned a configuration: %+v", revision)
			}
		})
	}

	// Nothing was stored and the campaign stays unusable for sending.
	revisions, err := service.Revisions(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 0 {
		t.Fatalf("revisions = %+v, want none", revisions)
	}
	loaded, err := service.LoadCampaign(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.CurrentRevisionID != "" {
		t.Fatal("a rejected revision became the current configuration")
	}
}

func TestResultLabelChangeKeepsContentApproval(t *testing.T) {
	t.Parallel()
	app := newTestApp(t)
	service := newService(t, app)
	ctx := context.Background()

	campaignRecord, err := service.CreateCampaign(ctx, campaign.CreateCampaignInput{Name: "Engineers", RecipientHeader: "Email"})
	if err != nil {
		t.Fatal(err)
	}
	mappings := []campaign.Mapping{{Variable: "candidate_name", Header: "Applicant Full Name", Required: true, Format: campaign.FormatName}}
	document := []byte("stored docx bytes")
	hash := campaign.ContentHash("Hello {{candidate_name}}", document, mappings, "pandoc 3.10")

	revision := storeRevision(t, app, campaignRecord.ID, 1,
		"Hello {{candidate_name}}", "<p>Hello {{candidate_name}}</p>", mappings, hash)

	if err := service.UpdateResultLabels(ctx, campaignRecord.ID, []string{"Status", "Submitted"}); err != nil {
		t.Fatal(err)
	}
	if err := service.UpdateResultLabels(ctx, campaignRecord.ID, []string{"Status", "Status"}); err == nil {
		t.Fatal("duplicate result labels were accepted")
	}

	loaded, err := service.LoadConfiguration(ctx, revision)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ContentHash != hash {
		t.Fatalf("content hash = %q, want the unchanged %q", loaded.ContentHash, hash)
	}
	if loaded.ApprovedHash != "" || loaded.ApprovalCurrent() {
		t.Fatal("an unapproved revision reported a current approval")
	}
	updated, err := service.LoadCampaign(ctx, campaignRecord.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.ResultLabels) != 2 || updated.ResultLabels[0] != "Status" || updated.ResultLabels[1] != "Submitted" {
		t.Fatalf("result labels = %v, want the updated labels", updated.ResultLabels)
	}
	if updated.CurrentRevisionID != revision {
		t.Fatalf("current configuration = %q, want %q unchanged", updated.CurrentRevisionID, revision)
	}
}

func TestRevisionsAreImmutableAndOrdered(t *testing.T) {
	t.Parallel()
	app := newTestApp(t)
	service := newService(t, app)
	ctx := context.Background()

	created, err := service.CreateCampaign(ctx, campaign.CreateCampaignInput{Name: "Engineers", RecipientHeader: "Email"})
	if err != nil {
		t.Fatal(err)
	}
	mappings := []campaign.Mapping{{Variable: "candidate_name", Header: "Applicant Full Name", Required: true, Format: campaign.FormatName}}
	first := storeRevision(t, app, created.ID, 1, "First {{candidate_name}}", "<p>First {{candidate_name}}</p>", mappings, "hash-1")
	second := storeRevision(t, app, created.ID, 2, "Second {{candidate_name}}", "<p>Second {{candidate_name}}</p>", mappings, "hash-2")

	revisions, err := service.Revisions(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 2 || revisions[0].ID != second || revisions[1].ID != first {
		t.Fatalf("revisions = %+v, want newest first", revisions)
	}
	// Editing the current revision is not a service operation: the older
	// revision keeps its stored subject, body and hash.
	loaded, err := service.LoadConfiguration(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Subject != "First {{candidate_name}}" || loaded.ContentHash != "hash-1" {
		t.Fatalf("historical revision changed: %+v", loaded)
	}
}

func TestConverterStatusReportsMissingConverter(t *testing.T) {
	t.Parallel()
	service := newService(t, newTestApp(t))
	err := service.ConverterStatus(context.Background())
	if err == nil {
		t.Skip("a pandoc executable is installed on this machine")
	}
	if !errors.Is(err, convert.ErrConverterMissing) {
		t.Fatalf("error = %v, want ErrConverterMissing", err)
	}
}

// storeRevision writes a revision record directly, which is how tests exercise
// pages and behaviour that need a stored revision without a DOCX converter.
func storeRevision(t *testing.T, app core.App, campaignID string, revision int, subject, bodyHTML string, mappings []campaign.Mapping, hash string) string {
	t.Helper()
	collection, err := app.FindCollectionByNameOrId(campaign.ConfigurationsCollection)
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(collection)
	record.Set("campaign", campaignID)
	record.Set("revision", revision)
	record.Set("subject", subject)
	record.Set("body_html", bodyHTML)
	record.Set("mappings", mappings)
	record.Set("content_hash", hash)
	record.Set("converter", "pandoc test")
	document, err := filesystem.NewFileFromBytes([]byte("stored docx bytes"), "body.docx")
	if err != nil {
		t.Fatal(err)
	}
	record.Set("docx", document)
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	parent, err := app.FindRecordById(campaign.CampaignsCollection, campaignID)
	if err != nil {
		t.Fatal(err)
	}
	parent.Set("current_configuration", record.Id)
	if err := app.Save(parent); err != nil {
		t.Fatal(err)
	}
	return record.Id
}

// macroDocument is a DOCX package that also contains a VBA project: it must be
// refused instead of converted.
func macroDocument(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for name, content := range map[string]string{
		"word/document.xml":   `<?xml version="1.0"?><w:document xmlns:w="w"><w:body/></w:document>`,
		"word/vbaProject.bin": "macro bytes",
	} {
		writer, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
