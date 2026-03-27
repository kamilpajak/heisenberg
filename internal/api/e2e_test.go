package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kamilpajak/heisenberg/internal/auth"
	"github.com/kamilpajak/heisenberg/internal/billing"
	"github.com/kamilpajak/heisenberg/internal/database"
	"github.com/kamilpajak/heisenberg/internal/testutil"
	"github.com/kamilpajak/heisenberg/pkg/llm"
	"github.com/kamilpajak/heisenberg/pkg/saas"

	"github.com/google/uuid"
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
			RCA: &llm.RootCauseAnalysis{
				Title:       "Checkout timeout",
				FailureType: llm.FailureTypeTimeout,
				RootCause:   "Modal overlay blocks submit button",
				Remediation: "Wait for modal to close before clicking submit",
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
	assert.NotNil(t, analysis.RCA)
	assert.Equal(t, "Checkout timeout", analysis.RCA.Title)
	assert.Equal(t, "main", *analysis.Branch)
	assert.Equal(t, "e2eabc123", *analysis.CommitSHA)
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
