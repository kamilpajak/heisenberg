package billing

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWebhookHandler_MissingSignature(t *testing.T) {
	client := NewClient(Config{
		WebhookSecret: "whsec_test123",
	})

	var called bool
	handler := NewWebhookHandler(client, func(event WebhookEvent) error {
		called = true
		return nil
	})

	body := bytes.NewBufferString(`{"type": "test"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", body)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.False(t, called, "handler should not be called for invalid signature")
}

func TestWebhookHandler_InvalidSignature(t *testing.T) {
	client := NewClient(Config{
		WebhookSecret: "whsec_test123",
	})

	handler := NewWebhookHandler(client, func(event WebhookEvent) error {
		return nil
	})

	body := bytes.NewBufferString(`{"type": "test"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", body)
	req.Header.Set("Stripe-Signature", "invalid_signature")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid signature")
}

func TestWebhookEvent_Fields(t *testing.T) {
	event := WebhookEvent{
		Type:               "checkout.session.completed",
		CustomerID:         "cus_123",
		SubscriptionID:     "sub_456",
		SubscriptionStatus: "active",
		PriceID:            "price_789",
		OrgID:              "org_abc",
	}

	assert.Equal(t, "checkout.session.completed", event.Type)
	assert.Equal(t, "cus_123", event.CustomerID)
	assert.Equal(t, "sub_456", event.SubscriptionID)
	assert.Equal(t, "active", event.SubscriptionStatus)
	assert.Equal(t, "price_789", event.PriceID)
	assert.Equal(t, "org_abc", event.OrgID)
}
