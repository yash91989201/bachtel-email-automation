package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

// The reset path is destructive, so it must be proven against disposable
// directories only: it refuses a directory another dispatcher owns, refuses a
// path that is not a PocketBase data directory, and removes exactly the data
// while leaving the directory ready for a fresh start.
func TestResetLocalData(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "data.db"), []byte("sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	storage := filepath.Join(dir, "storage", "uploads")
	if err := os.MkdirAll(storage, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storage, "source.xlsx"), []byte("bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := resetLocalData(dir); err != nil {
		t.Fatalf("reset disposable data: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != ".sending.lock" {
			t.Fatalf("%s survived the reset", entry.Name())
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "data.db"), []byte("sqlite"), 0o600); err != nil {
		t.Fatalf("reset must leave a writable directory: %v", err)
	}
}

func TestResetLocalDataRefusesRunningDispatcher(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "data.db"), []byte("sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	release, err := sending.LockDataDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := release(); err != nil {
			t.Error(err)
		}
	}()

	if err := resetLocalData(dir); err == nil {
		t.Fatal("reset while a dispatcher owns the directory must fail")
	}
	if _, err := os.Stat(filepath.Join(dir, "data.db")); err != nil {
		t.Fatalf("refused reset removed data: %v", err)
	}
}

func TestResetLocalDataRejectsForeignDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := resetLocalData(dir); err == nil {
		t.Fatal("a directory without data.db must be refused")
	}
	if err := resetLocalData(""); err == nil {
		t.Fatal("an empty path must be refused")
	}
}
