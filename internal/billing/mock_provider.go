package billing

import (
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
