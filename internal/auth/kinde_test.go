package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

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
