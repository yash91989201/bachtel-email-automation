package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/pocketbase/pocketbase"

	"github.com/yash91989201/bachtel-email-automation/internal/config"
	"github.com/yash91989201/bachtel-email-automation/internal/database"
	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
	_ "github.com/yash91989201/bachtel-email-automation/internal/migrations"
	"github.com/yash91989201/bachtel-email-automation/internal/operator"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

var backupTestKey = []byte("0123456789abcdef0123456789abcdef")

// A backup of the data directory preserves both operators and their exclusive
// mailbox claims; a restored instance starts with dispatch paused because the
// restore-review flag blocks every send-like action.
func TestBackupRestorePreservesOperatorsAndMailboxes(t *testing.T) {
	source := t.TempDir()
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: source, DBConnect: database.Open})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := app.RunAppMigrations(); err != nil {
		t.Fatal(err)
	}
	providers := []struct {
		email  string
		sender string
	}{
		{"first@example.test", "first@example.test"},
		{"second@example.test", "second@example.test"},
	}
	mailboxes := mailbox.NewStore(app, func() ([]byte, error) { return backupTestKey, nil })
	for _, entry := range providers {
		if err := operator.Provision(app, entry.email, "correct-horse-battery"); err != nil {
			t.Fatal(err)
		}
		record, err := app.FindAuthRecordByEmail(operator.CollectionName, entry.email)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = mailboxes.Install(context.Background(), mailbox.Candidate{
			OwnerID:    record.Id,
			Provider:   mailbox.ProviderTitan,
			Sender:     entry.sender,
			Credential: mailbox.Credential{Provider: mailbox.ProviderTitan, Password: "mailbox-password"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := app.ClearBootstrap(); err != nil {
		t.Fatal(err)
	}

	restored := t.TempDir()
	copyTree(t, source, restored)

	app = pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: restored, DBConnect: database.Open})
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
	for _, entry := range providers {
		if _, err := app.FindAuthRecordByEmail(operator.CollectionName, entry.email); err != nil {
			t.Fatalf("restored operator %s: %v", entry.email, err)
		}
	}
	restoredMailboxes := mailbox.NewStore(app, func() ([]byte, error) { return backupTestKey, nil })
	connections, err := restoredMailboxes.ListCurrent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(connections) != 2 {
		t.Fatalf("restored connections = %d, want 2", len(connections))
	}
	for _, connection := range connections {
		credential, err := restoredMailboxes.Credential(context.Background(), connection)
		if err != nil || credential.Password != "mailbox-password" {
			t.Fatalf("restored credential for %s: %+v %v", connection.Sender, credential, err)
		}
	}

	// The restored instance must not send until the operator reconciles the
	// backup gap, even though the restored data itself is intact.
	cfg := config.Config{Origin: "http://127.0.0.1:8090", RestoreReview: true}
	store := sending.NewStore(app, sending.Options{RestoreReview: cfg.RestoreReview})
	work, err := store.NextWork(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if work != nil {
		t.Fatal("restored instance selected work while restore review is active")
	}
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		target := filepath.Join(to, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		source, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() {
			if err := source.Close(); err != nil {
				t.Error(err)
			}
		}()
		destination, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		if _, err = io.Copy(destination, source); err != nil {
			return err
		}
		return destination.Close()
	})
	if err != nil {
		t.Fatal(err)
	}
}
