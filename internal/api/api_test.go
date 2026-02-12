package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/kamilpajak/heisenberg/internal/auth"
	"github.com/kamilpajak/heisenberg/internal/billing"
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

// testServer creates a test API server without auth middleware.
// Tests inject auth via withAuthContext helper.
func testServer(t *testing.T, db *database.DB) *Server {
	t.Helper()

	billingClient := billing.NewClient(billing.Config{
		SecretKey: "sk_test_fake",
		PriceIDs: billing.PriceIDs{
			Team:       "price_team_test",
			Enterprise: "price_ent_test",
		},
	})

	server := &Server{
		db:            db,
		authVerifier:  nil,
		billingClient: billingClient,
		usageChecker:  billing.NewUsageChecker(db),
		mux:           http.NewServeMux(),
	}

	// Register routes WITHOUT auth middleware for testing
	// Tests use withAuthContext to inject claims directly
	server.mux.HandleFunc("GET /health", server.handleHealth)
	server.mux.HandleFunc("POST /api/auth/sync", server.handleAuthSync)
	server.mux.HandleFunc("GET /api/me", server.handleGetMe)
	server.mux.HandleFunc("GET /api/organizations", server.handleListOrganizations)
	server.mux.HandleFunc("POST /api/organizations", server.handleCreateOrganization)
	server.mux.HandleFunc("GET /api/organizations/{orgID}", server.handleGetOrganization)
	server.mux.HandleFunc("GET /api/organizations/{orgID}/repositories", server.handleListRepositories)
	server.mux.HandleFunc("GET /api/organizations/{orgID}/repositories/{repoID}", server.handleGetRepository)
	server.mux.HandleFunc("GET /api/organizations/{orgID}/repositories/{repoID}/analyses", server.handleListAnalyses)
	server.mux.HandleFunc("GET /api/organizations/{orgID}/analyses/{analysisID}", server.handleGetAnalysis)
	server.mux.HandleFunc("GET /api/organizations/{orgID}/usage", server.handleGetUsage)
	server.mux.HandleFunc("POST /api/billing/checkout", server.handleCreateCheckout)
	server.mux.HandleFunc("POST /api/billing/portal", server.handleCreatePortal)

	return server
}

// withAuthContext wraps a request with authenticated user claims.
func withAuthContext(r *http.Request, userID, email string) *http.Request {
	claims := auth.NewTestClaims(userID, email)
	ctx := auth.WithClaims(r.Context(), claims)
	return r.WithContext(ctx)
}

func TestHealthEndpoint(t *testing.T) {
	db := testDB(t)
	server := testServer(t, db)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]string
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "ok", resp["status"])
}

func TestCORS(t *testing.T) {
	db := testDB(t)
	server := testServer(t, db)

	t.Run("OPTIONS request returns 200", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/api/me", nil)
		rec := httptest.NewRecorder()

		server.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("CORS headers on regular request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rec := httptest.NewRecorder()

		server.ServeHTTP(rec, req)

		assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
		assert.Contains(t, rec.Header().Get("Access-Control-Allow-Methods"), "GET")
	})
}

func TestAuthSync(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	server := testServer(t, db)

	kindeUserID := "kp_" + uuid.New().String()[:8]
	email := "sync-" + uuid.New().String()[:8] + "@example.com"

	t.Run("creates new user", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/sync", nil)
		req = withAuthContext(req, kindeUserID, email)
		rec := httptest.NewRecorder()

		server.mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var resp map[string]any
		err := json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, email, resp["email"])
		assert.Equal(t, kindeUserID, resp["kinde_id"])
	})

	t.Run("returns existing user", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/sync", nil)
		req = withAuthContext(req, kindeUserID, email)
		rec := httptest.NewRecorder()

		server.mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	// Cleanup
	user, _ := db.GetUserByClerkID(ctx, kindeUserID)
	if user != nil {
		_ = db.DeleteUser(ctx, user.ID)
	}
}

