// Package billing provides Stripe integration for subscription management.
package billing

import (
	"github.com/stripe/stripe-go/v76"
	portalsession "github.com/stripe/stripe-go/v76/billingportal/session"
	"github.com/stripe/stripe-go/v76/checkout/session"
	"github.com/stripe/stripe-go/v76/customer"
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

// StripeProvider defines the interface for Stripe API operations.
// This allows mocking in tests.
type StripeProvider interface {
	CreateCheckoutSession(params *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error)
	CreateCustomer(params *stripe.CustomerParams) (*stripe.Customer, error)
	GetCustomer(id string) (*stripe.Customer, error)
	CreatePortalSession(params *stripe.BillingPortalSessionParams) (*stripe.BillingPortalSession, error)
}

// DefaultStripeProvider implements StripeProvider using the real Stripe SDK.
type DefaultStripeProvider struct{}

// CreateCheckoutSession creates a checkout session via Stripe SDK.
func (p *DefaultStripeProvider) CreateCheckoutSession(params *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) {
	return session.New(params)
}

// CreateCustomer creates a customer via Stripe SDK.
func (p *DefaultStripeProvider) CreateCustomer(params *stripe.CustomerParams) (*stripe.Customer, error) {
	return customer.New(params)
}

// GetCustomer retrieves a customer via Stripe SDK.
func (p *DefaultStripeProvider) GetCustomer(id string) (*stripe.Customer, error) {
	return customer.Get(id, nil)
}

// CreatePortalSession creates a billing portal session via Stripe SDK.
func (p *DefaultStripeProvider) CreatePortalSession(params *stripe.BillingPortalSessionParams) (*stripe.BillingPortalSession, error) {
	return portalsession.New(params)
}

// Client wraps Stripe operations.
type Client struct {
	config   Config
	provider StripeProvider
}

// NewClient creates a new Stripe client.
func NewClient(cfg Config) *Client {
	stripe.Key = cfg.SecretKey
	return &Client{
		config:   cfg,
		provider: &DefaultStripeProvider{},
	}
}

// NewClientWithProvider creates a new Stripe client with a custom provider (for testing).
func NewClientWithProvider(cfg Config, provider StripeProvider) *Client {
	return &Client{
		config:   cfg,
		provider: provider,
	}
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
