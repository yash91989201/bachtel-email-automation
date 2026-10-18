// Package operator_test drives provisioning through the real migrations, so it
// runs in an external test package to break the migrations -> operator cycle.
package operator_test

import (
	"errors"
	"testing"

	"github.com/pocketbase/pocketbase"
	_ "github.com/yash91989201/bachtel-email-automation/internal/migrations"
	"github.com/yash91989201/bachtel-email-automation/internal/operator"
)

func newApp(t *testing.T) *pocketbase.PocketBase {
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

func TestProvisionCreatesSingleOperator(t *testing.T) {
	app := newApp(t)

	if total, err := app.CountRecords(operator.CollectionName); err != nil || total != 0 {
		t.Fatalf("expected no seeded operator, got %d (err=%v)", total, err)
	}

	if err := operator.Provision(app, "Operator@BachtelCorporation.com", "correct-horse-battery"); err != nil {
		t.Fatal(err)
	}

	record, err := app.FindAuthRecordByEmail(operator.CollectionName, "operator@bachtelcorporation.com")
	if err != nil {
		t.Fatal(err)
	}
	if !record.ValidatePassword("correct-horse-battery") {
		t.Fatal("provisioned password does not verify")
	}
	if record.ValidatePassword("wrong-password") {
		t.Fatal("wrong password verified")
	}
	if !record.Verified() {
		t.Fatal("provisioned operator should be verified")
	}

	// Repeated provisioning must never add a second account.
	if err := operator.Provision(app, "second@bachtelcorporation.com", "another-password"); !errors.Is(err, operator.ErrAlreadyProvisioned) {
		t.Fatalf("second provision: got %v, want operator.ErrAlreadyProvisioned", err)
	}
	if total, err := app.CountRecords(operator.CollectionName); err != nil || total != 1 {
		t.Fatalf("expected exactly one operator, got %d (err=%v)", total, err)
	}
}

func TestProvisionRejectsInvalidInput(t *testing.T) {
	app := newApp(t)

	for _, tc := range []struct {
		name     string
		email    string
		password string
	}{
		{"missing email", "  ", "correct-horse-battery"},
		{"missing password", "operator@bachtelcorporation.com", ""},
		{"password below PocketBase policy", "operator@bachtelcorporation.com", "short"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := operator.Provision(app, tc.email, tc.password); err == nil {
				t.Fatal("expected an error")
			}
			if total, err := app.CountRecords(operator.CollectionName); err != nil || total != 0 {
				t.Fatalf("rejected provisioning created %d accounts (err=%v)", total, err)
			}
		})
	}
}

// The operator collection must stay unreachable through the public PocketBase API.
func TestOperatorCollectionIsLockedDown(t *testing.T) {
	app := newApp(t)

	collection, err := app.FindCollectionByNameOrId(operator.CollectionName)
	if err != nil {
		t.Fatal(err)
	}
	if !collection.IsAuth() {
		t.Fatal("operator collection must be an auth collection")
	}
	for name, rule := range map[string]*string{
		"list":   collection.ListRule,
		"view":   collection.ViewRule,
		"create": collection.CreateRule,
		"update": collection.UpdateRule,
		"delete": collection.DeleteRule,
		"auth":   collection.AuthRule,
	} {
		if rule != nil {
			t.Fatalf("%s rule must stay nil (admin only), got %q", name, *rule)
		}
	}
}
