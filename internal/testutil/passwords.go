// Package testutil provides fixtures for tests using isolated temporary data.
package testutil

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"golang.org/x/crypto/bcrypt"
)

// FastPasswords retains real password hashing and validation while avoiding
// production bcrypt work in fixtures. Call only on a test-owned temporary app,
// after migrations and before provisioning. Operator and migration tests keep
// the production defaults.
func FastPasswords(t testing.TB, app core.App) {
	t.Helper()
	collection, err := app.FindCollectionByNameOrId("operators")
	if err != nil {
		t.Fatal(err)
	}
	field, ok := collection.Fields.GetByName(core.FieldNamePassword).(*core.PasswordField)
	if !ok {
		t.Fatal("operator collection has no password field")
	}
	field.Cost = bcrypt.MinCost
	if err := app.Save(collection); err != nil {
		t.Fatal(err)
	}
}
