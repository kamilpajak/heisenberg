package billing

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/stripe/stripe-go/v76"
	"github.com/stripe/stripe-go/v76/webhook"
)

// WebhookVerifier defines the interface for verifying webhook signatures.
type WebhookVerifier interface {
	ConstructEvent(payload []byte, header string, secret string) (stripe.Event, error)
}

// DefaultWebhookVerifier uses the real Stripe webhook package.
type DefaultWebhookVerifier struct{}

// ConstructEvent verifies and constructs a Stripe event from webhook payload.
func (v *DefaultWebhookVerifier) ConstructEvent(payload []byte, header string, secret string) (stripe.Event, error) {
	return webhook.ConstructEvent(payload, header, secret)
}

// WebhookEvent represents a parsed Stripe webhook event.
type WebhookEvent struct {
	Type               string
	CustomerID         string
	SubscriptionID     string
	SubscriptionStatus string
	PriceID            string
	OrgID              string
}

// WebhookHandler handles Stripe webhook requests.
type WebhookHandler struct {
	client   *Client
	verifier WebhookVerifier
	onEvent  func(event WebhookEvent) error
}

// NewWebhookHandler creates a new webhook handler.
func NewWebhookHandler(client *Client, onEvent func(WebhookEvent) error) *WebhookHandler {
	return &WebhookHandler{
		client:   client,
		verifier: &DefaultWebhookVerifier{},
		onEvent:  onEvent,
	}
}

// NewWebhookHandlerWithVerifier creates a webhook handler with a custom verifier (for testing).
func NewWebhookHandlerWithVerifier(client *Client, verifier WebhookVerifier, onEvent func(WebhookEvent) error) *WebhookHandler {
	return &WebhookHandler{
		client:   client,
		verifier: verifier,
		onEvent:  onEvent,
	}
}

// ServeHTTP handles incoming Stripe webhooks.
func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	signature := r.Header.Get("Stripe-Signature")
	event, err := h.verifier.ConstructEvent(body, signature, h.client.config.WebhookSecret)
	if err != nil {
		http.Error(w, "invalid signature", http.StatusBadRequest)
		return
	}

	webhookEvent, err := h.parseEvent(&event)
	if err != nil {
		// Log but don't fail - we may not handle all event types
		w.WriteHeader(http.StatusOK)
		return
	}

	if h.onEvent != nil {
		if err := h.onEvent(webhookEvent); err != nil {
			http.Error(w, "handler error", http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
}

func (h *WebhookHandler) parseEvent(event *stripe.Event) (WebhookEvent, error) {
	we := WebhookEvent{
		Type: string(event.Type),
	}

	switch event.Type {
	case "checkout.session.completed":
		var session stripe.CheckoutSession
		if err := json.Unmarshal(event.Data.Raw, &session); err != nil {
			return we, err
		}
		we.CustomerID = session.Customer.ID
		we.SubscriptionID = session.Subscription.ID
		if session.Metadata != nil {
			we.OrgID = session.Metadata["org_id"]
		}

	case "customer.subscription.created",
		"customer.subscription.updated",
		"customer.subscription.deleted":
		var sub stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
			return we, err
		}
		we.CustomerID = sub.Customer.ID
		we.SubscriptionID = sub.ID
		we.SubscriptionStatus = string(sub.Status)
		if len(sub.Items.Data) > 0 {
			we.PriceID = sub.Items.Data[0].Price.ID
		}
		if sub.Metadata != nil {
			we.OrgID = sub.Metadata["org_id"]
		}

	case "invoice.payment_succeeded",
		"invoice.payment_failed":
		var invoice stripe.Invoice
		if err := json.Unmarshal(event.Data.Raw, &invoice); err != nil {
			return we, err
		}
		we.CustomerID = invoice.Customer.ID
		if invoice.Subscription != nil {
			we.SubscriptionID = invoice.Subscription.ID
		}

	default:
		return we, fmt.Errorf("unhandled event type: %s", event.Type)
	}

	return we, nil
}

// RelevantEventTypes returns the Stripe event types that should be handled.
func RelevantEventTypes() []string {
	return []string{
		"checkout.session.completed",
		"customer.subscription.created",
		"customer.subscription.updated",
		"customer.subscription.deleted",
		"invoice.payment_succeeded",
		"invoice.payment_failed",
	}
}
