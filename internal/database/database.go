// Package database configures the persistent store used by the application.
package database

import (
	"github.com/pocketbase/dbx"
	_ "modernc.org/sqlite"
)

// Open retains PocketBase's SQLite settings while requiring FULL synchronous
// commits on every pooled connection. A committed submission intent must survive
// a host interruption before its network request can begin. Setting the pragma
// only once on a pool would leave later connections at their driver's default.
func Open(path string) (*dbx.DB, error) {
	return dbx.Open("sqlite", path+"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=journal_size_limit(200000000)&_pragma=synchronous(FULL)&_pragma=foreign_keys(ON)&_pragma=temp_store(MEMORY)&_pragma=cache_size(-32000)&_defensive=1")
}
