package auth

import (
	"context"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
)

func TestClaims(t *testing.T) {
	t.Run("returns nil for empty context", func(t *testing.T) {
		ctx := context.Background()
		assert.Nil(t, Claims(ctx))
	})

	t.Run("returns claims from context", func(t *testing.T) {
		claims := &KindeClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject: "user_123",
			},
			Email: "test@example.com",
		}
		ctx := context.WithValue(context.Background(), claimsKey, claims)

		got := Claims(ctx)
		assert.NotNil(t, got)
		assert.Equal(t, "user_123", got.Subject)
		assert.Equal(t, "test@example.com", got.Email)
	})
}

func TestUserID(t *testing.T) {
	t.Run("returns empty string for empty context", func(t *testing.T) {
		ctx := context.Background()
		assert.Equal(t, "", UserID(ctx))
	})

	t.Run("returns user ID from claims", func(t *testing.T) {
		claims := &KindeClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject: "kp_abc123",
			},
		}
		ctx := context.WithValue(context.Background(), claimsKey, claims)
		assert.Equal(t, "kp_abc123", UserID(ctx))
	})
}

func TestEmail(t *testing.T) {
	t.Run("returns empty string for empty context", func(t *testing.T) {
		ctx := context.Background()
		assert.Equal(t, "", Email(ctx))
	})

	t.Run("returns email from claims", func(t *testing.T) {
		claims := &KindeClaims{
			Email: "user@example.com",
		}
		ctx := context.WithValue(context.Background(), claimsKey, claims)
		assert.Equal(t, "user@example.com", Email(ctx))
	})
}

func TestOrgCode(t *testing.T) {
	t.Run("returns empty string for empty context", func(t *testing.T) {
		ctx := context.Background()
		assert.Equal(t, "", OrgCode(ctx))
	})

	t.Run("returns org code from claims", func(t *testing.T) {
		claims := &KindeClaims{
			OrgCode: "org_acme",
		}
		ctx := context.WithValue(context.Background(), claimsKey, claims)
		assert.Equal(t, "org_acme", OrgCode(ctx))
	})
}

func TestIsAuthenticated(t *testing.T) {
	t.Run("returns false for empty context", func(t *testing.T) {
		ctx := context.Background()
		assert.False(t, IsAuthenticated(ctx))
	})

	t.Run("returns true when claims present", func(t *testing.T) {
		claims := &KindeClaims{}
		ctx := context.WithValue(context.Background(), claimsKey, claims)
		assert.True(t, IsAuthenticated(ctx))
	})
}

func TestHasPermission(t *testing.T) {
	t.Run("returns false for empty context", func(t *testing.T) {
		ctx := context.Background()
		assert.False(t, HasPermission(ctx, "read:users"))
	})

	t.Run("returns false for missing permission", func(t *testing.T) {
		claims := &KindeClaims{
			Permissions: []string{"read:users", "write:users"},
		}
		ctx := context.WithValue(context.Background(), claimsKey, claims)
		assert.False(t, HasPermission(ctx, "delete:users"))
	})

	t.Run("returns true for existing permission", func(t *testing.T) {
		claims := &KindeClaims{
			Permissions: []string{"read:users", "write:users"},
		}
		ctx := context.WithValue(context.Background(), claimsKey, claims)
		assert.True(t, HasPermission(ctx, "read:users"))
	})
}

func TestHasRole(t *testing.T) {
	t.Run("returns false for empty context", func(t *testing.T) {
		ctx := context.Background()
		assert.False(t, HasRole(ctx, "admin"))
	})

	t.Run("returns false for missing role", func(t *testing.T) {
		claims := &KindeClaims{
			Roles: []Role{
				{Key: "user", Name: "User"},
				{Key: "moderator", Name: "Moderator"},
			},
		}
		ctx := context.WithValue(context.Background(), claimsKey, claims)
		assert.False(t, HasRole(ctx, "admin"))
	})

	t.Run("returns true for existing role", func(t *testing.T) {
		claims := &KindeClaims{
			Roles: []Role{
				{Key: "admin", Name: "Administrator"},
			},
		}
		ctx := context.WithValue(context.Background(), claimsKey, claims)
		assert.True(t, HasRole(ctx, "admin"))
	})
}
