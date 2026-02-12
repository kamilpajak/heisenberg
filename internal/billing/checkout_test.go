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
