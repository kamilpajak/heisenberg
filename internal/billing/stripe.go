// Package billing provides Stripe integration for subscription management.
package billing

import (
	"github.com/stripe/stripe-go/v76"
)

// Config holds Stripe configuration.
type Config struct {
	SecretKey     string
	WebhookSecret string
	PriceIDs      PriceIDs
}

// PriceIDs holds Stripe price IDs for each tier.
type PriceIDs struct {
	Team       string
	Enterprise string
}

// Tier constants matching database.
const (
	TierFree       = "free"
	TierTeam       = "team"
	TierEnterprise = "enterprise"
)

// UsageLimits defines analysis limits per tier per month.
var UsageLimits = map[string]int{
	TierFree:       10,
	TierTeam:       1000,
	TierEnterprise: -1, // Unlimited
}

// Client wraps Stripe operations.
type Client struct {
	config Config
}

// NewClient creates a new Stripe client.
func NewClient(cfg Config) *Client {
	stripe.Key = cfg.SecretKey
	return &Client{config: cfg}
}

// GetConfig returns the client configuration.
func (c *Client) GetConfig() Config {
	return c.config
}

// TierFromPriceID returns the tier for a given Stripe price ID.
func (c *Client) TierFromPriceID(priceID string) string {
	switch priceID {
	case c.config.PriceIDs.Team:
		return TierTeam
	case c.config.PriceIDs.Enterprise:
		return TierEnterprise
	default:
		return TierFree
	}
}

// PriceIDFromTier returns the Stripe price ID for a given tier.
func (c *Client) PriceIDFromTier(tier string) string {
	switch tier {
	case TierTeam:
		return c.config.PriceIDs.Team
	case TierEnterprise:
		return c.config.PriceIDs.Enterprise
	default:
		return ""
	}
}
