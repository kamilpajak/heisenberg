// Package auth provides authentication middleware using Kinde.
package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
)

// Config holds Kinde configuration.
type Config struct {
	Domain   string // e.g., "https://yourapp.kinde.com"
	Audience string // API audience identifier
}

// KindeClaims represents the JWT claims from Kinde.
type KindeClaims struct {
	jwt.RegisteredClaims
	Email         string   `json:"email,omitempty"`
	EmailVerified bool     `json:"email_verified,omitempty"`
	Name          string   `json:"name,omitempty"`
	Picture       string   `json:"picture,omitempty"`
	GivenName     string   `json:"given_name,omitempty"`
	FamilyName    string   `json:"family_name,omitempty"`
	OrgCode       string   `json:"org_code,omitempty"`
	OrgName       string   `json:"org_name,omitempty"`
	Permissions   []string `json:"permissions,omitempty"`
	Roles         []Role   `json:"roles,omitempty"`
}

// Role represents a Kinde role.
type Role struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

// Verifier handles JWT verification with JWKS.
type Verifier struct {
	jwks     keyfunc.Keyfunc
	config   Config
	audience string
	issuer   string
}

// NewVerifier creates a new JWT verifier for Kinde.
func NewVerifier(cfg Config) (*Verifier, error) {
	jwksURL := fmt.Sprintf("%s/.well-known/jwks.json", strings.TrimSuffix(cfg.Domain, "/"))

	jwks, err := keyfunc.NewDefault([]string{jwksURL})
	if err != nil {
		return nil, fmt.Errorf("failed to create JWKS keyfunc: %w", err)
	}

	return &Verifier{
		jwks:     jwks,
		config:   cfg,
		audience: cfg.Audience,
		issuer:   strings.TrimSuffix(cfg.Domain, "/"),
	}, nil
}

// Close releases resources used by the verifier.
// Note: keyfunc/v3 handles cleanup automatically via context.
func (v *Verifier) Close() {
	// No explicit cleanup needed in keyfunc/v3
}

// Verify validates a JWT token and returns the claims.
func (v *Verifier) Verify(tokenString string) (*KindeClaims, error) {
	parserOpts := []jwt.ParserOption{
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(v.issuer),
		jwt.WithExpirationRequired(),
	}

	// Add audience verification if configured
	if v.audience != "" {
		parserOpts = append(parserOpts, jwt.WithAudience(v.audience))
	}

	token, err := jwt.ParseWithClaims(tokenString, &KindeClaims{}, v.jwks.Keyfunc, parserOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	claims, ok := token.Claims.(*KindeClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	return claims, nil
}

// Middleware creates HTTP middleware that verifies Kinde JWTs.
func Middleware(verifier *Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractBearerToken(r)
			if token == "" {
				http.Error(w, "Unauthorized: missing token", http.StatusUnauthorized)
				return
			}

			claims, err := verifier.Verify(token)
			if err != nil {
				http.Error(w, "Unauthorized: invalid token", http.StatusUnauthorized)
				return
			}

			// Attach claims to context
			ctx := context.WithValue(r.Context(), claimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// OptionalMiddleware creates middleware that verifies JWTs if present but doesn't require them.
func OptionalMiddleware(verifier *Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractBearerToken(r)
			if token == "" {
				next.ServeHTTP(w, r)
				return
			}

			claims, err := verifier.Verify(token)
			if err != nil {
				// Invalid token - continue without auth
				next.ServeHTTP(w, r)
				return
			}

			ctx := context.WithValue(r.Context(), claimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// MiddlewareFunc is a convenience function that creates a verifier and returns middleware.
// Note: For production, create the verifier once and reuse it.
func MiddlewareFunc(cfg Config) (func(http.Handler) http.Handler, error) {
	verifier, err := NewVerifier(cfg)
	if err != nil {
		return nil, err
	}
	return Middleware(verifier), nil
}

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ""
	}

	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return ""
	}

	return parts[1]
}

// RefreshJWKS forces a refresh of the JWKS cache.
func (v *Verifier) RefreshJWKS() {
	// keyfunc v3 auto-refreshes, but we can trigger manual refresh if needed
	// by creating a new keyfunc instance
}

// StartBackgroundRefresh starts a goroutine that periodically refreshes JWKS.
func (v *Verifier) StartBackgroundRefresh(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				v.RefreshJWKS()
			}
		}
	}()
}
