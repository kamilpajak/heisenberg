package billing

import (
	"fmt"

	"github.com/stripe/stripe-go/v76"
)

// CreateCheckoutParams contains parameters for creating a checkout session.
type CreateCheckoutParams struct {
	CustomerID string // Stripe customer ID (optional, will create if empty)
	Email      string // Customer email (required if no CustomerID)
	OrgID      string // Organization ID for metadata
	Tier       string // Target tier
	SuccessURL string
	CancelURL  string
}

// CreateCheckoutSession creates a Stripe checkout session for subscription.
func (c *Client) CreateCheckoutSession(params CreateCheckoutParams) (*stripe.CheckoutSession, error) {
	priceID := c.PriceIDFromTier(params.Tier)
	if priceID == "" {
		return nil, fmt.Errorf("invalid tier: %s", params.Tier)
	}

	sessionParams := &stripe.CheckoutSessionParams{
		Mode: stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(priceID),
				Quantity: stripe.Int64(1),
			},
		},
		SuccessURL: stripe.String(params.SuccessURL),
		CancelURL:  stripe.String(params.CancelURL),
		Metadata: map[string]string{
			"org_id": params.OrgID,
		},
	}

	if params.CustomerID != "" {
		sessionParams.Customer = stripe.String(params.CustomerID)
	} else if params.Email != "" {
		sessionParams.CustomerEmail = stripe.String(params.Email)
	}

	return c.provider.CreateCheckoutSession(sessionParams)
}

// CreateCustomer creates a new Stripe customer.
func (c *Client) CreateCustomer(email, name, orgID string) (*stripe.Customer, error) {
	params := &stripe.CustomerParams{
		Email: stripe.String(email),
		Name:  stripe.String(name),
		Metadata: map[string]string{
			"org_id": orgID,
		},
	}
	return c.provider.CreateCustomer(params)
}

// GetCustomer retrieves a Stripe customer by ID.
func (c *Client) GetCustomer(customerID string) (*stripe.Customer, error) {
	return c.provider.GetCustomer(customerID)
}

// CreatePortalSession creates a Stripe billing portal session.
func (c *Client) CreatePortalSession(customerID, returnURL string) (*stripe.BillingPortalSession, error) {
	params := &stripe.BillingPortalSessionParams{
		Customer:  stripe.String(customerID),
		ReturnURL: stripe.String(returnURL),
	}
	return c.provider.CreatePortalSession(params)
}
