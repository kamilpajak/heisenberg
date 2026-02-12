package billing

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stripe/stripe-go/v76"
)

func TestCreateCheckoutSession_InvalidTier(t *testing.T) {
	client := NewClient(Config{
		SecretKey: "sk_test_fake",
		PriceIDs: PriceIDs{
			Team:       "price_team_123",
			Enterprise: "price_ent_456",
		},
	})

	// Free tier has no price ID
	_, err := client.CreateCheckoutSession(CreateCheckoutParams{
		Email:      "test@example.com",
		OrgID:      "org_123",
		Tier:       TierFree,
		SuccessURL: "https://example.com/success",
		CancelURL:  "https://example.com/cancel",
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid tier")
}

func TestCreateCheckoutSession_UnknownTier(t *testing.T) {
	client := NewClient(Config{
		SecretKey: "sk_test_fake",
		PriceIDs: PriceIDs{
			Team:       "price_team_123",
			Enterprise: "price_ent_456",
		},
	})

	_, err := client.CreateCheckoutSession(CreateCheckoutParams{
		Email:      "test@example.com",
		OrgID:      "org_123",
		Tier:       "unknown_tier",
		SuccessURL: "https://example.com/success",
		CancelURL:  "https://example.com/cancel",
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid tier")
}

func TestCreateCheckoutParams_Fields(t *testing.T) {
	params := CreateCheckoutParams{
		CustomerID: "cus_123",
		Email:      "test@example.com",
		OrgID:      "org_456",
		Tier:       TierTeam,
		SuccessURL: "https://example.com/success",
		CancelURL:  "https://example.com/cancel",
	}

	assert.Equal(t, "cus_123", params.CustomerID)
	assert.Equal(t, "test@example.com", params.Email)
	assert.Equal(t, "org_456", params.OrgID)
	assert.Equal(t, TierTeam, params.Tier)
	assert.Equal(t, "https://example.com/success", params.SuccessURL)
	assert.Equal(t, "https://example.com/cancel", params.CancelURL)
}

func TestCreateCheckoutParams_EmptyCustomerID(t *testing.T) {
	params := CreateCheckoutParams{
		CustomerID: "",
		Email:      "test@example.com",
		OrgID:      "org_456",
		Tier:       TierTeam,
		SuccessURL: "https://example.com/success",
		CancelURL:  "https://example.com/cancel",
	}

	assert.Empty(t, params.CustomerID)
	assert.NotEmpty(t, params.Email)
}

func TestCreateCheckoutSession_EnterpriseTier(t *testing.T) {
	client := NewClient(Config{
		SecretKey: "sk_test_fake",
		PriceIDs: PriceIDs{
			Team:       "price_team_123",
			Enterprise: "price_ent_456",
		},
	})

	// Enterprise tier should have a price ID
	priceID := client.PriceIDFromTier(TierEnterprise)
	assert.Equal(t, "price_ent_456", priceID)
}

func TestCreateCheckoutSession_TeamTier(t *testing.T) {
	client := NewClient(Config{
		SecretKey: "sk_test_fake",
		PriceIDs: PriceIDs{
			Team:       "price_team_123",
			Enterprise: "price_ent_456",
		},
	})

	// Team tier should have a price ID
	priceID := client.PriceIDFromTier(TierTeam)
	assert.Equal(t, "price_team_123", priceID)
}

func TestPriceIDs_Fields(t *testing.T) {
	priceIDs := PriceIDs{
		Team:       "price_team_abc",
		Enterprise: "price_ent_xyz",
	}

	assert.Equal(t, "price_team_abc", priceIDs.Team)
	assert.Equal(t, "price_ent_xyz", priceIDs.Enterprise)
}

func TestConfig_WithWebhookSecret(t *testing.T) {
	cfg := Config{
		SecretKey:     "sk_test_123",
		WebhookSecret: "whsec_456",
		PriceIDs: PriceIDs{
			Team:       "price_team",
			Enterprise: "price_ent",
		},
	}

	assert.Equal(t, "sk_test_123", cfg.SecretKey)
	assert.Equal(t, "whsec_456", cfg.WebhookSecret)
	assert.Equal(t, "price_team", cfg.PriceIDs.Team)
	assert.Equal(t, "price_ent", cfg.PriceIDs.Enterprise)
}

func TestCreateCheckoutSession_WithMock(t *testing.T) {
	mockProvider := &MockStripeProvider{
		CreateCheckoutSessionFn: func(params *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) {
			return &stripe.CheckoutSession{
				ID:  "cs_test_123",
				URL: "https://checkout.stripe.com/test_session",
			}, nil
		},
	}

	client := NewClientWithProvider(Config{
		PriceIDs: PriceIDs{
			Team:       "price_team_123",
			Enterprise: "price_ent_456",
		},
	}, mockProvider)

	session, err := client.CreateCheckoutSession(CreateCheckoutParams{
		Email:      "test@example.com",
		OrgID:      "org_123",
		Tier:       TierTeam,
		SuccessURL: "https://example.com/success",
		CancelURL:  "https://example.com/cancel",
	})

	assert.NoError(t, err)
	assert.Equal(t, "cs_test_123", session.ID)
	assert.Equal(t, "https://checkout.stripe.com/test_session", session.URL)
}

func TestCreateCheckoutSession_WithCustomerID(t *testing.T) {
	var capturedParams *stripe.CheckoutSessionParams

	mockProvider := &MockStripeProvider{
		CreateCheckoutSessionFn: func(params *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) {
			capturedParams = params
			return &stripe.CheckoutSession{ID: "cs_test"}, nil
		},
	}

	client := NewClientWithProvider(Config{
		PriceIDs: PriceIDs{Team: "price_team"},
	}, mockProvider)

	_, err := client.CreateCheckoutSession(CreateCheckoutParams{
		CustomerID: "cus_existing",
		OrgID:      "org_123",
		Tier:       TierTeam,
		SuccessURL: "https://example.com/success",
		CancelURL:  "https://example.com/cancel",
	})

	assert.NoError(t, err)
	assert.Equal(t, "cus_existing", *capturedParams.Customer)
}

func TestCreateCheckoutSession_WithEmail(t *testing.T) {
	var capturedParams *stripe.CheckoutSessionParams

	mockProvider := &MockStripeProvider{
		CreateCheckoutSessionFn: func(params *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) {
			capturedParams = params
			return &stripe.CheckoutSession{ID: "cs_test"}, nil
		},
	}

	client := NewClientWithProvider(Config{
		PriceIDs: PriceIDs{Team: "price_team"},
	}, mockProvider)

	_, err := client.CreateCheckoutSession(CreateCheckoutParams{
		Email:      "new@example.com",
		OrgID:      "org_123",
		Tier:       TierTeam,
		SuccessURL: "https://example.com/success",
		CancelURL:  "https://example.com/cancel",
	})

	assert.NoError(t, err)
	assert.Equal(t, "new@example.com", *capturedParams.CustomerEmail)
}

func TestCreateCustomer_WithMock(t *testing.T) {
	mockProvider := &MockStripeProvider{
		CreateCustomerFn: func(params *stripe.CustomerParams) (*stripe.Customer, error) {
			return &stripe.Customer{
				ID:    "cus_new_123",
				Email: *params.Email,
				Name:  *params.Name,
			}, nil
		},
	}

	client := NewClientWithProvider(Config{}, mockProvider)

	customer, err := client.CreateCustomer("test@example.com", "Test User", "org_456")

	assert.NoError(t, err)
	assert.Equal(t, "cus_new_123", customer.ID)
	assert.Equal(t, "test@example.com", customer.Email)
	assert.Equal(t, "Test User", customer.Name)
}

func TestGetCustomer_WithMock(t *testing.T) {
	mockProvider := &MockStripeProvider{
		GetCustomerFn: func(id string) (*stripe.Customer, error) {
			return &stripe.Customer{
				ID:    id,
				Email: "retrieved@example.com",
			}, nil
		},
	}

	client := NewClientWithProvider(Config{}, mockProvider)

	customer, err := client.GetCustomer("cus_test_456")

	assert.NoError(t, err)
	assert.Equal(t, "cus_test_456", customer.ID)
	assert.Equal(t, "retrieved@example.com", customer.Email)
}

func TestCreatePortalSession_WithMock(t *testing.T) {
	mockProvider := &MockStripeProvider{
		CreatePortalSessionFn: func(params *stripe.BillingPortalSessionParams) (*stripe.BillingPortalSession, error) {
			return &stripe.BillingPortalSession{
				ID:  "bps_test_123",
				URL: "https://billing.stripe.com/session/test",
			}, nil
		},
	}

	client := NewClientWithProvider(Config{}, mockProvider)

	session, err := client.CreatePortalSession("cus_123", "https://example.com/return")

	assert.NoError(t, err)
	assert.Equal(t, "bps_test_123", session.ID)
	assert.Equal(t, "https://billing.stripe.com/session/test", session.URL)
}

func TestCreateCheckoutSession_ProviderError(t *testing.T) {
	mockProvider := &MockStripeProvider{
		CreateCheckoutSessionFn: func(params *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) {
			return nil, assert.AnError
		},
	}

	client := NewClientWithProvider(Config{
		PriceIDs: PriceIDs{Team: "price_team"},
	}, mockProvider)

	_, err := client.CreateCheckoutSession(CreateCheckoutParams{
		Email:      "test@example.com",
		OrgID:      "org_123",
		Tier:       TierTeam,
		SuccessURL: "https://example.com/success",
		CancelURL:  "https://example.com/cancel",
	})

	assert.Error(t, err)
}
