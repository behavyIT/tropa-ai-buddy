// Package storetest provides an isolated PostgreSQL schema per test.
package storetest

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"

	"tropa/internal/store"
)

// DSN returns the test database DSN.
func DSN() string {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://localhost:5432/tropa_test?sslmode=disable"
}

// New returns a migrated store bound to a fresh schema that is dropped after the test.
func New(t testing.TB) *store.Store {
	t.Helper()
	ctx := context.Background()
	admin, err := store.Open(ctx, DSN(), "")
	if err != nil {
		t.Skipf("PostgreSQL недоступен (%v): задайте TEST_DATABASE_URL или запустите `make test-docker`", err)
	}
	schema := store.SchemaName(fmt.Sprintf("t_%s_%d", t.Name(), rand.IntN(1_000_000_000)))
	if len(schema) > 60 {
		schema = schema[len(schema)-60:]
		schema = "t" + schema[1:]
	}
	if err := admin.CreateSchema(ctx, schema); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, DSN(), schema)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		st.Close()
		_ = admin.DropSchema(context.Background(), schema)
		admin.Close()
	})
	return st
}
