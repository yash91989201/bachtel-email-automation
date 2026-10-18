// Package operator owns the single application operator account.
//
// The account lives in a dedicated PocketBase auth collection whose API rules
// deny every public operation, so the only ways to create it are this package
// (used by the provision-operator command) and PocketBase superusers.
package operator

import (
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// CollectionName is the PocketBase auth collection that holds the operator.
const CollectionName = "operators"

// ErrAlreadyProvisioned is returned when an operator already exists. Repeated
// provisioning never adds a second account.
var ErrAlreadyProvisioned = errors.New("an operator is already provisioned")

// Provision creates the single operator account using PocketBase password
// handling (bcrypt hashing and token-key generation). No credentials are ever
// seeded by migrations or code defaults.
func Provision(app core.App, email, password string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return errors.New("operator email is required")
	}
	if password == "" {
		return errors.New("operator password is required")
	}

	return app.RunInTransaction(func(txApp core.App) error {
		total, err := txApp.CountRecords(CollectionName)
		if err != nil {
			return err
		}
		if total > 0 {
			return ErrAlreadyProvisioned
		}

		collection, err := txApp.FindCollectionByNameOrId(CollectionName)
		if err != nil {
			return fmt.Errorf("operator collection is missing; run migrations first: %w", err)
		}

		record := core.NewRecord(collection)
		record.SetEmail(email)
		record.SetPassword(password)
		// There is no out-of-band verification channel (no SMTP in this
		// deployment), so the provisioned account is marked verified.
		record.SetVerified(true)

		return txApp.Save(record)
	})
}
