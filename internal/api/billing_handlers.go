package api

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/kamilpajak/heisenberg/internal/billing"
)

// handleGetUsage returns usage statistics for an organization.
func (s *Server) handleGetUsage(w http.ResponseWriter, r *http.Request) {
	oc, ok := s.requireOrgMember(w, r)
	if !ok {
		return
	}

	stats, err := s.usageChecker.GetUsageStats(r.Context(), oc.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get usage stats")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"tier":            stats.Tier,
		"used_this_month": stats.UsedThisMonth,
		"limit":           stats.Limit,
		"remaining":       stats.Remaining,
		"reset_date":      stats.ResetDate,
	})
}

// handleCreateCheckout creates a Stripe checkout session.
func (s *Server) handleCreateCheckout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrgID      string `json:"org_id"`
		Tier       string `json:"tier"`
		SuccessURL string `json:"success_url"`
		CancelURL  string `json:"cancel_url"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	oc, ok := s.requireOrgAdminFromBody(w, r, req.OrgID)
	if !ok {
		return
	}

	org, err := s.db.GetOrganizationByID(r.Context(), oc.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	customerID := ""
	if org.StripeCustomerID != nil {
		customerID = *org.StripeCustomerID
	}

	session, err := s.billingClient.CreateCheckoutSession(billing.CreateCheckoutParams{
		CustomerID: customerID,
		Email:      oc.User.Email,
		OrgID:      oc.OrgID.String(),
		Tier:       req.Tier,
		SuccessURL: req.SuccessURL,
		CancelURL:  req.CancelURL,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create checkout session")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"checkout_url": session.URL,
	})
}

// handleCreatePortal creates a Stripe billing portal session.
func (s *Server) handleCreatePortal(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrgID     string `json:"org_id"`
		ReturnURL string `json:"return_url"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	oc, ok := s.requireOrgAdminFromBody(w, r, req.OrgID)
	if !ok {
		return
	}

	org, err := s.db.GetOrganizationByID(r.Context(), oc.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	if org.StripeCustomerID == nil || *org.StripeCustomerID == "" {
		writeError(w, http.StatusBadRequest, "organization has no billing account")
		return
	}

	session, err := s.billingClient.CreatePortalSession(*org.StripeCustomerID, req.ReturnURL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create portal session")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"portal_url": session.URL,
	})
}

// createWebhookHandler returns the Stripe webhook handler.
func (s *Server) createWebhookHandler() http.Handler {
	return billing.NewWebhookHandler(s.billingClient, func(event billing.WebhookEvent) error {
		ctx := context.Background()

		if event.OrgID == "" {
			return nil
		}

		orgID, err := uuid.Parse(event.OrgID)
		if err != nil {
			return nil
		}

		switch event.Type {
		case "checkout.session.completed":
			if event.CustomerID != "" {
				tier := s.billingClient.TierFromPriceID(event.PriceID)
				return s.db.UpdateOrganizationStripe(ctx, orgID, event.CustomerID, tier)
			}
		case "customer.subscription.updated":
			tier := s.billingClient.TierFromPriceID(event.PriceID)
			return s.db.UpdateOrganizationTier(ctx, orgID, tier)
		case "customer.subscription.deleted":
			return s.db.UpdateOrganizationTier(ctx, orgID, billing.TierFree)
		}

		return nil
	})
}
