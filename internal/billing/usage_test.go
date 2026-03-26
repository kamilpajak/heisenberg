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

// Unit tests with mocks (run without DATABASE_URL)

func TestNewUsageChecker(t *testing.T) {
	mockDB := &MockUsageDB{}
	checker := NewUsageChecker(mockDB)
	assert.NotNil(t, checker)
}

func TestGetUsageStats_FreeTier_Mock(t *testing.T) {
	orgID := uuid.New()

	mockDB := &MockUsageDB{
		GetOrganizationByIDFn: func(ctx context.Context, id uuid.UUID) (*database.Organization, error) {
			return &database.Organization{ID: id, Tier: TierFree}, nil
		},
		CountOrgAnalysesSinceFn: func(ctx context.Context, oid uuid.UUID, since time.Time) (int, error) {
			return 5, nil
		},
	}

	checker := NewUsageChecker(mockDB)
	stats, err := checker.GetUsageStats(context.Background(), orgID)

	assert.NoError(t, err)
	assert.Equal(t, orgID, stats.OrgID)
	assert.Equal(t, TierFree, stats.Tier)
	assert.Equal(t, 5, stats.UsedThisMonth)
	assert.Equal(t, 10, stats.Limit)
	assert.Equal(t, 5, stats.Remaining)
}

func TestGetUsageStats_TeamTier_Mock(t *testing.T) {
	orgID := uuid.New()

	mockDB := &MockUsageDB{
		GetOrganizationByIDFn: func(ctx context.Context, id uuid.UUID) (*database.Organization, error) {
			return &database.Organization{ID: id, Tier: TierTeam}, nil
		},
		CountOrgAnalysesSinceFn: func(ctx context.Context, oid uuid.UUID, since time.Time) (int, error) {
			return 500, nil
		},
	}

	checker := NewUsageChecker(mockDB)
	stats, err := checker.GetUsageStats(context.Background(), orgID)

	assert.NoError(t, err)
	assert.Equal(t, TierTeam, stats.Tier)
	assert.Equal(t, 500, stats.UsedThisMonth)
	assert.Equal(t, 1000, stats.Limit)
	assert.Equal(t, 500, stats.Remaining)
}

func TestGetUsageStats_EnterpriseTier_Unlimited_Mock(t *testing.T) {
	orgID := uuid.New()

	mockDB := &MockUsageDB{
		GetOrganizationByIDFn: func(ctx context.Context, id uuid.UUID) (*database.Organization, error) {
			return &database.Organization{ID: id, Tier: TierEnterprise}, nil
		},
		CountOrgAnalysesSinceFn: func(ctx context.Context, oid uuid.UUID, since time.Time) (int, error) {
			return 10000, nil
		},
	}

	checker := NewUsageChecker(mockDB)
	stats, err := checker.GetUsageStats(context.Background(), orgID)

	assert.NoError(t, err)
	assert.Equal(t, TierEnterprise, stats.Tier)
	assert.Equal(t, -1, stats.Limit)
	assert.Equal(t, -1, stats.Remaining)
}

func TestGetUsageStats_ExceededLimit_Mock(t *testing.T) {
	orgID := uuid.New()

	mockDB := &MockUsageDB{
		GetOrganizationByIDFn: func(ctx context.Context, id uuid.UUID) (*database.Organization, error) {
			return &database.Organization{ID: id, Tier: TierFree}, nil
		},
		CountOrgAnalysesSinceFn: func(ctx context.Context, oid uuid.UUID, since time.Time) (int, error) {
			return 15, nil
		},
	}

	checker := NewUsageChecker(mockDB)
	stats, err := checker.GetUsageStats(context.Background(), orgID)

	assert.NoError(t, err)
	assert.Equal(t, 15, stats.UsedThisMonth)
	assert.Equal(t, 0, stats.Remaining) // Capped at 0
}

func TestGetUsageStats_OrgNotFound_Mock(t *testing.T) {
	orgID := uuid.New()

	mockDB := &MockUsageDB{
		GetOrganizationByIDFn: func(ctx context.Context, id uuid.UUID) (*database.Organization, error) {
			return nil, nil
		},
	}

	checker := NewUsageChecker(mockDB)
	_, err := checker.GetUsageStats(context.Background(), orgID)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "organization not found")
}

func TestGetUsageStats_DBError_Mock(t *testing.T) {
	orgID := uuid.New()

	mockDB := &MockUsageDB{
		GetOrganizationByIDFn: func(ctx context.Context, id uuid.UUID) (*database.Organization, error) {
			return nil, assert.AnError
		},
	}

	checker := NewUsageChecker(mockDB)
	_, err := checker.GetUsageStats(context.Background(), orgID)

	assert.Error(t, err)
}

func TestGetUsageStats_CountError_Mock(t *testing.T) {
	orgID := uuid.New()

	mockDB := &MockUsageDB{
		GetOrganizationByIDFn: func(ctx context.Context, id uuid.UUID) (*database.Organization, error) {
			return &database.Organization{ID: id, Tier: TierFree}, nil
		},
		CountOrgAnalysesSinceFn: func(ctx context.Context, oid uuid.UUID, since time.Time) (int, error) {
			return 0, assert.AnError
		},
	}

	checker := NewUsageChecker(mockDB)
	_, err := checker.GetUsageStats(context.Background(), orgID)

	assert.Error(t, err)
}

