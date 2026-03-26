package testutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPostgresURL_SharedAcrossCalls(t *testing.T) {
	var urls [3]string
	for i := 0; i < 3; i++ {
		urls[i] = PostgresURL(t)
	}
	// All calls should return the same URL (same container reused)
	assert.Equal(t, urls[0], urls[1])
	assert.Equal(t, urls[1], urls[2])
}

func TestPostgresURL_RespectsEnvironment(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://fake:fake@localhost:5432/fake")
	url := PostgresURL(t)
	assert.Equal(t, "postgres://fake:fake@localhost:5432/fake", url)
}
