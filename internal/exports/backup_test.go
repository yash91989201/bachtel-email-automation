package exports_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/pocketbase/pocketbase"

	"github.com/yash91989201/bachtel-email-automation/internal/exports"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	_ "github.com/yash91989201/bachtel-email-automation/internal/migrations"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

// TestStoppedVolumeBackupRestoresFilesAndAttemptReferences verifies the
// documented restore boundary: a stopped data-directory copy contains the
// database, retained sources, generated downloads and attempt references.
func TestStoppedVolumeBackupRestoresFilesAndAttemptReferences(t *testing.T) {
	f := newExportFixture(t)
	task := f.mixedTask(t)
	if err := f.service.GenerateTask(context.Background(), f.ownerID, task, false); err != nil {
		t.Fatal(err)
	}
	if err := f.service.GenerateCampaign(context.Background(), f.ownerID, f.campaignID, true); err != nil {
		t.Fatal(err)
	}
	attempts := f.records(t, sending.AttemptsCollection, "")
	if len(attempts) == 0 {
		t.Fatal("fixture produced no attempts")
	}
	artifact, err := f.service.TaskArtifact(context.Background(), f.ownerID, task)
	if err != nil || artifact == nil || !artifact.Ready {
		t.Fatalf("task artifact missing before backup: %v %+v", err, artifact)
	}

	// Stop the application before copying, exactly as the documented stopped
	// volume procedure requires.
	if err = f.app.ClearBootstrap(); err != nil {
		t.Fatal(err)
	}
	source := f.app.DataDir()
	restoredDir := t.TempDir()
	if err = os.CopyFS(restoredDir, os.DirFS(source)); err != nil {
		t.Fatal(err)
	}

	restored := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: restoredDir})
	if err = restored.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := restored.ClearBootstrap(); err != nil {
			t.Error(err)
		}
	})
	if err = restored.RunAppMigrations(); err != nil {
		t.Fatal(err)
	}

	restoredTask, err := restored.FindRecordById(intake.TasksCollection, task)
	if err != nil {
		t.Fatalf("restored task missing: %v", err)
	}
	if restoredTask.GetString("status") != "completed" {
		t.Fatalf("restored task status = %q", restoredTask.GetString("status"))
	}
	restoredAttempts, err := restored.FindRecordsByFilter(sending.AttemptsCollection, "", "created", 0, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(restoredAttempts) != len(attempts) {
		t.Fatalf("restored attempts %d, want %d", len(restoredAttempts), len(attempts))
	}
	for _, attempt := range restoredAttempts {
		if member := attempt.GetString("member"); member != "" {
			if _, err := restored.FindRecordById(intake.TaskCandidatesCollection, member); err != nil {
				t.Fatalf("restored attempt %s lost its member reference: %v", attempt.Id, err)
			}
		}
		if candidate := attempt.GetString("candidate"); candidate != "" {
			if _, err := restored.FindRecordById(intake.CandidatesCollection, candidate); err != nil {
				t.Fatalf("restored attempt %s lost its candidate reference: %v", attempt.Id, err)
			}
		}
	}

	// Retained source, manifest and generated artifact files come back through
	// the restored record's private filesystem path.
	restoredExports := exports.NewService(restored, f.cfg)
	artifact, err = restoredExports.TaskArtifact(context.Background(), f.ownerID, task)
	if err != nil || artifact == nil || !artifact.Ready {
		t.Fatalf("restored artifact missing: %v %+v", err, artifact)
	}
	if _, _, err = restoredExports.DownloadTask(context.Background(), f.ownerID, task); err != nil {
		t.Fatalf("restored results file is unreadable: %v", err)
	}
	fsys, err := restored.NewFilesystem()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := fsys.Close(); err != nil {
			t.Error(err)
		}
	}()
	for _, field := range []string{"source", "manifest", "excluded"} {
		record, err := restored.FindRecordById(intake.TasksCollection, task)
		if err != nil {
			t.Fatal(err)
		}
		if record.GetString(field) == "" {
			t.Fatalf("%s file reference missing after restore", field)
		}
		reader, err := fsys.GetReader(record.BaseFilesPath() + "/" + record.GetString(field))
		if err != nil {
			t.Fatalf("%s file missing after restore: %v", field, err)
		}
		if err := reader.Close(); err != nil {
			t.Error(err)
		}
	}
	if _, err := os.Stat(filepath.Join(restoredDir, "data.db")); err != nil {
		t.Fatalf("restored database file missing: %v", err)
	}
}
