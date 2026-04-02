package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kamilpajak/heisenberg/ee/auth"
	"github.com/kamilpajak/heisenberg/ee/billing"
	"github.com/kamilpajak/heisenberg/ee/database"
	eepatterns "github.com/kamilpajak/heisenberg/ee/patterns"
	"github.com/kamilpajak/heisenberg/ee/testutil"
	"github.com/kamilpajak/heisenberg/pkg/llm"
	"github.com/kamilpajak/heisenberg/pkg/saas"

	"github.com/google/uuid"
)

const (
	e2eTitleCheckout = "Checkout timeout"
	e2eAuthBearer    = "Bearer "
)

// seedTestAPIKey creates a user, org, and API key, returning the plaintext key.
func seedTestAPIKey(t *testing.T, db *database.DB) (plainKey string, orgID uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	clerkID := "kp_e2e_" + uuid.New().String()[:8]
	email := "e2e-" + uuid.New().String()[:8] + "@example.com"

	user, err := db.CreateUser(ctx, clerkID, email)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteUser(ctx, user.ID) })

	org, err := db.CreateOrganizationWithOwner(ctx, "E2E Test Org", user.ID)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteOrganization(ctx, org.ID) })

	// Generate API key and store hash
	plainKey = "hsb_e2etest_" + uuid.New().String()[:16]
	keyHash := auth.HashAPIKey(plainKey)
	_, err = db.CreateAPIKey(ctx, keyHash, user.ID, org.ID, "E2E Test Key")
	require.NoError(t, err)

	return plainKey, org.ID
}

// e2eServer creates an API server with real auth middleware (API key path).
func e2eServer(t *testing.T, db *database.DB) *httptest.Server {
	t.Helper()

	billingClient := billing.NewClient(billing.Config{
		SecretKey: "sk_test_fake",
		PriceIDs: billing.PriceIDs{
			Team:       "price_team_test",
			Enterprise: "price_ent_test",
		},
	})

	apiKeyStore := &dbAPIKeyStore{db: db}
	server := &Server{
		db:            db,
		authVerifier:  nil, // No Kinde in tests
		billingClient: billingClient,
		usageChecker:  billing.NewUsageChecker(db),
		mux:           http.NewServeMux(),
	}

	// Register routes WITH API key auth middleware (real auth path)
	authMiddleware := auth.Middleware(nil, apiKeyStore)
	server.mux.HandleFunc("GET /health", server.handleHealth)
	server.mux.HandleFunc("POST /api/organizations/{orgID}/analyses", server.withAuth(authMiddleware, server.handleCreateAnalysis))
	server.mux.HandleFunc("GET /api/organizations/{orgID}/repositories/{repoID}/analyses", server.withAuth(authMiddleware, server.handleListAnalyses))

	ts := httptest.NewServer(server)
	t.Cleanup(ts.Close)
	return ts
}

