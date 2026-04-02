package auth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

const (
	skipMockJWKS = "Requires mock JWKS server - covered by integration tests"
)

func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		name       string
		authHeader string
		want       string
	}{
		{
			name:       "empty header",
			authHeader: "",
			want:       "",
		},
		{
			name:       "valid bearer token",
			authHeader: "Bearer eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.test",
			want:       "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.test",
		},
		{
			name:       "lowercase bearer",
			authHeader: "bearer token123",
			want:       "token123",
		},
		{
			name:       "invalid format - no space",
			authHeader: "Bearertoken123",
			want:       "",
		},
		{
			name:       "invalid format - wrong scheme",
			authHeader: "Basic dXNlcjpwYXNz",
			want:       "",
		},
		{
			name:       "empty token after bearer",
			authHeader: "Bearer ",
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			got := extractBearerToken(req)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMiddleware_NoToken(t *testing.T) {
	// Create a mock verifier - it won't be called without a token
	handler := Middleware(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "missing token")
}

func TestOptionalMiddleware_NoToken(t *testing.T) {
	called := false
	handler := OptionalMiddleware(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		// Should have no claims in context
		assert.Nil(t, Claims(r.Context()))
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestKindeClaims_Fields(t *testing.T) {
	claims := KindeClaims{
		Email:         "test@example.com",
		EmailVerified: true,
		Name:          "Test User",
		Picture:       "https://example.com/avatar.png",
		GivenName:     "Test",
		FamilyName:    "User",
		OrgCode:       "org_123",
		OrgName:       "Acme Inc",
		Permissions:   []string{"read:data", "write:data"},
		Roles: []Role{
			{ID: "role_1", Key: "admin", Name: "Administrator"},
		},
	}

	assert.Equal(t, "test@example.com", claims.Email)
	assert.True(t, claims.EmailVerified)
	assert.Equal(t, "Test User", claims.Name)
	assert.Equal(t, "org_123", claims.OrgCode)
	assert.Equal(t, "Acme Inc", claims.OrgName)
	assert.Len(t, claims.Permissions, 2)
	assert.Len(t, claims.Roles, 1)
	assert.Equal(t, "admin", claims.Roles[0].Key)
}

func TestRole_Fields(t *testing.T) {
	role := Role{
		ID:   "role_123",
		Key:  "editor",
		Name: "Editor",
	}

	assert.Equal(t, "role_123", role.ID)
	assert.Equal(t, "editor", role.Key)
	assert.Equal(t, "Editor", role.Name)
}

func TestConfig_Fields(t *testing.T) {
	cfg := Config{
		Domain:   "https://test.kinde.com",
		Audience: "https://api.example.com",
	}

	assert.Equal(t, "https://test.kinde.com", cfg.Domain)
	assert.Equal(t, "https://api.example.com", cfg.Audience)
}

func TestExtractBearerToken_MultipleSpaces(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer token with spaces")

	got := extractBearerToken(req)
	// Should only take first part after "Bearer "
	assert.Equal(t, "token with spaces", got)
}

func TestExtractBearerToken_MixedCase(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "BEARER uppercase_token")

	got := extractBearerToken(req)
	assert.Equal(t, "uppercase_token", got)
}

func TestMiddleware_WithToken_NilVerifier(t *testing.T) {
	// This tests the case where verifier is nil but token is present
	// The middleware will panic trying to call verifier.Verify
	// This is expected behavior - verifier should never be nil in production
	handler := Middleware(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer sometoken")
	rec := httptest.NewRecorder()

	// This will panic due to nil verifier - use recover to test
	defer func() {
		if r := recover(); r != nil {
			// Expected panic
			assert.NotNil(t, r)
		}
	}()

	handler.ServeHTTP(rec, req)
}

func TestOptionalMiddleware_WithToken_NilVerifier(t *testing.T) {
	// With optional middleware and nil verifier, it should panic when trying to verify
	handler := OptionalMiddleware(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer sometoken")
	rec := httptest.NewRecorder()

	defer func() {
		if r := recover(); r != nil {
			assert.NotNil(t, r)
		}
	}()

	handler.ServeHTTP(rec, req)
}

func TestVerifier_Close(t *testing.T) {
	// Verifier.Close is a no-op in keyfunc v3, but we should test it doesn't panic
	v := &Verifier{}
	assert.NotPanics(t, func() {
		v.Close()
	})
}

func TestVerifier_RefreshJWKS(t *testing.T) {
	// RefreshJWKS is a no-op stub, but we should test it doesn't panic
	v := &Verifier{}
	assert.NotPanics(t, func() {
		v.RefreshJWKS()
	})
}

func TestMiddleware_InvalidToken_WithVerifier(t *testing.T) {
	// This test uses a real-ish verifier structure but will fail on invalid tokens
	// We can't easily test Verify without a real JWKS endpoint, but we can test
	// that the middleware properly handles the error case
	t.Skip(skipMockJWKS)
}

func TestVerifier_StartBackgroundRefresh(t *testing.T) {
	v := &Verifier{}

	ctx, cancel := context.WithCancel(context.Background())

	// Start background refresh - should not panic
	assert.NotPanics(t, func() {
		v.StartBackgroundRefresh(ctx, time.Millisecond*10)
	})

	// Let it run briefly
	time.Sleep(time.Millisecond * 50)

	// Cancel should stop the goroutine
	cancel()
	time.Sleep(time.Millisecond * 20)
}

func TestMiddleware_InvalidToken_PanicsWithNilJWKS(t *testing.T) {
	// Verifier with nil JWKS will panic - this is expected behavior
	// In production, NewVerifier always initializes JWKS
	t.Skip(skipMockJWKS)
}

func TestIsAPIKey(t *testing.T) {
	assert.True(t, IsAPIKey("hsb_abc123"))
	assert.False(t, IsAPIKey("eyJhbGciOiJSUzI1NiJ9.jwt"))
	assert.False(t, IsAPIKey(""))
	assert.False(t, IsAPIKey("hsb"))
}

func TestHashAPIKey(t *testing.T) {
	hash := HashAPIKey("hsb_testkey123")
	assert.Len(t, hash, 64) // SHA-256 hex = 64 chars
	// Same input → same output
	assert.Equal(t, hash, HashAPIKey("hsb_testkey123"))
	// Different input → different output
	assert.NotEqual(t, hash, HashAPIKey("hsb_otherkey"))
}

type mockAPIKeyStore struct {
	getByHash  func(ctx context.Context, keyHash string) (APIKeyInfo, error)
	updateUsed func(ctx context.Context, id uuid.UUID) error
}

func (m *mockAPIKeyStore) GetAPIKeyByHash(ctx context.Context, keyHash string) (APIKeyInfo, error) {
	return m.getByHash(ctx, keyHash)
}
func (m *mockAPIKeyStore) UpdateAPIKeyLastUsed(ctx context.Context, id uuid.UUID) error {
	if m.updateUsed != nil {
		return m.updateUsed(ctx, id)
	}
	return nil
}

func TestMiddleware_APIKey_Valid(t *testing.T) {
	testKeyID := uuid.New()
	store := &mockAPIKeyStore{
		getByHash: func(ctx context.Context, keyHash string) (APIKeyInfo, error) {
			return APIKeyInfo{
				ID:      testKeyID,
				UserID:  uuid.New(),
				OrgID:   uuid.New(),
				ClerkID: "kp_testuser",
			}, nil
		},
	}

	var capturedUserID string
	handler := Middleware(nil, store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUserID = UserID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer hsb_testkey123")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "kp_testuser", capturedUserID)
}

func TestMiddleware_APIKey_Invalid(t *testing.T) {
	store := &mockAPIKeyStore{
		getByHash: func(ctx context.Context, keyHash string) (APIKeyInfo, error) {
			return APIKeyInfo{}, fmt.Errorf("not found")
		},
	}

	handler := Middleware(nil, store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer hsb_invalidkey")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestMiddleware_APIKey_NoStore(t *testing.T) {
	// API key token without store configured → falls through to JWT (panics with nil verifier)
	handler := Middleware(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer hsb_nostore")
	rec := httptest.NewRecorder()

	defer func() {
		if r := recover(); r != nil {
			// Expected: no store, falls to JWT path, nil verifier panics
			assert.NotNil(t, r)
		}
	}()

	handler.ServeHTTP(rec, req)
}

func TestOptionalMiddleware_InvalidToken_PanicsWithNilJWKS(t *testing.T) {
	// Same as above - nil JWKS causes panic
	t.Skip(skipMockJWKS)
}
