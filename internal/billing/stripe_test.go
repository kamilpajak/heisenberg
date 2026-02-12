package billing

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewClient(t *testing.T) {
	cfg := Config{
		SecretKey:     "sk_test_123",
		WebhookSecret: "whsec_123",
		PriceIDs: PriceIDs{
			Team:       "price_team",
			Enterprise: "price_enterprise",
		},
	}

	client := NewClient(cfg)
	assert.NotNil(t, client)
	assert.Equal(t, cfg, client.GetConfig())
}

func TestTierFromPriceID(t *testing.T) {
	client := NewClient(Config{
		PriceIDs: PriceIDs{
			Team:       "price_team_123",
			Enterprise: "price_ent_456",
		},
	})

	tests := []struct {
		priceID string
		want    string
	}{
		{"price_team_123", TierTeam},
		{"price_ent_456", TierEnterprise},
		{"price_unknown", TierFree},
		{"", TierFree},
	}

	for _, tt := range tests {
		t.Run(tt.priceID, func(t *testing.T) {
			got := client.TierFromPriceID(tt.priceID)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPriceIDFromTier(t *testing.T) {
	client := NewClient(Config{
		PriceIDs: PriceIDs{
			Team:       "price_team_123",
			Enterprise: "price_ent_456",
		},
	})

	tests := []struct {
		tier string
		want string
	}{
		{TierTeam, "price_team_123"},
		{TierEnterprise, "price_ent_456"},
		{TierFree, ""},
		{"unknown", ""},
	}

	for _, tt := range tests {
		t.Run(tt.tier, func(t *testing.T) {
			got := client.PriceIDFromTier(tt.tier)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestUsageLimits(t *testing.T) {
	assert.Equal(t, 10, UsageLimits[TierFree])
	assert.Equal(t, 1000, UsageLimits[TierTeam])
	assert.Equal(t, -1, UsageLimits[TierEnterprise])
}

func TestRelevantEventTypes(t *testing.T) {
	types := RelevantEventTypes()
	assert.Contains(t, types, "checkout.session.completed")
	assert.Contains(t, types, "customer.subscription.created")
	assert.Contains(t, types, "customer.subscription.updated")
	assert.Contains(t, types, "customer.subscription.deleted")
	assert.Contains(t, types, "invoice.payment_succeeded")
	assert.Contains(t, types, "invoice.payment_failed")
}
