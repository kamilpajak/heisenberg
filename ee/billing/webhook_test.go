package billing

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stripe/stripe-go/v76"
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

func TestNewWebhookHandler(t *testing.T) {
	client := NewClient(Config{
		WebhookSecret: "whsec_test",
	})

	var receivedEvent WebhookEvent
	handler := NewWebhookHandler(client, func(event WebhookEvent) error {
		receivedEvent = event
		return nil
	})

	assert.NotNil(t, handler)
	assert.NotNil(t, handler.client)
	assert.NotNil(t, handler.onEvent)

	// Test that onEvent callback is stored correctly
	testEvent := WebhookEvent{Type: "test.event"}
	_ = handler.onEvent(testEvent)
	assert.Equal(t, "test.event", receivedEvent.Type)
}

func TestWebhookHandler_EmptyBody(t *testing.T) {
	client := NewClient(Config{
		WebhookSecret: "whsec_test123",
	})

	handler := NewWebhookHandler(client, func(event WebhookEvent) error {
		return nil
	})

	req := httptest.NewRequest(http.MethodPost, "/webhook", nil)
	req.Header.Set("Stripe-Signature", "t=123,v1=abc")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestWebhookHandler_NilCallback(t *testing.T) {
	client := NewClient(Config{
		WebhookSecret: "whsec_test123",
	})

	// Handler with nil callback should not panic
	handler := NewWebhookHandler(client, nil)
	assert.NotNil(t, handler)
	assert.Nil(t, handler.onEvent)
}

func TestWebhookHandler_WithMockVerifier_CheckoutCompleted(t *testing.T) {
	client := NewClient(Config{
		WebhookSecret: "whsec_test",
	})

	var receivedEvent WebhookEvent

	mockVerifier := &MockWebhookVerifier{
		ConstructEventFn: func(payload []byte, header string, secret string) (stripe.Event, error) {
			return stripe.Event{
				Type: "checkout.session.completed",
				Data: &stripe.EventData{
					Raw: []byte(`{
						"id": "cs_123",
						"customer": {"id": "cus_456"},
						"subscription": {"id": "sub_789"},
						"metadata": {"org_id": "org_abc"}
					}`),
				},
			}, nil
		},
	}

	handler := NewWebhookHandlerWithVerifier(client, mockVerifier, func(event WebhookEvent) error {
		receivedEvent = event
		return nil
	})

	body := bytes.NewBufferString(`{"type": "checkout.session.completed"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", body)
	req.Header.Set("Stripe-Signature", "valid_sig")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "checkout.session.completed", receivedEvent.Type)
	assert.Equal(t, "cus_456", receivedEvent.CustomerID)
	assert.Equal(t, "sub_789", receivedEvent.SubscriptionID)
	assert.Equal(t, "org_abc", receivedEvent.OrgID)
}

func TestWebhookHandler_WithMockVerifier_SubscriptionCreated(t *testing.T) {
	client := NewClient(Config{
		WebhookSecret: "whsec_test",
	})

	var receivedEvent WebhookEvent

	mockVerifier := &MockWebhookVerifier{
		ConstructEventFn: func(payload []byte, header string, secret string) (stripe.Event, error) {
			return stripe.Event{
				Type: "customer.subscription.created",
				Data: &stripe.EventData{
					Raw: []byte(`{
						"id": "sub_123",
						"customer": {"id": "cus_456"},
						"status": "active",
						"items": {"data": [{"price": {"id": "price_team"}}]},
						"metadata": {"org_id": "org_def"}
					}`),
				},
			}, nil
		},
	}

	handler := NewWebhookHandlerWithVerifier(client, mockVerifier, func(event WebhookEvent) error {
		receivedEvent = event
		return nil
	})

	body := bytes.NewBufferString(`{"type": "customer.subscription.created"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", body)
	req.Header.Set("Stripe-Signature", "valid_sig")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "customer.subscription.created", receivedEvent.Type)
	assert.Equal(t, "cus_456", receivedEvent.CustomerID)
	assert.Equal(t, "sub_123", receivedEvent.SubscriptionID)
	assert.Equal(t, "active", receivedEvent.SubscriptionStatus)
	assert.Equal(t, "price_team", receivedEvent.PriceID)
}

func TestWebhookHandler_WithMockVerifier_SubscriptionUpdated(t *testing.T) {
	client := NewClient(Config{WebhookSecret: "whsec_test"})

	var receivedEvent WebhookEvent

	mockVerifier := &MockWebhookVerifier{
		ConstructEventFn: func(payload []byte, header string, secret string) (stripe.Event, error) {
			return stripe.Event{
				Type: "customer.subscription.updated",
				Data: &stripe.EventData{
					Raw: []byte(`{
						"id": "sub_123",
						"customer": {"id": "cus_456"},
						"status": "past_due",
						"items": {"data": [{"price": {"id": "price_ent"}}]}
					}`),
				},
			}, nil
		},
	}

	handler := NewWebhookHandlerWithVerifier(client, mockVerifier, func(event WebhookEvent) error {
		receivedEvent = event
		return nil
	})

	body := bytes.NewBufferString(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", body)
	req.Header.Set("Stripe-Signature", "valid")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "customer.subscription.updated", receivedEvent.Type)
	assert.Equal(t, "past_due", receivedEvent.SubscriptionStatus)
}

func TestWebhookHandler_WithMockVerifier_SubscriptionDeleted(t *testing.T) {
	client := NewClient(Config{WebhookSecret: "whsec_test"})

	var receivedEvent WebhookEvent

	mockVerifier := &MockWebhookVerifier{
		ConstructEventFn: func(payload []byte, header string, secret string) (stripe.Event, error) {
			return stripe.Event{
				Type: "customer.subscription.deleted",
				Data: &stripe.EventData{
					Raw: []byte(`{
						"id": "sub_deleted",
						"customer": {"id": "cus_789"},
						"status": "canceled",
						"items": {"data": []}
					}`),
				},
			}, nil
		},
	}

	handler := NewWebhookHandlerWithVerifier(client, mockVerifier, func(event WebhookEvent) error {
		receivedEvent = event
		return nil
	})

	body := bytes.NewBufferString(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", body)
	req.Header.Set("Stripe-Signature", "valid")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "customer.subscription.deleted", receivedEvent.Type)
	assert.Equal(t, "canceled", receivedEvent.SubscriptionStatus)
}

func TestWebhookHandler_WithMockVerifier_InvoicePaymentSucceeded(t *testing.T) {
	client := NewClient(Config{WebhookSecret: "whsec_test"})

	var receivedEvent WebhookEvent

	mockVerifier := &MockWebhookVerifier{
		ConstructEventFn: func(payload []byte, header string, secret string) (stripe.Event, error) {
			return stripe.Event{
				Type: "invoice.payment_succeeded",
				Data: &stripe.EventData{
					Raw: []byte(`{
						"id": "inv_123",
						"customer": {"id": "cus_invoice"},
						"subscription": {"id": "sub_invoice"}
					}`),
				},
			}, nil
		},
	}

	handler := NewWebhookHandlerWithVerifier(client, mockVerifier, func(event WebhookEvent) error {
		receivedEvent = event
		return nil
	})

	body := bytes.NewBufferString(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", body)
	req.Header.Set("Stripe-Signature", "valid")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "invoice.payment_succeeded", receivedEvent.Type)
	assert.Equal(t, "cus_invoice", receivedEvent.CustomerID)
	assert.Equal(t, "sub_invoice", receivedEvent.SubscriptionID)
}

func TestWebhookHandler_WithMockVerifier_InvoicePaymentFailed(t *testing.T) {
	client := NewClient(Config{WebhookSecret: "whsec_test"})

	var receivedEvent WebhookEvent

	mockVerifier := &MockWebhookVerifier{
		ConstructEventFn: func(payload []byte, header string, secret string) (stripe.Event, error) {
			return stripe.Event{
				Type: "invoice.payment_failed",
				Data: &stripe.EventData{
					Raw: []byte(`{
						"id": "inv_failed",
						"customer": {"id": "cus_failed"}
					}`),
				},
			}, nil
		},
	}

	handler := NewWebhookHandlerWithVerifier(client, mockVerifier, func(event WebhookEvent) error {
		receivedEvent = event
		return nil
	})

	body := bytes.NewBufferString(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", body)
	req.Header.Set("Stripe-Signature", "valid")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "invoice.payment_failed", receivedEvent.Type)
	assert.Equal(t, "cus_failed", receivedEvent.CustomerID)
}

func TestWebhookHandler_WithMockVerifier_UnhandledEvent(t *testing.T) {
	client := NewClient(Config{WebhookSecret: "whsec_test"})

	mockVerifier := &MockWebhookVerifier{
		ConstructEventFn: func(payload []byte, header string, secret string) (stripe.Event, error) {
			return stripe.Event{
				Type: "unhandled.event.type",
				Data: &stripe.EventData{
					Raw: []byte(`{}`),
				},
			}, nil
		},
	}

	var called bool
	handler := NewWebhookHandlerWithVerifier(client, mockVerifier, func(event WebhookEvent) error {
		called = true
		return nil
	})

	body := bytes.NewBufferString(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", body)
	req.Header.Set("Stripe-Signature", "valid")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Unhandled events return 200 but don't call the handler
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.False(t, called)
}

func TestWebhookHandler_WithMockVerifier_HandlerError(t *testing.T) {
	client := NewClient(Config{WebhookSecret: "whsec_test"})

	mockVerifier := &MockWebhookVerifier{
		ConstructEventFn: func(payload []byte, header string, secret string) (stripe.Event, error) {
			return stripe.Event{
				Type: "checkout.session.completed",
				Data: &stripe.EventData{
					Raw: []byte(`{
						"customer": {"id": "cus_123"},
						"subscription": {"id": "sub_456"},
						"metadata": {"org_id": "org_789"}
					}`),
				},
			}, nil
		},
	}

	handler := NewWebhookHandlerWithVerifier(client, mockVerifier, func(event WebhookEvent) error {
		return assert.AnError
	})

	body := bytes.NewBufferString(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", body)
	req.Header.Set("Stripe-Signature", "valid")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestWebhookHandler_WithMockVerifier_NilCallback(t *testing.T) {
	client := NewClient(Config{WebhookSecret: "whsec_test"})

	mockVerifier := &MockWebhookVerifier{
		ConstructEventFn: func(payload []byte, header string, secret string) (stripe.Event, error) {
			return stripe.Event{
				Type: "checkout.session.completed",
				Data: &stripe.EventData{
					Raw: []byte(`{
						"customer": {"id": "cus_123"},
						"subscription": {"id": "sub_456"},
						"metadata": {"org_id": "org_789"}
					}`),
				},
			}, nil
		},
	}

	// Handler with nil callback
	handler := NewWebhookHandlerWithVerifier(client, mockVerifier, nil)

	body := bytes.NewBufferString(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", body)
	req.Header.Set("Stripe-Signature", "valid")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Should succeed even with nil callback
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestDefaultWebhookVerifier(t *testing.T) {
	verifier := &DefaultWebhookVerifier{}

	// Test with invalid signature (will fail but shouldn't panic)
	_, err := verifier.ConstructEvent([]byte(`{}`), "invalid", "whsec_test")
	assert.Error(t, err)
}

func TestDefaultStripeProvider(t *testing.T) {
	// Just ensure the provider can be created
	provider := &DefaultStripeProvider{}
	assert.NotNil(t, provider)
}
