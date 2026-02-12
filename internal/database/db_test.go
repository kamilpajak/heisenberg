package database

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kamilpajak/heisenberg/pkg/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testDB returns a connected DB or skips if DATABASE_URL is not set.
func testDB(t *testing.T) *DB {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set")
	}

	ctx := context.Background()
	db, err := New(ctx, dbURL)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	return db
}

func TestMigrations(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set")
	}

	// Just test that migrations can run (idempotent)
	// Don't run MigrateDown as it interferes with parallel test packages
	err := Migrate(dbURL)
	require.NoError(t, err)
}

func TestUserCRUD(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	// Create
	clerkID := "clerk_" + uuid.New().String()[:8]
	user, err := db.CreateUser(ctx, clerkID, "test@example.com")
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, user.ID)
	assert.Equal(t, clerkID, user.ClerkID)
	assert.Equal(t, "test@example.com", user.Email)

	// Get by Clerk ID
	found, err := db.GetUserByClerkID(ctx, clerkID)
	require.NoError(t, err)
	assert.Equal(t, user.ID, found.ID)

	// Get by ID
	found, err = db.GetUserByID(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, user.ClerkID, found.ClerkID)

	// Update email
	err = db.UpdateUserEmail(ctx, user.ID, "new@example.com")
	require.NoError(t, err)
	found, _ = db.GetUserByID(ctx, user.ID)
	assert.Equal(t, "new@example.com", found.Email)

	// GetOrCreate existing
	existing, err := db.GetOrCreateUser(ctx, clerkID, "different@example.com")
	require.NoError(t, err)
	assert.Equal(t, user.ID, existing.ID)

	// GetOrCreate new
	newClerkID := "clerk_" + uuid.New().String()[:8]
	created, err := db.GetOrCreateUser(ctx, newClerkID, "created@example.com")
	require.NoError(t, err)
	assert.Equal(t, newClerkID, created.ClerkID)

	// Delete
	err = db.DeleteUser(ctx, user.ID)
	require.NoError(t, err)
	found, err = db.GetUserByID(ctx, user.ID)
	require.NoError(t, err)
	assert.Nil(t, found)

	// Cleanup
	_ = db.DeleteUser(ctx, created.ID)
}

func TestOrganizationCRUD(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	// Create user first
	clerkID := "clerk_" + uuid.New().String()[:8]
	user, err := db.CreateUser(ctx, clerkID, "owner@example.com")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteUser(ctx, user.ID) })

	// Create org with owner
	org, err := db.CreateOrganizationWithOwner(ctx, "Test Org", user.ID)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, org.ID)
	assert.Equal(t, "Test Org", org.Name)
	assert.Equal(t, TierFree, org.Tier)
	t.Cleanup(func() { _ = db.DeleteOrganization(ctx, org.ID) })

	// Get by ID
	found, err := db.GetOrganizationByID(ctx, org.ID)
	require.NoError(t, err)
	assert.Equal(t, org.Name, found.Name)

	// List user orgs
	orgs, err := db.ListUserOrganizations(ctx, user.ID)
	require.NoError(t, err)
	assert.Len(t, orgs, 1)
	assert.Equal(t, org.ID, orgs[0].ID)

	// Check membership
	member, err := db.GetOrgMember(ctx, org.ID, user.ID)
	require.NoError(t, err)
	assert.Equal(t, RoleOwner, member.Role)

	// Update tier
	err = db.UpdateOrganizationTier(ctx, org.ID, TierTeam)
	require.NoError(t, err)
	found, _ = db.GetOrganizationByID(ctx, org.ID)
	assert.Equal(t, TierTeam, found.Tier)

	// Add another member
	clerkID2 := "clerk_" + uuid.New().String()[:8]
	user2, _ := db.CreateUser(ctx, clerkID2, "member@example.com")
	t.Cleanup(func() { _ = db.DeleteUser(ctx, user2.ID) })

	err = db.AddOrgMember(ctx, org.ID, user2.ID, RoleMember)
	require.NoError(t, err)

	members, err := db.ListOrgMembers(ctx, org.ID)
	require.NoError(t, err)
	assert.Len(t, members, 2)

	// Remove member
	err = db.RemoveOrgMember(ctx, org.ID, user2.ID)
	require.NoError(t, err)
	members, _ = db.ListOrgMembers(ctx, org.ID)
	assert.Len(t, members, 1)
}

