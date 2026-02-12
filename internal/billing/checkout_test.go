package billing

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
