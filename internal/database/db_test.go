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

func TestAnalysisWithBranch(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	// Setup
	clerkID := "clerk_" + uuid.New().String()[:8]
	user, _ := db.CreateUser(ctx, clerkID, "branch@example.com")
	t.Cleanup(func() { _ = db.DeleteUser(ctx, user.ID) })

	org, _ := db.CreateOrganizationWithOwner(ctx, "Branch Test Org", user.ID)
	t.Cleanup(func() { _ = db.DeleteOrganization(ctx, org.ID) })

	repo, _ := db.CreateRepository(ctx, org.ID, "branchowner", "branchrepo")

	branch := "feature/test"
	commitSHA := "abc123def456"

	analysis, err := db.CreateAnalysis(ctx, CreateAnalysisParams{
		RepoID:    repo.ID,
		RunID:     99999,
		Branch:    &branch,
		CommitSHA: &commitSHA,
		Category:  llm.CategoryDiagnosis,
		Text:      "Analysis with branch",
	})
	require.NoError(t, err)
	assert.Equal(t, &branch, analysis.Branch)
	assert.Equal(t, &commitSHA, analysis.CommitSHA)
}

func TestAnalysisWithoutRCA(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	// Setup
	clerkID := "clerk_" + uuid.New().String()[:8]
	user, _ := db.CreateUser(ctx, clerkID, "norca@example.com")
	t.Cleanup(func() { _ = db.DeleteUser(ctx, user.ID) })

	org, _ := db.CreateOrganizationWithOwner(ctx, "No RCA Test Org", user.ID)
	t.Cleanup(func() { _ = db.DeleteOrganization(ctx, org.ID) })

	repo, _ := db.CreateRepository(ctx, org.ID, "norcaowner", "norcarepo")

	// Create analysis without RCA (like no_failures category)
	analysis, err := db.CreateAnalysis(ctx, CreateAnalysisParams{
		RepoID:   repo.ID,
		RunID:    88888,
		Category: llm.CategoryNoFailures,
		Text:     "No failures detected",
	})
	require.NoError(t, err)
	assert.Nil(t, analysis.RCA)
	assert.Nil(t, analysis.Confidence)

	// Get by ID should also have nil RCA
	found, err := db.GetAnalysisByID(ctx, analysis.ID)
	require.NoError(t, err)
	assert.Nil(t, found.RCA)
}

func TestListRepoAnalysesWithCategory(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	// Setup
	clerkID := "clerk_" + uuid.New().String()[:8]
	user, _ := db.CreateUser(ctx, clerkID, "category@example.com")
	t.Cleanup(func() { _ = db.DeleteUser(ctx, user.ID) })

	org, _ := db.CreateOrganizationWithOwner(ctx, "Category Test Org", user.ID)
	t.Cleanup(func() { _ = db.DeleteOrganization(ctx, org.ID) })

	repo, _ := db.CreateRepository(ctx, org.ID, "catowner", "catrepo")

	// Create analyses with different categories
	_, err := db.CreateAnalysis(ctx, CreateAnalysisParams{
		RepoID:   repo.ID,
		RunID:    1001,
		Category: llm.CategoryDiagnosis,
		Text:     "Diagnosis 1",
	})
	require.NoError(t, err)

	_, err = db.CreateAnalysis(ctx, CreateAnalysisParams{
		RepoID:   repo.ID,
		RunID:    1002,
		Category: llm.CategoryNoFailures,
		Text:     "No failures",
	})
	require.NoError(t, err)

	// List with category filter
	category := llm.CategoryDiagnosis
	analyses, err := db.ListRepoAnalyses(ctx, ListRepoAnalysesParams{
		RepoID:   repo.ID,
		Category: &category,
	})
	require.NoError(t, err)
	assert.Len(t, analyses, 1)
	assert.Equal(t, llm.CategoryDiagnosis, analyses[0].Category)
}

func TestListRepoAnalysesPagination(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	// Setup
	clerkID := "clerk_" + uuid.New().String()[:8]
	user, _ := db.CreateUser(ctx, clerkID, "pagination@example.com")
	t.Cleanup(func() { _ = db.DeleteUser(ctx, user.ID) })

	org, _ := db.CreateOrganizationWithOwner(ctx, "Pagination Test Org", user.ID)
	t.Cleanup(func() { _ = db.DeleteOrganization(ctx, org.ID) })

	repo, _ := db.CreateRepository(ctx, org.ID, "pageowner", "pagerepo")

	// Create 5 analyses
	for i := 0; i < 5; i++ {
		_, err := db.CreateAnalysis(ctx, CreateAnalysisParams{
			RepoID:   repo.ID,
			RunID:    int64(2000 + i),
			Category: llm.CategoryDiagnosis,
			Text:     "Analysis",
		})
		require.NoError(t, err)
	}

	// List with limit
	analyses, err := db.ListRepoAnalyses(ctx, ListRepoAnalysesParams{
		RepoID: repo.ID,
		Limit:  2,
		Offset: 0,
	})
	require.NoError(t, err)
	assert.Len(t, analyses, 2)

	// List with offset
	analyses, err = db.ListRepoAnalyses(ctx, ListRepoAnalysesParams{
		RepoID: repo.ID,
		Limit:  2,
		Offset: 2,
	})
	require.NoError(t, err)
	assert.Len(t, analyses, 2)

	// List with offset past end
	analyses, err = db.ListRepoAnalyses(ctx, ListRepoAnalysesParams{
		RepoID: repo.ID,
		Limit:  10,
		Offset: 10,
	})
	require.NoError(t, err)
	assert.Len(t, analyses, 0)
}