func TestRepositoryCRUD(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	// Setup
	clerkID := "clerk_" + uuid.New().String()[:8]
	user, _ := db.CreateUser(ctx, clerkID, "test@example.com")
	t.Cleanup(func() { _ = db.DeleteUser(ctx, user.ID) })

	org, _ := db.CreateOrganizationWithOwner(ctx, "Test Org", user.ID)
	t.Cleanup(func() { _ = db.DeleteOrganization(ctx, org.ID) })

	// Create
	repo, err := db.CreateRepository(ctx, org.ID, "owner", "repo")
	require.NoError(t, err)
	assert.Equal(t, "owner", repo.Owner)
	assert.Equal(t, "repo", repo.Name)
	assert.Equal(t, "owner/repo", repo.FullName)

	// Get by ID
	found, err := db.GetRepositoryByID(ctx, repo.ID)
	require.NoError(t, err)
	assert.Equal(t, repo.FullName, found.FullName)

	// Get by name
	found, err = db.GetRepositoryByName(ctx, org.ID, "owner", "repo")
	require.NoError(t, err)
	assert.Equal(t, repo.ID, found.ID)

	// GetOrCreate existing
	existing, err := db.GetOrCreateRepository(ctx, org.ID, "owner", "repo")
	require.NoError(t, err)
	assert.Equal(t, repo.ID, existing.ID)

	// GetOrCreate new
	created, err := db.GetOrCreateRepository(ctx, org.ID, "other", "repo")
	require.NoError(t, err)
	assert.NotEqual(t, repo.ID, created.ID)

	// List
	repos, err := db.ListOrgRepositories(ctx, org.ID)
	require.NoError(t, err)
	assert.Len(t, repos, 2)

	// Delete
	err = db.DeleteRepository(ctx, repo.ID)
	require.NoError(t, err)
	_ = db.DeleteRepository(ctx, created.ID)
}

func TestAnalysisCRUD(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	// Setup
	clerkID := "clerk_" + uuid.New().String()[:8]
	user, _ := db.CreateUser(ctx, clerkID, "test@example.com")
	t.Cleanup(func() { _ = db.DeleteUser(ctx, user.ID) })

	org, _ := db.CreateOrganizationWithOwner(ctx, "Test Org", user.ID)
	t.Cleanup(func() { _ = db.DeleteOrganization(ctx, org.ID) })

	repo, _ := db.CreateRepository(ctx, org.ID, "owner", "repo")

	// Create analysis with RCA
	confidence := 85
	sensitivity := "medium"
	rca := &llm.RootCauseAnalysis{
		Title:       "Timeout in checkout",
		FailureType: llm.FailureTypeTimeout,
		Location: &llm.CodeLocation{
			FilePath:   "tests/checkout.spec.ts",
			LineNumber: 45,
		},
		Symptom:     "Submit button not clickable",
		RootCause:   "Element hidden by modal",
		Remediation: "Wait for modal to close",
		Evidence: []llm.Evidence{
			{Type: llm.EvidenceScreenshot, Content: "Shows modal over button"},
		},
	}

	analysis, err := db.CreateAnalysis(ctx, CreateAnalysisParams{
		RepoID:      repo.ID,
		RunID:       12345,
		Category:    llm.CategoryDiagnosis,
		Confidence:  &confidence,
		Sensitivity: &sensitivity,
		RCA:         rca,
		Text:        "Analysis text",
	})
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, analysis.ID)

	// Get by ID
	found, err := db.GetAnalysisByID(ctx, analysis.ID)
	require.NoError(t, err)
	assert.Equal(t, "Analysis text", found.Text)
	assert.NotNil(t, found.RCA)
	assert.Equal(t, "Timeout in checkout", found.RCA.Title)
	assert.Len(t, found.RCA.Evidence, 1)

	// Get by run ID
	found, err = db.GetAnalysisByRunID(ctx, repo.ID, 12345)
	require.NoError(t, err)
	assert.Equal(t, analysis.ID, found.ID)

	// List
	analyses, err := db.ListRepoAnalyses(ctx, ListRepoAnalysesParams{RepoID: repo.ID})
	require.NoError(t, err)
	assert.Len(t, analyses, 1)

	// Count
	count, err := db.CountRepoAnalyses(ctx, repo.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Count org analyses
	orgCount, err := db.CountOrgAnalysesSince(ctx, org.ID, time.Now().Add(-24*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 1, orgCount)

	// Delete
	err = db.DeleteAnalysis(ctx, analysis.ID)
	require.NoError(t, err)
}
