package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
	"github.com/yash91989201/bachtel-email-automation/internal/operator"
)

// The operator collection is deliberately unreachable through the public
// PocketBase API:
//
//   - list/view/create/update/delete rules stay nil, which restricts the record
//     API to PocketBase superusers.
//   - AuthRule stays nil, which disables the public password-authentication
//     endpoint, so internal/web is the only sign-in path (it adds rate limiting,
//     origin checks, and its own session cookie).
//
// Creation happens through the one-time `provision-operator` command, which
// refuses to add a second account.
func init() {
	m.Register(func(app core.App) error {
		collection := core.NewAuthCollection(operator.CollectionName)
		// Auth collections default to an open AuthRule (""), which would expose
		// PocketBase's own password endpoint and bypass the login rate limiting
		// in internal/web. nil restricts authentication to administrators.
		collection.AuthRule = nil
		collection.Fields.Add(&core.TextField{
			Name: "name",
			Max:  100,
		})
		return app.Save(collection)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId(operator.CollectionName)
		if err != nil {
			return err
		}
		return app.Delete(collection)
	})
}
