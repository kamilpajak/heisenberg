package billing

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kamilpajak/heisenberg/internal/database"
	"github.com/kamilpajak/heisenberg/pkg/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testDB returns a connected DB or skips if DATABASE_URL is not set.
// It also ensures migrations are run before tests.
func testDB(t *testing.T) *database.DB {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set")
	}

	// Ensure migrations are run (idempotent)
	err := database.Migrate(dbURL)
	require.NoError(t, err)

	ctx := context.Background()
	db, err := database.New(ctx, dbURL)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	return db
}

func TestUsageChecker_GetUsageStats(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	uc := NewUsageChecker(db)

	// Setup: create user and org
	clerkID := "clerk_" + uuid.New().String()[:8]
	user, err := db.CreateUser(ctx, clerkID, "usage@example.com")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteUser(ctx, user.ID) })

	org, err := db.CreateOrganizationWithOwner(ctx, "Usage Test Org", user.ID)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteOrganization(ctx, org.ID) })

	// Get stats for org with no analyses
	stats, err := uc.GetUsageStats(ctx, org.ID)
	require.NoError(t, err)
	assert.Equal(t, org.ID, stats.OrgID)
	assert.Equal(t, TierFree, stats.Tier)
	assert.Equal(t, 0, stats.UsedThisMonth)
	assert.Equal(t, 10, stats.Limit)
	assert.Equal(t, 10, stats.Remaining)

	// Create a repo and add an analysis
	repo, err := db.CreateRepository(ctx, org.ID, "test", "repo")
	require.NoError(t, err)

	_, err = db.CreateAnalysis(ctx, database.CreateAnalysisParams{
		RepoID:   repo.ID,
		RunID:    1001,
		Category: llm.CategoryDiagnosis,
		Text:     "Test analysis",
	})
	require.NoError(t, err)

	// Stats should now show 1 used
	stats, err = uc.GetUsageStats(ctx, org.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, stats.UsedThisMonth)
	assert.Equal(t, 9, stats.Remaining)
}

func TestUsageChecker_CanAnalyze(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	uc := NewUsageChecker(db)

	// Setup
	clerkID := "clerk_" + uuid.New().String()[:8]
	user, err := db.CreateUser(ctx, clerkID, "cananalyze@example.com")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteUser(ctx, user.ID) })

	org, err := db.CreateOrganizationWithOwner(ctx, "Can Analyze Org", user.ID)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteOrganization(ctx, org.ID) })

	// Free tier should allow analysis
	can, err := uc.CanAnalyze(ctx, org.ID)
	require.NoError(t, err)
	assert.True(t, can)

	// Upgrade to enterprise (unlimited)
	err = db.UpdateOrganizationTier(ctx, org.ID, TierEnterprise)
	require.NoError(t, err)

	can, err = uc.CanAnalyze(ctx, org.ID)
	require.NoError(t, err)
	assert.True(t, can)
}

func TestUsageChecker_CheckAndDeduct(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	uc := NewUsageChecker(db)

	// Setup
	clerkID := "clerk_" + uuid.New().String()[:8]
	user, err := db.CreateUser(ctx, clerkID, "deduct@example.com")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteUser(ctx, user.ID) })

	org, err := db.CreateOrganizationWithOwner(ctx, "Deduct Org", user.ID)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteOrganization(ctx, org.ID) })

	// Should not return error when under limit
	err = uc.CheckAndDeduct(ctx, org.ID)
	require.NoError(t, err)
}

func TestUsageChecker_LimitExceeded(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	uc := NewUsageChecker(db)

	// Setup
	clerkID := "clerk_" + uuid.New().String()[:8]
	user, err := db.CreateUser(ctx, clerkID, "limit@example.com")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteUser(ctx, user.ID) })

	org, err := db.CreateOrganizationWithOwner(ctx, "Limit Org", user.ID)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteOrganization(ctx, org.ID) })

	repo, err := db.CreateRepository(ctx, org.ID, "limit", "repo")
	require.NoError(t, err)

	// Create 10 analyses to hit the free tier limit
	for i := 0; i < 10; i++ {
		_, err = db.CreateAnalysis(ctx, database.CreateAnalysisParams{
			RepoID:   repo.ID,
			RunID:    int64(2000 + i),
			Category: llm.CategoryDiagnosis,
			Text:     "Analysis",
		})
		require.NoError(t, err)
	}

	// Should now be at limit
	can, err := uc.CanAnalyze(ctx, org.ID)
	require.NoError(t, err)
	assert.False(t, can)

	// CheckAndDeduct should return LimitExceededError
	err = uc.CheckAndDeduct(ctx, org.ID)
	require.Error(t, err)
	assert.True(t, IsLimitExceeded(err))

	limitErr, ok := err.(*LimitExceededError)
	require.True(t, ok)
	assert.Equal(t, org.ID, limitErr.OrgID)
	assert.Equal(t, TierFree, limitErr.Tier)
	assert.Equal(t, 10, limitErr.Limit)
	assert.Equal(t, 10, limitErr.Used)
}

func TestLimitExceededError(t *testing.T) {
	now := time.Now().UTC()
	resetDate := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC)

	err := &LimitExceededError{
		OrgID:     uuid.New(),
		Tier:      TierFree,
		Limit:     10,
		Used:      10,
		ResetDate: resetDate,
	}

	msg := err.Error()
	assert.Contains(t, msg, "usage limit exceeded")
	assert.Contains(t, msg, "10/10")
	assert.Contains(t, msg, "free")
}

func TestIsLimitExceeded(t *testing.T) {
	t.Run("returns true for LimitExceededError", func(t *testing.T) {
		err := &LimitExceededError{}
		assert.True(t, IsLimitExceeded(err))
	})

	t.Run("returns false for other errors", func(t *testing.T) {
		err := assert.AnError
		assert.False(t, IsLimitExceeded(err))
	})

	t.Run("returns false for nil", func(t *testing.T) {
		assert.False(t, IsLimitExceeded(nil))
	})
}
