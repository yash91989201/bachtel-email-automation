package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestEveryPooledConnectionUsesDurableCommits(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	ctx := context.Background()
	var conns []*sql.Conn
	defer func() {
		for _, conn := range conns {
			if err := conn.Close(); err != nil {
				t.Error(err)
			}
		}
	}()
	for range 4 {
		conn, err := db.DB().Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, conn)
		var sync int
		if err := conn.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&sync); err != nil {
			t.Fatal(err)
		}
		if sync != 2 {
			t.Fatalf("synchronous = %d; want FULL (2)", sync)
		}
	}
}
