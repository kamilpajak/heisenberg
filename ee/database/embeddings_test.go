package database_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kamilpajak/heisenberg/ee/database"
	"github.com/kamilpajak/heisenberg/pkg/llm"
)

// setupEmbeddingTest creates the prerequisite user, org, repo, and analysis for embedding tests.
func setupEmbeddingTest(t *testing.T, db *database.DB) (orgID, analysisID uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	clerkID := "clerk_" + uuid.New().String()[:8]
	user, err := db.CreateUser(ctx, clerkID, "embed@example.com")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteUser(ctx, user.ID) })

	org, err := db.CreateOrganizationWithOwner(ctx, "Embedding Test Org", user.ID)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteOrganization(ctx, org.ID) })

	repo, err := db.CreateRepository(ctx, org.ID, "embedowner", "embedrepo-"+uuid.New().String()[:8])
	require.NoError(t, err)

	confidence := 85
	analysis, err := db.CreateAnalysis(ctx, database.CreateAnalysisParams{
		RepoID:     repo.ID,
		RunID:      int64(40000 + len(t.Name())),
		Category:   llm.CategoryDiagnosis,
		Confidence: &confidence,
		RCAs: []llm.RootCauseAnalysis{
			{
				Title:       "Timeout in checkout",
				FailureType: llm.FailureTypeTimeout,
				RootCause:   "waitForSelector timed out",
				Remediation: "Fix selector",
			},
		},
		Text: "Embedding test analysis",
	})
	require.NoError(t, err)

	return org.ID, analysis.ID
}

func makeVector(dims int, base float32) pgvector.Vector {
	vals := make([]float32, dims)
	for i := range vals {
		vals[i] = base + float32(i)*0.001
	}
	return pgvector.NewVector(vals)
}

func TestRCAEmbeddingCRUD(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	orgID, analysisID := setupEmbeddingTest(t, db)

	failureType := "timeout"
	vec := makeVector(768, 0.1)

	// Create
	emb, err := db.CreateRCAEmbedding(ctx, database.CreateEmbeddingParams{
		AnalysisID:    analysisID,
		RCAIndex:      0,
		OrgID:         orgID,
		FailureType:   &failureType,
		EmbeddingText: "failure_type: timeout\nroot_cause: waitForSelector timed out",
		Embedding:     vec,
	})
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, emb.ID)
	assert.Equal(t, analysisID, emb.AnalysisID)
	assert.Equal(t, 0, emb.RCAIndex)
	assert.Equal(t, &failureType, emb.FailureType)

	// Get by analysis
	embeddings, err := db.GetRCAEmbeddingsByAnalysis(ctx, analysisID)
	require.NoError(t, err)
	assert.Len(t, embeddings, 1)
	assert.Equal(t, emb.ID, embeddings[0].ID)
}

func TestRCAEmbedding_UniqueConstraint(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	orgID, analysisID := setupEmbeddingTest(t, db)

	vec := makeVector(768, 0.2)
	params := database.CreateEmbeddingParams{
		AnalysisID:    analysisID,
		RCAIndex:      0,
		OrgID:         orgID,
		EmbeddingText: "test text",
		Embedding:     vec,
	}

	_, err := db.CreateRCAEmbedding(ctx, params)
	require.NoError(t, err)

	// Duplicate (same analysis_id + rca_index) should fail
	_, err = db.CreateRCAEmbedding(ctx, params)
	require.Error(t, err)
}

