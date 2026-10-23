package testutil_test

import (
	"testing"

	"github.com/yash91989201/bachtel-email-automation/internal/operator"
	"github.com/yash91989201/bachtel-email-automation/internal/testutil"
	"golang.org/x/crypto/bcrypt"
)

func TestNewAppIsolation(t *testing.T) {
	for _, name := range []string{"first", "second"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			app := testutil.NewApp(t)
			if count, err := app.CountRecords(operator.CollectionName); err != nil || count != 0 {
				t.Fatalf("fixture contains %d operators: %v", count, err)
			}
			// Identical identities must work in concurrent, independent copies.
			const email, password = "fixture@example.test", "correct-horse-battery"
			if err := operator.Provision(app, email, password); err != nil {
				t.Fatal(err)
			}
			record, err := app.FindAuthRecordByEmail(operator.CollectionName, email)
			if err != nil {
				t.Fatal(err)
			}
			if !record.ValidatePassword(password) || record.ValidatePassword("wrong-password") {
				t.Fatal("fixture bypasses password verification")
			}
			if cost, err := bcrypt.Cost([]byte(record.GetString("password:hash"))); err != nil || cost != bcrypt.MinCost {
				t.Fatalf("fixture bcrypt cost = %d: %v", cost, err)
			}
		})
	}
}
