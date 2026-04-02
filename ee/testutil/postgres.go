// Package testutil provides shared test helpers.
package testutil

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/kamilpajak/heisenberg/ee/database"
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
// Otherwise, it starts a shared PostgreSQL container via testcontainers.
// This is separate from NewTestDB to allow database package tests
// to get a URL without creating a circular import through NewTestDB.
func PostgresURL(t *testing.T) string {
	t.Helper()

	if url := os.Getenv("DATABASE_URL"); url != "" {
		return url
	}

	return startContainer(t)
}

var (
	sharedURL  string
	sharedOnce sync.Once
	sharedErr  error
)

func startContainer(t *testing.T) string {
	t.Helper()

	sharedOnce.Do(func() {
		ctx := context.Background()

		container, err := postgres.Run(ctx,
			"pgvector/pgvector:pg16",
			postgres.WithDatabase(testDBName),
			postgres.WithUsername(testDBUser),
			postgres.WithPassword(testDBPass),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2).
					WithStartupTimeout(30*time.Second),
			),
		)
		if err != nil {
			sharedErr = err
			return
		}

		// Ryuk sidecar automatically cleans up the container when the process exits.
		sharedURL, sharedErr = container.ConnectionString(ctx, "sslmode=disable")
	})

	if sharedErr != nil {
		t.Skipf("Docker not available: %v", sharedErr)
	}
	return sharedURL
}