func TestGetMe(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	server := testServer(t, db)

	// Create user
	kindeUserID := "kp_" + uuid.New().String()[:8]
	email := "me-" + uuid.New().String()[:8] + "@example.com"
	user, err := db.CreateUser(ctx, kindeUserID, email)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteUser(ctx, user.ID) })

	t.Run("returns user info", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
		req = withAuthContext(req, kindeUserID, email)
		rec := httptest.NewRecorder()

		server.mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var resp map[string]any
		err := json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, email, resp["email"])
		// organizations may be null if user has no orgs
		_, hasOrgs := resp["organizations"]
		assert.True(t, hasOrgs, "response should include organizations key")
	})

	t.Run("returns 404 for non-existent user", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
		req = withAuthContext(req, "kp_nonexistent", "ghost@example.com")
		rec := httptest.NewRecorder()

		server.mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestOrganizations(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	server := testServer(t, db)

	// Create user
	kindeUserID := "kp_" + uuid.New().String()[:8]
	email := "org-" + uuid.New().String()[:8] + "@example.com"
	user, err := db.CreateUser(ctx, kindeUserID, email)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteUser(ctx, user.ID) })

	var orgID uuid.UUID

	t.Run("create organization", func(t *testing.T) {
		body := bytes.NewBufferString(`{"name": "Test Organization"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/organizations", body)
		req.Header.Set("Content-Type", "application/json")
		req = withAuthContext(req, kindeUserID, email)
		rec := httptest.NewRecorder()

		server.mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusCreated, rec.Code)

		var resp database.Organization
		err := json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "Test Organization", resp.Name)
		assert.Equal(t, database.TierFree, resp.Tier)
		orgID = resp.ID
	})

	t.Cleanup(func() {
		if orgID != uuid.Nil {
			_ = db.DeleteOrganization(ctx, orgID)
		}
	})

	t.Run("list organizations", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/organizations", nil)
		req = withAuthContext(req, kindeUserID, email)
		rec := httptest.NewRecorder()

		server.mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var resp map[string][]database.Organization
		err := json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Len(t, resp["organizations"], 1)
	})

	t.Run("get organization", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/organizations/"+orgID.String(), nil)
		req = withAuthContext(req, kindeUserID, email)
		rec := httptest.NewRecorder()

		server.mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var resp map[string]any
		err := json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, database.RoleOwner, resp["role"])
	})

	t.Run("get organization - forbidden for non-member", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/organizations/"+orgID.String(), nil)
		req = withAuthContext(req, "kp_other", "other@example.com")
		rec := httptest.NewRecorder()

		server.mux.ServeHTTP(rec, req)

		// Non-member will get unauthorized because user doesn't exist
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("create organization - empty name", func(t *testing.T) {
		body := bytes.NewBufferString(`{"name": ""}`)
		req := httptest.NewRequest(http.MethodPost, "/api/organizations", body)
		req.Header.Set("Content-Type", "application/json")
		req = withAuthContext(req, kindeUserID, email)
		rec := httptest.NewRecorder()

		server.mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

func TestRepositories(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	server := testServer(t, db)

	// Setup
	kindeUserID := "kp_" + uuid.New().String()[:8]
	email := "repo-" + uuid.New().String()[:8] + "@example.com"
	user, err := db.CreateUser(ctx, kindeUserID, email)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteUser(ctx, user.ID) })

	org, err := db.CreateOrganizationWithOwner(ctx, "Repo Test Org", user.ID)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteOrganization(ctx, org.ID) })

	repo, err := db.CreateRepository(ctx, org.ID, "testowner", "testrepo")
	require.NoError(t, err)

	t.Run("list repositories", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/organizations/"+org.ID.String()+"/repositories", nil)
		req = withAuthContext(req, kindeUserID, email)
		rec := httptest.NewRecorder()

		server.mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var resp map[string][]database.Repository
		err := json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Len(t, resp["repositories"], 1)
		assert.Equal(t, "testowner/testrepo", resp["repositories"][0].FullName)
	})

	t.Run("get repository", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/organizations/"+org.ID.String()+"/repositories/"+repo.ID.String(), nil)
		req = withAuthContext(req, kindeUserID, email)
		rec := httptest.NewRecorder()

		server.mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var resp map[string]any
		err := json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, float64(0), resp["analysis_count"])
	})

	t.Run("get repository - not found", func(t *testing.T) {
		fakeID := uuid.New()
		req := httptest.NewRequest(http.MethodGet, "/api/organizations/"+org.ID.String()+"/repositories/"+fakeID.String(), nil)
		req = withAuthContext(req, kindeUserID, email)
		rec := httptest.NewRecorder()

		server.mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestAnalyses(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	server := testServer(t, db)

	// Setup
	kindeUserID := "kp_" + uuid.New().String()[:8]
	email := "analysis-" + uuid.New().String()[:8] + "@example.com"
	user, err := db.CreateUser(ctx, kindeUserID, email)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteUser(ctx, user.ID) })

	org, err := db.CreateOrganizationWithOwner(ctx, "Analysis Test Org", user.ID)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteOrganization(ctx, org.ID) })

	repo, err := db.CreateRepository(ctx, org.ID, "analysisowner", "analysisrepo")
	require.NoError(t, err)

	analysis, err := db.CreateAnalysis(ctx, database.CreateAnalysisParams{
		RepoID:   repo.ID,
		RunID:    12345,
		Category: llm.CategoryDiagnosis,
		Text:     "Test analysis content",
	})
	require.NoError(t, err)

	t.Run("list analyses", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/organizations/"+org.ID.String()+"/repositories/"+repo.ID.String()+"/analyses", nil)
		req = withAuthContext(req, kindeUserID, email)
		rec := httptest.NewRecorder()

		server.mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var resp map[string]any
		err := json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, float64(1), resp["total"])
		assert.Len(t, resp["analyses"].([]any), 1)
	})

	t.Run("list analyses with pagination", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/organizations/"+org.ID.String()+"/repositories/"+repo.ID.String()+"/analyses?limit=10&offset=0", nil)
		req = withAuthContext(req, kindeUserID, email)
		rec := httptest.NewRecorder()

		server.mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var resp map[string]any
		err := json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, float64(10), resp["limit"])
		assert.Equal(t, float64(0), resp["offset"])
	})

	t.Run("get analysis", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/organizations/"+org.ID.String()+"/analyses/"+analysis.ID.String(), nil)
		req = withAuthContext(req, kindeUserID, email)
		rec := httptest.NewRecorder()

		server.mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var resp map[string]any
		err := json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.NotNil(t, resp["analysis"])
		assert.NotNil(t, resp["repository"])
	})

	t.Run("get analysis - not found", func(t *testing.T) {
		fakeID := uuid.New()
		req := httptest.NewRequest(http.MethodGet, "/api/organizations/"+org.ID.String()+"/analyses/"+fakeID.String(), nil)
		req = withAuthContext(req, kindeUserID, email)
		rec := httptest.NewRecorder()

		server.mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestUsage(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	server := testServer(t, db)

	// Setup
	kindeUserID := "kp_" + uuid.New().String()[:8]
	email := "usage-" + uuid.New().String()[:8] + "@example.com"
	user, err := db.CreateUser(ctx, kindeUserID, email)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteUser(ctx, user.ID) })

	org, err := db.CreateOrganizationWithOwner(ctx, "Usage Test Org", user.ID)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteOrganization(ctx, org.ID) })

	t.Run("get usage stats", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/organizations/"+org.ID.String()+"/usage", nil)
		req = withAuthContext(req, kindeUserID, email)
		rec := httptest.NewRecorder()

		server.mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var resp map[string]any
		err := json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, database.TierFree, resp["tier"])
		assert.Equal(t, float64(0), resp["used_this_month"])
		assert.Equal(t, float64(10), resp["limit"])
		assert.Equal(t, float64(10), resp["remaining"])
	})
}

func TestInvalidUUIDs(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	server := testServer(t, db)

	// Setup user and org for auth
	kindeUserID := "kp_" + uuid.New().String()[:8]
	email := "invalid-" + uuid.New().String()[:8] + "@example.com"
	user, err := db.CreateUser(ctx, kindeUserID, email)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteUser(ctx, user.ID) })

	org, err := db.CreateOrganizationWithOwner(ctx, "Invalid UUID Test Org", user.ID)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteOrganization(ctx, org.ID) })

	tests := []struct {
		name string
		path string
	}{
		{"invalid org ID", "/api/organizations/not-a-uuid"},
		{"invalid repo ID", "/api/organizations/" + org.ID.String() + "/repositories/not-a-uuid"},
		{"invalid analysis ID", "/api/organizations/" + org.ID.String() + "/analyses/not-a-uuid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			req = withAuthContext(req, kindeUserID, email)
			rec := httptest.NewRecorder()

			server.mux.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
}

func TestBillingCheckout(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	server := testServer(t, db)

	// Setup
	kindeUserID := "kp_" + uuid.New().String()[:8]
	email := "checkout-" + uuid.New().String()[:8] + "@example.com"
	user, err := db.CreateUser(ctx, kindeUserID, email)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteUser(ctx, user.ID) })

	org, err := db.CreateOrganizationWithOwner(ctx, "Checkout Test Org", user.ID)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteOrganization(ctx, org.ID) })

	t.Run("invalid request body", func(t *testing.T) {
		body := bytes.NewBufferString(`not valid json`)
		req := httptest.NewRequest(http.MethodPost, "/api/billing/checkout", body)
		req.Header.Set("Content-Type", "application/json")
		req = withAuthContext(req, kindeUserID, email)
		rec := httptest.NewRecorder()

		server.mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("invalid org ID", func(t *testing.T) {
		body := bytes.NewBufferString(`{
			"org_id": "not-a-uuid",
			"tier": "team",
			"success_url": "https://example.com/success",
			"cancel_url": "https://example.com/cancel"
		}`)
		req := httptest.NewRequest(http.MethodPost, "/api/billing/checkout", body)
		req.Header.Set("Content-Type", "application/json")
		req = withAuthContext(req, kindeUserID, email)
		rec := httptest.NewRecorder()

		server.mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("non-admin cannot create checkout", func(t *testing.T) {
		// Create another user who is not an admin
		otherKindeID := "kp_" + uuid.New().String()[:8]
		otherEmail := "other-" + uuid.New().String()[:8] + "@example.com"
		otherUser, err := db.CreateUser(ctx, otherKindeID, otherEmail)
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.DeleteUser(ctx, otherUser.ID) })

		// Add as member (not admin)
		err = db.AddOrgMember(ctx, org.ID, otherUser.ID, database.RoleMember)
		require.NoError(t, err)

		body := bytes.NewBufferString(`{
			"org_id": "` + org.ID.String() + `",
			"tier": "team",
			"success_url": "https://example.com/success",
			"cancel_url": "https://example.com/cancel"
		}`)
		req := httptest.NewRequest(http.MethodPost, "/api/billing/checkout", body)
		req.Header.Set("Content-Type", "application/json")
		req = withAuthContext(req, otherKindeID, otherEmail)
		rec := httptest.NewRecorder()

		server.mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("invalid tier returns error", func(t *testing.T) {
		body := bytes.NewBufferString(`{
			"org_id": "` + org.ID.String() + `",
			"tier": "free",
			"success_url": "https://example.com/success",
			"cancel_url": "https://example.com/cancel"
		}`)
		req := httptest.NewRequest(http.MethodPost, "/api/billing/checkout", body)
		req.Header.Set("Content-Type", "application/json")
		req = withAuthContext(req, kindeUserID, email)
		rec := httptest.NewRecorder()

		server.mux.ServeHTTP(rec, req)

		// Free tier has no price ID, so it should fail
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestBillingPortal(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	server := testServer(t, db)

	// Setup
	kindeUserID := "kp_" + uuid.New().String()[:8]
	email := "portal-" + uuid.New().String()[:8] + "@example.com"
	user, err := db.CreateUser(ctx, kindeUserID, email)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteUser(ctx, user.ID) })

	org, err := db.CreateOrganizationWithOwner(ctx, "Portal Test Org", user.ID)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteOrganization(ctx, org.ID) })

	t.Run("invalid request body", func(t *testing.T) {
		body := bytes.NewBufferString(`not valid json`)
		req := httptest.NewRequest(http.MethodPost, "/api/billing/portal", body)
		req.Header.Set("Content-Type", "application/json")
		req = withAuthContext(req, kindeUserID, email)
		rec := httptest.NewRecorder()

		server.mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("invalid org ID", func(t *testing.T) {
		body := bytes.NewBufferString(`{
			"org_id": "not-a-uuid",
			"return_url": "https://example.com/settings"
		}`)
		req := httptest.NewRequest(http.MethodPost, "/api/billing/portal", body)
		req.Header.Set("Content-Type", "application/json")
		req = withAuthContext(req, kindeUserID, email)
		rec := httptest.NewRecorder()

		server.mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("org without stripe customer", func(t *testing.T) {
		body := bytes.NewBufferString(`{
			"org_id": "` + org.ID.String() + `",
			"return_url": "https://example.com/settings"
		}`)
		req := httptest.NewRequest(http.MethodPost, "/api/billing/portal", body)
		req.Header.Set("Content-Type", "application/json")
		req = withAuthContext(req, kindeUserID, email)
		rec := httptest.NewRecorder()

		server.mux.ServeHTTP(rec, req)

		// Org has no Stripe customer ID, should return error
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "no billing account")
	})

	t.Run("non-admin cannot access portal", func(t *testing.T) {
		// Create another user who is not an admin
		otherKindeID := "kp_" + uuid.New().String()[:8]
		otherEmail := "other-portal-" + uuid.New().String()[:8] + "@example.com"
		otherUser, err := db.CreateUser(ctx, otherKindeID, otherEmail)
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.DeleteUser(ctx, otherUser.ID) })

		// Add as member (not admin)
		err = db.AddOrgMember(ctx, org.ID, otherUser.ID, database.RoleMember)
		require.NoError(t, err)

		body := bytes.NewBufferString(`{
			"org_id": "` + org.ID.String() + `",
			"return_url": "https://example.com/settings"
		}`)
		req := httptest.NewRequest(http.MethodPost, "/api/billing/portal", body)
		req.Header.Set("Content-Type", "application/json")
		req = withAuthContext(req, otherKindeID, otherEmail)
		rec := httptest.NewRecorder()

		server.mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusForbidden, rec.Code)
	})
}

func TestAuthSyncMissingClaims(t *testing.T) {
	db := testDB(t)
	server := testServer(t, db)

	// Request without auth context
	req := httptest.NewRequest(http.MethodPost, "/api/auth/sync", nil)
	rec := httptest.NewRecorder()

	server.mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGetMeMissingClaims(t *testing.T) {
	db := testDB(t)
	server := testServer(t, db)

	// Request without auth context
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	rec := httptest.NewRecorder()

	server.mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestOrganizationsMissingClaims(t *testing.T) {
	db := testDB(t)
	server := testServer(t, db)

	t.Run("list without auth", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/organizations", nil)
		rec := httptest.NewRecorder()

		server.mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("create without auth", func(t *testing.T) {
		body := bytes.NewBufferString(`{"name": "Test Org"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/organizations", body)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		server.mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})
}

func TestServerClose(t *testing.T) {
	db := testDB(t)
	server := testServer(t, db)

	// Close should not panic
	assert.NotPanics(t, func() {
		server.Close()
	})
}
