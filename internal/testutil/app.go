package testutil

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/pocketbase/pocketbase"

	_ "github.com/yash91989201/bachtel-email-automation/internal/migrations"
)

// The cached bytes come from a closed, fully migrated app. Each test gets its
// own writable copy; neither records nor open SQLite connections are shared.
var migratedData = sync.OnceValues(func() (data map[string][]byte, err error) {
	dir, err := os.MkdirTemp("", "bachtel-test-schema-")
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(dir)) }()
	if err := migrate(dir); err != nil {
		return nil, err
	}
	data = make(map[string][]byte, 2)
	for _, name := range []string{"data.db", "auxiliary.db"} {
		data[name], err = os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
	}
	return data, nil
})

func migrate(dir string) (err error) {
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: dir})
	if err := app.Bootstrap(); err != nil {
		return err
	}
	defer func() { err = errors.Join(err, app.ClearBootstrap()) }()
	return app.RunAppMigrations()
}

// NewApp bootstraps an isolated temporary app without replaying all migrations
// for every test. Migration and production provisioning tests use fresh apps
// directly so they still verify the uncached setup and password defaults.
func NewApp(t testing.TB) *pocketbase.PocketBase {
	t.Helper()
	data, err := migratedData()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, contents := range data {
		if err := os.WriteFile(filepath.Join(dir, name), contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: dir})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.ClearBootstrap(); err != nil {
			t.Error(err)
		}
	})
	FastPasswords(t, app)
	return app
}
