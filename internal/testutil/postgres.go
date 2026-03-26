// Package testutil provides shared test helpers.
package testutil

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/kamilpajak/heisenberg/internal/database"
)

const (
	testDBName = "heisenberg_test"
	testDBUser = "heisenberg"
	testDBPass = "heisenberg"
)

// NewTestDB returns a connected database for integration tests.
// If DATABASE_URL is set (CI), it uses that directly.
// Otherwise, it starts a PostgreSQL container via testcontainers.
// Migrations are run automatically.
func NewTestDB(t *testing.T) *database.DB {
	t.Helper()

	dbURL := PostgresURL(t)

	err := database.Migrate(dbURL)
	require.NoError(t, err)

	ctx := context.Background()
	db, err := database.New(ctx, dbURL)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	return db
}

// PostgresURL returns a PostgreSQL connection string for integration tests.
// If DATABASE_URL is set (CI), it returns that.
// Otherwise, it starts a PostgreSQL container via testcontainers and returns its URL.
// This function does not import the database package, so it can be used from
// database package tests without circular dependencies.
func PostgresURL(t *testing.T) string {
	t.Helper()

	if url := os.Getenv("DATABASE_URL"); url != "" {
		return url
	}

	return startContainer(t)
}

func startContainer(t *testing.T) string {
	t.Helper()

	ctx := context.Background()

	container, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase(testDBName),
		postgres.WithUsername(testDBUser),
		postgres.WithPassword(testDBPass),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		if err := container.Terminate(ctx); err != nil {
			t.Logf("failed to terminate postgres container: %v", err)
		}
	})

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	return connStr
}