func TestDeleteOldAnalyses(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	// Setup
	clerkID := "clerk_" + uuid.New().String()[:8]
	user, _ := db.CreateUser(ctx, clerkID, "delete@example.com")
	t.Cleanup(func() { _ = db.DeleteUser(ctx, user.ID) })

	org, _ := db.CreateOrganizationWithOwner(ctx, "Delete Test Org", user.ID)
	t.Cleanup(func() { _ = db.DeleteOrganization(ctx, org.ID) })

	repo, _ := db.CreateRepository(ctx, org.ID, "delowner", "delrepo")

	// Create analysis
	_, err := db.CreateAnalysis(ctx, CreateAnalysisParams{
		RepoID:   repo.ID,
		RunID:    3000,
		Category: llm.CategoryDiagnosis,
		Text:     "To delete",
	})
	require.NoError(t, err)

	// Delete analyses older than future date (should delete all)
	deleted, err := db.DeleteOldAnalyses(ctx, time.Now().Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	// Verify deletion
	count, err := db.CountRepoAnalyses(ctx, repo.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestUpdateOrganizationStripe(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	// Setup
	clerkID := "clerk_" + uuid.New().String()[:8]
	user, _ := db.CreateUser(ctx, clerkID, "stripe@example.com")
	t.Cleanup(func() { _ = db.DeleteUser(ctx, user.ID) })

	org, _ := db.CreateOrganizationWithOwner(ctx, "Stripe Test Org", user.ID)
	t.Cleanup(func() { _ = db.DeleteOrganization(ctx, org.ID) })

	// Update Stripe customer ID and tier
	err := db.UpdateOrganizationStripe(ctx, org.ID, "cus_test123", TierTeam)
	require.NoError(t, err)

	// Verify update
	found, err := db.GetOrganizationByID(ctx, org.ID)
	require.NoError(t, err)
	assert.NotNil(t, found.StripeCustomerID)
	assert.Equal(t, "cus_test123", *found.StripeCustomerID)
	assert.Equal(t, TierTeam, found.Tier)
}

func TestGetNonExistent(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	fakeID := uuid.New()

	t.Run("user by ID", func(t *testing.T) {
		user, err := db.GetUserByID(ctx, fakeID)
		require.NoError(t, err)
		assert.Nil(t, user)
	})

	t.Run("user by clerk ID", func(t *testing.T) {
		user, err := db.GetUserByClerkID(ctx, "nonexistent")
		require.NoError(t, err)
		assert.Nil(t, user)
	})

	t.Run("organization by ID", func(t *testing.T) {
		org, err := db.GetOrganizationByID(ctx, fakeID)
		require.NoError(t, err)
		assert.Nil(t, org)
	})

	t.Run("repository by ID", func(t *testing.T) {
		repo, err := db.GetRepositoryByID(ctx, fakeID)
		require.NoError(t, err)
		assert.Nil(t, repo)
	})

	t.Run("analysis by ID", func(t *testing.T) {
		analysis, err := db.GetAnalysisByID(ctx, fakeID)
		require.NoError(t, err)
		assert.Nil(t, analysis)
	})

	t.Run("analysis by run ID", func(t *testing.T) {
		analysis, err := db.GetAnalysisByRunID(ctx, fakeID, 99999)
		require.NoError(t, err)
		assert.Nil(t, analysis)
	})
}

func TestAddOrgMemberUpsert(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	// Setup
	clerkID := "clerk_" + uuid.New().String()[:8]
	user, _ := db.CreateUser(ctx, clerkID, "role@example.com")
	t.Cleanup(func() { _ = db.DeleteUser(ctx, user.ID) })

	clerkID2 := "clerk_" + uuid.New().String()[:8]
	user2, _ := db.CreateUser(ctx, clerkID2, "role2@example.com")
	t.Cleanup(func() { _ = db.DeleteUser(ctx, user2.ID) })

	org, _ := db.CreateOrganizationWithOwner(ctx, "Role Test Org", user.ID)
	t.Cleanup(func() { _ = db.DeleteOrganization(ctx, org.ID) })

	// Add member
	err := db.AddOrgMember(ctx, org.ID, user2.ID, RoleMember)
	require.NoError(t, err)

	// Update role via AddOrgMember (upsert)
	err = db.AddOrgMember(ctx, org.ID, user2.ID, RoleAdmin)
	require.NoError(t, err)

	// Verify role was updated
	member, err := db.GetOrgMember(ctx, org.ID, user2.ID)
	require.NoError(t, err)
	assert.Equal(t, RoleAdmin, member.Role)
}
