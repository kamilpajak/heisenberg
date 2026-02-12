package auth

import (
	"context"

	"github.com/golang-jwt/jwt/v5"
)

// WithClaims returns a new context with the given claims.
// This is primarily for testing purposes.
func WithClaims(ctx context.Context, claims *KindeClaims) context.Context {
	return context.WithValue(ctx, claimsKey, claims)
}

// NewTestClaims creates a KindeClaims with the given user ID and email.
// This is primarily for testing purposes.
func NewTestClaims(userID, email string) *KindeClaims {
	return &KindeClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: userID,
		},
		Email: email,
	}
}