func TestFindSimilarRCAs(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	// Setup: create org, repo, and two analyses with embeddings
	clerkID := "clerk_" + uuid.New().String()[:8]
	user, err := db.CreateUser(ctx, clerkID, "similar@example.com")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteUser(ctx, user.ID) })

	org, err := db.CreateOrganizationWithOwner(ctx, "Similar Test Org", user.ID)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteOrganization(ctx, org.ID) })

	repo, err := db.CreateRepository(ctx, org.ID, "simowner", "simrepo-"+uuid.New().String()[:8])
	require.NoError(t, err)

	// Analysis 1: timeout failure
	a1, err := db.CreateAnalysis(ctx, database.CreateAnalysisParams{
		RepoID:   repo.ID,
		RunID:    50001,
		Category: llm.CategoryDiagnosis,
		RCAs:     []llm.RootCauseAnalysis{{Title: "Timeout A", FailureType: "timeout", RootCause: "selector timeout"}},
		Text:     "Analysis 1",
	})
	require.NoError(t, err)

	// Analysis 2: similar timeout failure
	a2, err := db.CreateAnalysis(ctx, database.CreateAnalysisParams{
		RepoID:   repo.ID,
		RunID:    50002,
		Category: llm.CategoryDiagnosis,
		RCAs:     []llm.RootCauseAnalysis{{Title: "Timeout B", FailureType: "timeout", RootCause: "selector timeout variant"}},
		Text:     "Analysis 2",
	})
	require.NoError(t, err)

	// Analysis 3: different failure type
	a3, err := db.CreateAnalysis(ctx, database.CreateAnalysisParams{
		RepoID:   repo.ID,
		RunID:    50003,
		Category: llm.CategoryDiagnosis,
		RCAs:     []llm.RootCauseAnalysis{{Title: "Network error", FailureType: "network", RootCause: "connection refused"}},
		Text:     "Analysis 3",
	})
	require.NoError(t, err)

	// Create embeddings: A1 and A2 are similar vectors, A3 is different
	vecSimilar1 := makeVector(768, 0.5)
	vecSimilar2 := makeVector(768, 0.501) // very close to vecSimilar1
	vecDifferent := makeVector(768, -0.5) // far from vecSimilar1

	ft1 := "timeout"
	ft2 := "network"

	_, err = db.CreateRCAEmbedding(ctx, database.CreateEmbeddingParams{
		AnalysisID: a1.ID, RCAIndex: 0, OrgID: org.ID, FailureType: &ft1,
		EmbeddingText: "timeout: selector", Embedding: vecSimilar1,
	})
	require.NoError(t, err)

	_, err = db.CreateRCAEmbedding(ctx, database.CreateEmbeddingParams{
		AnalysisID: a2.ID, RCAIndex: 0, OrgID: org.ID, FailureType: &ft1,
		EmbeddingText: "timeout: selector variant", Embedding: vecSimilar2,
	})
	require.NoError(t, err)

	_, err = db.CreateRCAEmbedding(ctx, database.CreateEmbeddingParams{
		AnalysisID: a3.ID, RCAIndex: 0, OrgID: org.ID, FailureType: &ft2,
		EmbeddingText: "network: connection refused", Embedding: vecDifferent,
	})
	require.NoError(t, err)

	// Find similar to A1 (should find A2 with high similarity, not A3)
	results, err := db.FindSimilarRCAs(ctx, org.ID, vecSimilar1, a1.ID, 5, 0.5)
	require.NoError(t, err)
	require.NotEmpty(t, results)
	assert.Equal(t, a2.ID, results[0].AnalysisID, "most similar should be A2")
	assert.Greater(t, results[0].Similarity, 0.9, "A2 should have high similarity")

	// A1 itself should be excluded
	for _, r := range results {
		assert.NotEqual(t, a1.ID, r.AnalysisID, "source analysis should be excluded")
	}

	// Verify joined data
	assert.Equal(t, int64(50002), results[0].RunID)
	assert.Equal(t, "simowner/simrepo-"+repo.Name[len("simrepo-"):], results[0].RepoFullName)
}

func TestFindSimilarRCAs_CrossOrgIsolation(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	// Setup: two orgs, each with an analysis
	clerkID := "clerk_" + uuid.New().String()[:8]
	user, err := db.CreateUser(ctx, clerkID, "isolation@example.com")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteUser(ctx, user.ID) })

	org1, err := db.CreateOrganizationWithOwner(ctx, "Iso Org 1", user.ID)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteOrganization(ctx, org1.ID) })

	org2, err := db.CreateOrganizationWithOwner(ctx, "Iso Org 2", user.ID)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteOrganization(ctx, org2.ID) })

	repo1, _ := db.CreateRepository(ctx, org1.ID, "iso1", "repo-"+uuid.New().String()[:8])
	repo2, _ := db.CreateRepository(ctx, org2.ID, "iso2", "repo-"+uuid.New().String()[:8])

	a1, _ := db.CreateAnalysis(ctx, database.CreateAnalysisParams{
		RepoID: repo1.ID, RunID: 60001, Category: llm.CategoryDiagnosis,
		RCAs: []llm.RootCauseAnalysis{{Title: "A", RootCause: "test"}}, Text: "A",
	})
	a2, _ := db.CreateAnalysis(ctx, database.CreateAnalysisParams{
		RepoID: repo2.ID, RunID: 60002, Category: llm.CategoryDiagnosis,
		RCAs: []llm.RootCauseAnalysis{{Title: "B", RootCause: "test"}}, Text: "B",
	})

	vec := makeVector(768, 0.3)
	_, _ = db.CreateRCAEmbedding(ctx, database.CreateEmbeddingParams{
		AnalysisID: a1.ID, RCAIndex: 0, OrgID: org1.ID,
		EmbeddingText: "test", Embedding: vec,
	})
	_, _ = db.CreateRCAEmbedding(ctx, database.CreateEmbeddingParams{
		AnalysisID: a2.ID, RCAIndex: 0, OrgID: org2.ID,
		EmbeddingText: "test", Embedding: vec,
	})

	// Search in org1 — should NOT find org2's embedding
	results, err := db.FindSimilarRCAs(ctx, org1.ID, vec, uuid.Nil, 10, 0.0)
	require.NoError(t, err)
	for _, r := range results {
		assert.Equal(t, a1.ID, r.AnalysisID, "should only find org1's analysis")
	}
}

func TestGetRCAEmbeddingsByAnalysis_Empty(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	embeddings, err := db.GetRCAEmbeddingsByAnalysis(ctx, uuid.New())
	require.NoError(t, err)
	assert.Empty(t, embeddings)
}