func TestE2E_CLIPersistence(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()

	// Seed API key (real hash in DB)
	apiKey, orgID := seedTestAPIKey(t, db)

	// Start real HTTP server with API key auth
	ts := e2eServer(t, db)

	// Create SaaS client (same as CLI would use)
	client := mustNewTestClient(ts.URL, apiKey)

	// Submit analysis (simulating CLI after analysis.Run())
	confidence := 85
	id, err := client.SubmitAnalysis(ctx, saas.SubmitParams{
		OrgID:     orgID.String(),
		Owner:     "e2eowner",
		Repo:      "e2erepo",
		RunID:     77001,
		Branch:    "main",
		CommitSHA: "e2eabc123",
		Result: &llm.AnalysisResult{
			Text:        "E2E root cause: timeout in checkout flow",
			Category:    llm.CategoryDiagnosis,
			Confidence:  confidence,
			Sensitivity: "medium",
			RCAs: []llm.RootCauseAnalysis{
				{
					Title:       e2eTitleCheckout,
					FailureType: llm.FailureTypeTimeout,
					RootCause:   "Modal overlay blocks submit button",
					Remediation: "Wait for modal to close before clicking submit",
				},
			},
		},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, id)

	// Verify: analysis persisted in DB
	analysisID, err := uuid.Parse(id)
	require.NoError(t, err)
	analysis, err := db.GetAnalysisByID(ctx, analysisID)
	require.NoError(t, err)
	assert.Equal(t, "E2E root cause: timeout in checkout flow", analysis.Text)
	assert.Equal(t, llm.CategoryDiagnosis, analysis.Category)
	assert.Equal(t, &confidence, analysis.Confidence)
	require.Len(t, analysis.RCAs, 1)
	assert.Equal(t, e2eTitleCheckout, analysis.RCAs[0].Title)
	assert.Equal(t, "main", *analysis.Branch)
	assert.Equal(t, "e2eabc123", *analysis.CommitSHA)
}

func TestE2E_MultiRCAPersistence(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()

	apiKey, orgID := seedTestAPIKey(t, db)
	ts := e2eServer(t, db)
	client := mustNewTestClient(ts.URL, apiKey)

	confidence := 90
	id, err := client.SubmitAnalysis(ctx, saas.SubmitParams{
		OrgID:     orgID.String(),
		Owner:     "multiowner",
		Repo:      "multirepo",
		RunID:     77002,
		Branch:    "feature/multi",
		CommitSHA: "multi123",
		Result: &llm.AnalysisResult{
			Text:        "Two distinct failures found",
			Category:    llm.CategoryDiagnosis,
			Confidence:  confidence,
			Sensitivity: "low",
			RCAs: []llm.RootCauseAnalysis{
				{
					Title:       "Timeout in checkout",
					FailureType: llm.FailureTypeTimeout,
					BugLocation: llm.BugLocationTest,
					RootCause:   "Cookie banner overlay",
					Remediation: "Dismiss banner first",
				},
				{
					Title:       "Assertion in login",
					FailureType: llm.FailureTypeAssertion,
					BugLocation: llm.BugLocationProduction,
					RootCause:   "Changed redirect URL",
					Remediation: "Update expected URL",
				},
			},
		},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, id)

	// Verify both RCAs survive the full pipeline
	analysisID, err := uuid.Parse(id)
	require.NoError(t, err)
	analysis, err := db.GetAnalysisByID(ctx, analysisID)
	require.NoError(t, err)
	assert.Equal(t, "Two distinct failures found", analysis.Text)
	require.Len(t, analysis.RCAs, 2)
	assert.Equal(t, "Timeout in checkout", analysis.RCAs[0].Title)
	assert.Equal(t, llm.FailureTypeTimeout, analysis.RCAs[0].FailureType)
	assert.Equal(t, "Assertion in login", analysis.RCAs[1].Title)
	assert.Equal(t, llm.FailureTypeAssertion, analysis.RCAs[1].FailureType)
}

func TestE2E_DuplicateRunID(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()

	apiKey, orgID := seedTestAPIKey(t, db)
	ts := e2eServer(t, db)
	client := mustNewTestClient(ts.URL, apiKey)

	params := saas.SubmitParams{
		OrgID: orgID.String(),
		Owner: "dupeowner", Repo: "duperepo",
		RunID:  77002,
		Result: &llm.AnalysisResult{Text: "First", Category: llm.CategoryDiagnosis},
	}

	// First submit: success
	_, err := client.SubmitAnalysis(ctx, params)
	require.NoError(t, err)

	// Second submit: conflict
	_, err = client.SubmitAnalysis(ctx, params)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "409")
}

func TestE2E_InvalidAPIKey(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()

	_, orgID := seedTestAPIKey(t, db)
	ts := e2eServer(t, db)
	client := mustNewTestClient(ts.URL, "hsb_invalid_key_does_not_exist")

	_, err := client.SubmitAnalysis(ctx, saas.SubmitParams{
		OrgID: orgID.String(),
		Owner: "badowner", Repo: "badrepo",
		RunID:  77003,
		Result: &llm.AnalysisResult{Text: "Should fail", Category: "diagnosis"},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

// mustNewTestClient creates a saas.Client for testing without env vars.
func mustNewTestClient(baseURL, apiKey string) *saas.Client {
	return saas.NewTestClient(baseURL, apiKey)
}

// mockEmbeddingServer returns an httptest.Server that produces deterministic
// embeddings based on the input text. Similar texts get similar vectors.
func mockEmbeddingServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		// Parse input text to produce a deterministic vector
		var req struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		}
		_ = json.Unmarshal(body, &req)

		text := ""
		if len(req.Content.Parts) > 0 {
			text = req.Content.Parts[0].Text
		}

		// Generate a deterministic 768-dim vector from text hash.
		// Texts containing "timeout" will cluster together; "network" will be different.
		vec := make([]float32, 768)
		seed := float32(0.5) // default
		if len(text) > 0 {
			// Simple hash: sum of bytes normalized
			var sum float32
			for _, b := range []byte(text) {
				sum += float32(b)
			}
			seed = float32(math.Mod(float64(sum)/1000.0, 1.0))
		}
		for i := range vec {
			vec[i] = seed + float32(i)*0.0001
		}
		// Normalize for cosine similarity
		var norm float32
		for _, v := range vec {
			norm += v * v
		}
		norm = float32(math.Sqrt(float64(norm)))
		for i := range vec {
			vec[i] /= norm
		}

		resp := map[string]any{
			"embedding": map[string]any{
				"values": vec,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

// e2eServerWithEmbeddings creates an API server with API key auth and embedding support.
func e2eServerWithEmbeddings(t *testing.T, db *database.DB, embeddingClient *eepatterns.EmbeddingClient) *httptest.Server {
	t.Helper()

	billingClient := billing.NewClient(billing.Config{
		SecretKey: "sk_test_fake",
		PriceIDs: billing.PriceIDs{
			Team:       "price_team_test",
			Enterprise: "price_ent_test",
		},
	})

	apiKeyStore := &dbAPIKeyStore{db: db}
	server := &Server{
		db:              db,
		authVerifier:    nil,
		billingClient:   billingClient,
		usageChecker:    billing.NewUsageChecker(db),
		embeddingClient: embeddingClient,
		mux:             http.NewServeMux(),
	}

	authMiddleware := auth.Middleware(nil, apiKeyStore)
	server.mux.HandleFunc("GET /health", server.handleHealth)
	server.mux.HandleFunc("POST /api/organizations/{orgID}/analyses", server.withAuth(authMiddleware, server.handleCreateAnalysis))
	server.mux.HandleFunc("GET /api/organizations/{orgID}/analyses/{analysisID}/similar", server.withAuth(authMiddleware, server.handleSimilarAnalyses))
	server.mux.HandleFunc("GET /api/organizations/{orgID}/patterns/search", server.withAuth(authMiddleware, server.handlePatternSearch))

	ts := httptest.NewServer(server)
	t.Cleanup(ts.Close)
	return ts
}

func TestE2E_SimilarAnalyses(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()

	// Start mock embedding server
	embServer := mockEmbeddingServer(t)
	defer embServer.Close()
	embClient := eepatterns.NewTestEmbeddingClient(embServer.URL)

	// Seed API key
	apiKey, orgID := seedTestAPIKey(t, db)

	// Start API server with embedding support
	ts := e2eServerWithEmbeddings(t, db, embClient)
	client := mustNewTestClient(ts.URL, apiKey)

	// Submit analysis 1: timeout failure
	id1, err := client.SubmitAnalysis(ctx, saas.SubmitParams{
		OrgID: orgID.String(), Owner: "simowner", Repo: "simrepo",
		RunID: 90001, Branch: "main",
		Result: &llm.AnalysisResult{
			Text:     "Timeout in checkout flow",
			Category: llm.CategoryDiagnosis,
			RCAs: []llm.RootCauseAnalysis{{
				Title:       e2eTitleCheckout,
				FailureType: llm.FailureTypeTimeout,
				Location:    &llm.CodeLocation{FilePath: "tests/checkout.spec.ts"},
				RootCause:   "waitForSelector timed out in beforeEach hook",
				Symptom:     "Test setup failed with TimeoutError",
				Remediation: "Fix selector",
			}},
		},
	})
	require.NoError(t, err)

	// Submit analysis 2: similar timeout failure (same pattern)
	id2, err := client.SubmitAnalysis(ctx, saas.SubmitParams{
		OrgID: orgID.String(), Owner: "simowner", Repo: "simrepo",
		RunID: 90002, Branch: "main",
		Result: &llm.AnalysisResult{
			Text:     "Timeout in login flow",
			Category: llm.CategoryDiagnosis,
			RCAs: []llm.RootCauseAnalysis{{
				Title:       "Login timeout",
				FailureType: llm.FailureTypeTimeout,
				Location:    &llm.CodeLocation{FilePath: "tests/login.spec.ts"},
				RootCause:   "waitForSelector timed out in beforeEach hook for login form",
				Symptom:     "Test setup failed with TimeoutError",
				Remediation: "Fix login selector",
			}},
		},
	})
	require.NoError(t, err)

	// Submit analysis 3: different failure type (network)
	_, err = client.SubmitAnalysis(ctx, saas.SubmitParams{
		OrgID: orgID.String(), Owner: "simowner", Repo: "simrepo",
		RunID: 90003, Branch: "main",
		Result: &llm.AnalysisResult{
			Text:     "Network error",
			Category: llm.CategoryDiagnosis,
			RCAs: []llm.RootCauseAnalysis{{
				Title:       "Database connection refused",
				FailureType: llm.FailureTypeNetwork,
				RootCause:   "ECONNREFUSED on port 5432 database server unreachable",
				Symptom:     "Connection refused",
				Remediation: "Check database service",
			}},
		},
	})
	require.NoError(t, err)

	// Wait for async embedding goroutines to complete
	analysisID1, _ := uuid.Parse(id1)
	analysisID2, _ := uuid.Parse(id2)
	waitForEmbeddings(t, db, analysisID1, 1)
	waitForEmbeddings(t, db, analysisID2, 1)

	// Query /similar for analysis 1
	similarURL := fmt.Sprintf("%s/api/organizations/%s/analyses/%s/similar?limit=5&threshold=0.5",
		ts.URL, orgID, id1)
	req, _ := http.NewRequest("GET", similarURL, nil)
	req.Header.Set("Authorization", e2eAuthBearer+apiKey)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var similarResp struct {
		Similar []database.SimilarRCA `json:"similar"`
	}
	err = json.NewDecoder(resp.Body).Decode(&similarResp)
	require.NoError(t, err)

	// Should find at least analysis 2 as similar
	require.NotEmpty(t, similarResp.Similar, "should find similar analyses")

	// The most similar should be analysis 2 (same timeout pattern)
	found := false
	for _, s := range similarResp.Similar {
		if s.AnalysisID == analysisID2 {
			found = true
			assert.Greater(t, s.Similarity, 0.5, "timeout patterns should have high similarity")
			assert.Equal(t, int64(90002), s.RunID)
			break
		}
	}
	assert.True(t, found, "analysis 2 should appear in similar results")

	// Analysis 1 should NOT appear in its own similar results
	for _, s := range similarResp.Similar {
		assert.NotEqual(t, analysisID1, s.AnalysisID, "source analysis should be excluded")
	}
}

func TestE2E_PatternSearch(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()

	embServer := mockEmbeddingServer(t)
	defer embServer.Close()
	embClient := eepatterns.NewTestEmbeddingClient(embServer.URL)

	apiKey, orgID := seedTestAPIKey(t, db)
	ts := e2eServerWithEmbeddings(t, db, embClient)
	client := mustNewTestClient(ts.URL, apiKey)

	// Submit an analysis
	id, err := client.SubmitAnalysis(ctx, saas.SubmitParams{
		OrgID: orgID.String(), Owner: "searchowner", Repo: "searchrepo",
		RunID: 91001, Branch: "main",
		Result: &llm.AnalysisResult{
			Text:     "Playwright timeout",
			Category: llm.CategoryDiagnosis,
			RCAs: []llm.RootCauseAnalysis{{
				Title:       "Selector timeout",
				FailureType: llm.FailureTypeTimeout,
				RootCause:   "waitForSelector timed out",
				Remediation: "Fix selector",
			}},
		},
	})
	require.NoError(t, err)

	// Wait for async embedding
	analysisID, _ := uuid.Parse(id)
	waitForEmbeddings(t, db, analysisID, 1)

	// Search for patterns
	searchURL := fmt.Sprintf("%s/api/organizations/%s/patterns/search?q=timeout+selector&limit=5",
		ts.URL, orgID)
	req, _ := http.NewRequest("GET", searchURL, nil)
	req.Header.Set("Authorization", e2eAuthBearer+apiKey)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var searchResp struct {
		Results []database.SimilarRCA `json:"results"`
	}
	err = json.NewDecoder(resp.Body).Decode(&searchResp)
	require.NoError(t, err)
	assert.NotEmpty(t, searchResp.Results, "pattern search should return results")
}

func TestE2E_SimilarAnalyses_NoEmbeddings(t *testing.T) {
	db := testutil.NewTestDB(t)

	embServer := mockEmbeddingServer(t)
	defer embServer.Close()
	embClient := eepatterns.NewTestEmbeddingClient(embServer.URL)

	apiKey, orgID := seedTestAPIKey(t, db)
	ts := e2eServerWithEmbeddings(t, db, embClient)

	// Query /similar for a non-existent analysis
	fakeID := uuid.New()
	similarURL := fmt.Sprintf("%s/api/organizations/%s/analyses/%s/similar",
		ts.URL, orgID, fakeID)
	req, _ := http.NewRequest("GET", similarURL, nil)
	req.Header.Set("Authorization", e2eAuthBearer+apiKey)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestE2E_SimilarAnalyses_EmptyArrayNotNull(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()

	embServer := mockEmbeddingServer(t)
	defer embServer.Close()
	embClient := eepatterns.NewTestEmbeddingClient(embServer.URL)

	apiKey, orgID := seedTestAPIKey(t, db)
	ts := e2eServerWithEmbeddings(t, db, embClient)
	client := mustNewTestClient(ts.URL, apiKey)

	// Submit one analysis so it has embeddings
	id, err := client.SubmitAnalysis(ctx, saas.SubmitParams{
		OrgID: orgID.String(), Owner: "nullowner", Repo: "nullrepo",
		RunID: 92001, Branch: "main",
		Result: &llm.AnalysisResult{
			Text:     "Unique failure",
			Category: llm.CategoryDiagnosis,
			RCAs: []llm.RootCauseAnalysis{{
				Title:       "Unique error",
				FailureType: llm.FailureTypeTimeout,
				RootCause:   "something unique that won't match anything",
				Remediation: "fix it",
			}},
		},
	})
	require.NoError(t, err)

	analysisID, _ := uuid.Parse(id)
	waitForEmbeddings(t, db, analysisID, 1)

	// Query /similar with threshold=0.99 — no matches expected
	// The response should contain "similar": [] (empty array), NOT "similar": null
	similarURL := fmt.Sprintf("%s/api/organizations/%s/analyses/%s/similar?threshold=0.99",
		ts.URL, orgID, id)
	req, _ := http.NewRequest("GET", similarURL, nil)
	req.Header.Set("Authorization", e2eAuthBearer+apiKey)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), `"similar":[]`, "empty results should be [] not null")
}

func TestE2E_PatternSearch_MissingQuery(t *testing.T) {
	db := testutil.NewTestDB(t)

	embServer := mockEmbeddingServer(t)
	defer embServer.Close()
	embClient := eepatterns.NewTestEmbeddingClient(embServer.URL)

	apiKey, orgID := seedTestAPIKey(t, db)
	ts := e2eServerWithEmbeddings(t, db, embClient)

	// Search without q parameter
	searchURL := fmt.Sprintf("%s/api/organizations/%s/patterns/search", ts.URL, orgID)
	req, _ := http.NewRequest("GET", searchURL, nil)
	req.Header.Set("Authorization", e2eAuthBearer+apiKey)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// waitForEmbeddings polls the database until the expected number of embeddings
// appear for the given analysis, or fails the test after a timeout.
func waitForEmbeddings(t *testing.T, db *database.DB, analysisID uuid.UUID, expected int) {
	t.Helper()
	ctx := context.Background()

	for range 50 { // 50 * 100ms = 5s max
		embeddings, err := db.GetRCAEmbeddingsByAnalysis(ctx, analysisID)
		if err == nil && len(embeddings) >= expected {
			return
		}
		sleepCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		<-sleepCtx.Done()
		cancel()
	}

	embeddings, _ := db.GetRCAEmbeddingsByAnalysis(ctx, analysisID)
	require.Len(t, embeddings, expected,
		"timed out waiting for embeddings for analysis %s", analysisID)
}