func TestCanAnalyze_WithinLimit_Mock(t *testing.T) {
	orgID := uuid.New()

	mockDB := &MockUsageDB{
		GetOrganizationByIDFn: func(ctx context.Context, id uuid.UUID) (*database.Organization, error) {
			return &database.Organization{ID: id, Tier: TierFree}, nil
		},
		CountOrgAnalysesSinceFn: func(ctx context.Context, oid uuid.UUID, since time.Time) (int, error) {
			return 5, nil
		},
	}

	checker := NewUsageChecker(mockDB)
	can, err := checker.CanAnalyze(context.Background(), orgID)

	assert.NoError(t, err)
	assert.True(t, can)
}

func TestCanAnalyze_AtLimit_Mock(t *testing.T) {
	orgID := uuid.New()

	mockDB := &MockUsageDB{
		GetOrganizationByIDFn: func(ctx context.Context, id uuid.UUID) (*database.Organization, error) {
			return &database.Organization{ID: id, Tier: TierFree}, nil
		},
		CountOrgAnalysesSinceFn: func(ctx context.Context, oid uuid.UUID, since time.Time) (int, error) {
			return 10, nil
		},
	}

	checker := NewUsageChecker(mockDB)
	can, err := checker.CanAnalyze(context.Background(), orgID)

	assert.NoError(t, err)
	assert.False(t, can)
}

func TestCanAnalyze_Unlimited_Mock(t *testing.T) {
	orgID := uuid.New()

	mockDB := &MockUsageDB{
		GetOrganizationByIDFn: func(ctx context.Context, id uuid.UUID) (*database.Organization, error) {
			return &database.Organization{ID: id, Tier: TierEnterprise}, nil
		},
		CountOrgAnalysesSinceFn: func(ctx context.Context, oid uuid.UUID, since time.Time) (int, error) {
			return 999999, nil
		},
	}

	checker := NewUsageChecker(mockDB)
	can, err := checker.CanAnalyze(context.Background(), orgID)

	assert.NoError(t, err)
	assert.True(t, can)
}

func TestCanAnalyze_Error_Mock(t *testing.T) {
	orgID := uuid.New()

	mockDB := &MockUsageDB{
		GetOrganizationByIDFn: func(ctx context.Context, id uuid.UUID) (*database.Organization, error) {
			return nil, assert.AnError
		},
	}

	checker := NewUsageChecker(mockDB)
	can, err := checker.CanAnalyze(context.Background(), orgID)

	assert.Error(t, err)
	assert.False(t, can)
}

func TestCheckAndDeduct_Success_Mock(t *testing.T) {
	orgID := uuid.New()

	mockDB := &MockUsageDB{
		GetOrganizationByIDFn: func(ctx context.Context, id uuid.UUID) (*database.Organization, error) {
			return &database.Organization{ID: id, Tier: TierTeam}, nil
		},
		CountOrgAnalysesSinceFn: func(ctx context.Context, oid uuid.UUID, since time.Time) (int, error) {
			return 100, nil
		},
	}

	checker := NewUsageChecker(mockDB)
	err := checker.CheckAndDeduct(context.Background(), orgID)

	assert.NoError(t, err)
}

func TestCheckAndDeduct_LimitExceeded_Mock(t *testing.T) {
	orgID := uuid.New()

	mockDB := &MockUsageDB{
		GetOrganizationByIDFn: func(ctx context.Context, id uuid.UUID) (*database.Organization, error) {
			return &database.Organization{ID: id, Tier: TierFree}, nil
		},
		CountOrgAnalysesSinceFn: func(ctx context.Context, oid uuid.UUID, since time.Time) (int, error) {
			return 10, nil
		},
	}

	checker := NewUsageChecker(mockDB)
	err := checker.CheckAndDeduct(context.Background(), orgID)

	assert.Error(t, err)
	assert.True(t, IsLimitExceeded(err))

	var limitErr *LimitExceededError
	assert.ErrorAs(t, err, &limitErr)
	assert.Equal(t, orgID, limitErr.OrgID)
	assert.Equal(t, TierFree, limitErr.Tier)
}

func TestCheckAndDeduct_DBError_Mock(t *testing.T) {
	orgID := uuid.New()

	mockDB := &MockUsageDB{
		GetOrganizationByIDFn: func(ctx context.Context, id uuid.UUID) (*database.Organization, error) {
			return nil, assert.AnError
		},
	}

	checker := NewUsageChecker(mockDB)
	err := checker.CheckAndDeduct(context.Background(), orgID)

	assert.Error(t, err)
	assert.False(t, IsLimitExceeded(err))
}

func TestUsageStats_Fields_Mock(t *testing.T) {
	orgID := uuid.New()
	resetDate := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)

	stats := UsageStats{
		OrgID:         orgID,
		Tier:          TierTeam,
		UsedThisMonth: 500,
		Limit:         1000,
		Remaining:     500,
		ResetDate:     resetDate,
	}

	assert.Equal(t, orgID, stats.OrgID)
	assert.Equal(t, TierTeam, stats.Tier)
	assert.Equal(t, 500, stats.UsedThisMonth)
	assert.Equal(t, 1000, stats.Limit)
	assert.Equal(t, 500, stats.Remaining)
	assert.Equal(t, resetDate, stats.ResetDate)
}
