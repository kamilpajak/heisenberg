package auth

import (
	"context"
)

type contextKey int

const (
	claimsKey contextKey = iota
)

// Claims returns the Kinde claims from context, or nil if not authenticated.
func Claims(ctx context.Context) *KindeClaims {
	claims, _ := ctx.Value(claimsKey).(*KindeClaims)
	return claims
}

// UserID returns the Kinde user ID (subject) from context, or empty string if not authenticated.
func UserID(ctx context.Context) string {
	claims := Claims(ctx)
	if claims == nil {
		return ""
	}
	return claims.Subject
}

// Email returns the user's email from context, or empty string if not available.
func Email(ctx context.Context) string {
	claims := Claims(ctx)
	if claims == nil {
		return ""
	}
	return claims.Email
}

// OrgCode returns the Kinde organization code from context, or empty string if not available.
func OrgCode(ctx context.Context) string {
	claims := Claims(ctx)
	if claims == nil {
		return ""
	}
	return claims.OrgCode
}

// IsAuthenticated returns true if the request has valid authentication.
func IsAuthenticated(ctx context.Context) bool {
	return Claims(ctx) != nil
}

// HasPermission checks if the user has a specific permission.
func HasPermission(ctx context.Context, permission string) bool {
	claims := Claims(ctx)
	if claims == nil {
		return false
	}
	for _, p := range claims.Permissions {
		if p == permission {
			return true
		}
	}
	return false
}

// HasRole checks if the user has a specific role by key.
func HasRole(ctx context.Context, roleKey string) bool {
	claims := Claims(ctx)
	if claims == nil {
		return false
	}
	for _, r := range claims.Roles {
		if r.Key == roleKey {
			return true
		}
	}
	return false
}
