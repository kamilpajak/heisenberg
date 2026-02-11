package api

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/kamilpajak/heisenberg/internal/billing"
	"github.com/kamilpajak/heisenberg/internal/database"
)

// handleGetUsage returns usage statistics for an organization.
func (s *Server) handleGetUsage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := s.getCurrentUser(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	orgID, err := uuid.Parse(r.PathValue("orgID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid organization ID")
		return
	}

	// Check membership
	member, err := s.db.GetOrgMember(ctx, orgID, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	if member == nil {
		writeError(w, http.StatusForbidden, "not a member of this organization")
		return
	}

	stats, err := s.usageChecker.GetUsageStats(ctx, orgID)
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
	ctx := r.Context()
	user, err := s.getCurrentUser(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

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

	orgID, err := uuid.Parse(req.OrgID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid organization ID")
		return
	}

	// Check membership and role
	member, err := s.db.GetOrgMember(ctx, orgID, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	if member == nil {
		writeError(w, http.StatusForbidden, "not a member of this organization")
		return
	}
	if member.Role != database.RoleOwner && member.Role != database.RoleAdmin {
		writeError(w, http.StatusForbidden, "only owners and admins can manage billing")
		return
	}

	org, err := s.db.GetOrganizationByID(ctx, orgID)
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
		Email:      user.Email,
		OrgID:      orgID.String(),
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
	ctx := r.Context()
	user, err := s.getCurrentUser(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	var req struct {
		OrgID     string `json:"org_id"`
		ReturnURL string `json:"return_url"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	orgID, err := uuid.Parse(req.OrgID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid organization ID")
		return
	}

	// Check membership
	member, err := s.db.GetOrgMember(ctx, orgID, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	if member == nil {
		writeError(w, http.StatusForbidden, "not a member of this organization")
		return
	}
	if member.Role != database.RoleOwner && member.Role != database.RoleAdmin {
		writeError(w, http.StatusForbidden, "only owners and admins can manage billing")
		return
	}

	org, err := s.db.GetOrganizationByID(ctx, orgID)
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

		switch event.Type {
		case "checkout.session.completed":
			// Update org with Stripe customer ID
			if event.OrgID != "" && event.CustomerID != "" {
				orgID, err := uuid.Parse(event.OrgID)
				if err != nil {
					return nil // Log and continue
				}
				tier := s.billingClient.TierFromPriceID(event.PriceID)
				return s.db.UpdateOrganizationStripe(ctx, orgID, event.CustomerID, tier)
			}

		case "customer.subscription.updated":
			if event.OrgID != "" {
				orgID, err := uuid.Parse(event.OrgID)
				if err != nil {
					return nil
				}
				tier := s.billingClient.TierFromPriceID(event.PriceID)
				return s.db.UpdateOrganizationTier(ctx, orgID, tier)
			}

		case "customer.subscription.deleted":
			if event.OrgID != "" {
				orgID, err := uuid.Parse(event.OrgID)
				if err != nil {
					return nil
				}
				return s.db.UpdateOrganizationTier(ctx, orgID, billing.TierFree)
			}
		}

		return nil
	})
}
