package billing

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/kamilpajak/heisenberg/internal/database"
	"github.com/stripe/stripe-go/v76"
)

// MockStripeProvider is a mock implementation of StripeProvider for testing.
type MockStripeProvider struct {
	CreateCheckoutSessionFn func(params *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error)
	CreateCustomerFn        func(params *stripe.CustomerParams) (*stripe.Customer, error)
	GetCustomerFn           func(id string) (*stripe.Customer, error)
	CreatePortalSessionFn   func(params *stripe.BillingPortalSessionParams) (*stripe.BillingPortalSession, error)
}

// CreateCheckoutSession calls the mock function.
func (m *MockStripeProvider) CreateCheckoutSession(params *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) {
	if m.CreateCheckoutSessionFn != nil {
		return m.CreateCheckoutSessionFn(params)
	}
	return &stripe.CheckoutSession{URL: "https://checkout.stripe.com/test"}, nil
}

// CreateCustomer calls the mock function.
func (m *MockStripeProvider) CreateCustomer(params *stripe.CustomerParams) (*stripe.Customer, error) {
	if m.CreateCustomerFn != nil {
		return m.CreateCustomerFn(params)
	}
	return &stripe.Customer{ID: "cus_mock123"}, nil
}

// GetCustomer calls the mock function.
func (m *MockStripeProvider) GetCustomer(id string) (*stripe.Customer, error) {
	if m.GetCustomerFn != nil {
		return m.GetCustomerFn(id)
	}
	return &stripe.Customer{ID: id}, nil
}

// CreatePortalSession calls the mock function.
func (m *MockStripeProvider) CreatePortalSession(params *stripe.BillingPortalSessionParams) (*stripe.BillingPortalSession, error) {
	if m.CreatePortalSessionFn != nil {
		return m.CreatePortalSessionFn(params)
	}
	return &stripe.BillingPortalSession{URL: "https://billing.stripe.com/test"}, nil
}

// MockWebhookVerifier is a mock implementation of WebhookVerifier for testing.
type MockWebhookVerifier struct {
	ConstructEventFn func(payload []byte, header string, secret string) (stripe.Event, error)
}

// ConstructEvent calls the mock function.
func (m *MockWebhookVerifier) ConstructEvent(payload []byte, header string, secret string) (stripe.Event, error) {
	if m.ConstructEventFn != nil {
		return m.ConstructEventFn(payload, header, secret)
	}
	return stripe.Event{}, nil
}

// MockUsageDB is a mock implementation of UsageDB for testing.
type MockUsageDB struct {
	GetOrganizationByIDFn   func(ctx context.Context, id uuid.UUID) (*database.Organization, error)
	CountOrgAnalysesSinceFn func(ctx context.Context, orgID uuid.UUID, since time.Time) (int, error)
}

// GetOrganizationByID calls the mock function.
func (m *MockUsageDB) GetOrganizationByID(ctx context.Context, id uuid.UUID) (*database.Organization, error) {
	if m.GetOrganizationByIDFn != nil {
		return m.GetOrganizationByIDFn(ctx, id)
	}
	return &database.Organization{ID: id, Tier: TierFree}, nil
}

// CountOrgAnalysesSince calls the mock function.
func (m *MockUsageDB) CountOrgAnalysesSince(ctx context.Context, orgID uuid.UUID, since time.Time) (int, error) {
	if m.CountOrgAnalysesSinceFn != nil {
		return m.CountOrgAnalysesSinceFn(ctx, orgID, since)
	}
	return 0, nil
}
