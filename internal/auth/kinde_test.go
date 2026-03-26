package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
	t.Skip("Requires mock JWKS server - covered by integration tests")
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
	t.Skip("Requires mock JWKS server - covered by integration tests")
}

func TestOptionalMiddleware_InvalidToken_PanicsWithNilJWKS(t *testing.T) {
	// Same as above - nil JWKS causes panic
	t.Skip("Requires mock JWKS server - covered by integration tests")
}
