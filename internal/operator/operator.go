// Package operator owns application operator accounts.
//
// Accounts live in a dedicated PocketBase auth collection whose API rules deny
// every public operation, so the only ways to create one are this package (used
// by the provision-operator command) and PocketBase superusers. There is no
// public registration and no operator-to-operator account administration.
package operator

import (
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// CollectionName is the PocketBase auth collection that holds operators.
const CollectionName = "operators"

// ErrEmailTaken is returned when the normalized login identity already exists.
// Provisioning never changes that account's password.
var ErrEmailTaken = errors.New("an operator with that email already exists")

// Provision creates one operator account using PocketBase password handling
// (bcrypt hashing and token-key generation). No credentials are ever seeded by
// migrations or code defaults.
func Provision(app core.App, email, password string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return errors.New("operator email is required")
	}
	if password == "" {
		return errors.New("operator password is required")
	}

	return app.RunInTransaction(func(txApp core.App) error {
		existing, err := txApp.FindAuthRecordByEmail(CollectionName, email)
		if err == nil && existing != nil {
			return ErrEmailTaken
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
