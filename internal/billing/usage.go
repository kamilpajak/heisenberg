package billing

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/kamilpajak/heisenberg/internal/database"
)

// UsageDB defines the database operations needed by UsageChecker.
type UsageDB interface {
	GetOrganizationByID(ctx context.Context, id uuid.UUID) (*database.Organization, error)
	CountOrgAnalysesSince(ctx context.Context, orgID uuid.UUID, since time.Time) (int, error)
}

// UsageChecker provides methods to check and enforce usage limits.
type UsageChecker struct {
	db UsageDB
}

// NewUsageChecker creates a new usage checker.
func NewUsageChecker(db UsageDB) *UsageChecker {
	return &UsageChecker{db: db}
}

// UsageStats contains current usage information for an organization.
type UsageStats struct {
	OrgID         uuid.UUID
	Tier          string
	UsedThisMonth int
	Limit         int
	Remaining     int
	ResetDate     time.Time
}

// GetUsageStats returns current usage statistics for an organization.
func (uc *UsageChecker) GetUsageStats(ctx context.Context, orgID uuid.UUID) (*UsageStats, error) {
	org, err := uc.db.GetOrganizationByID(ctx, orgID)
	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, fmt.Errorf("organization not found: %s", orgID)
	}

	// Get start of current month
	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	nextMonth := monthStart.AddDate(0, 1, 0)

	// Count analyses this month
	count, err := uc.db.CountOrgAnalysesSince(ctx, orgID, monthStart)
	if err != nil {
		return nil, err
	}

	limit := UsageLimits[org.Tier]
	remaining := limit - count
	if limit == -1 {
		remaining = -1 // Unlimited
	} else if remaining < 0 {
		remaining = 0
	}

	return &UsageStats{
		OrgID:         orgID,
		Tier:          org.Tier,
		UsedThisMonth: count,
		Limit:         limit,
		Remaining:     remaining,
		ResetDate:     nextMonth,
	}, nil
}

// CanAnalyze checks if an organization can perform another analysis.
func (uc *UsageChecker) CanAnalyze(ctx context.Context, orgID uuid.UUID) (bool, error) {
	stats, err := uc.GetUsageStats(ctx, orgID)
	if err != nil {
		return false, err
	}

	// Unlimited tier
	if stats.Limit == -1 {
		return true, nil
	}

	return stats.Remaining > 0, nil
}

// CheckAndDeduct verifies usage limits and returns an error if exceeded.
// This should be called before performing an analysis.
func (uc *UsageChecker) CheckAndDeduct(ctx context.Context, orgID uuid.UUID) error {
	can, err := uc.CanAnalyze(ctx, orgID)
	if err != nil {
		return err
	}
	if !can {
		stats, _ := uc.GetUsageStats(ctx, orgID)
		return &LimitExceededError{
			OrgID:     orgID,
			Tier:      stats.Tier,
			Limit:     stats.Limit,
			Used:      stats.UsedThisMonth,
			ResetDate: stats.ResetDate,
		}
	}
	return nil
}

// LimitExceededError is returned when an organization exceeds their usage limit.
type LimitExceededError struct {
	OrgID     uuid.UUID
	Tier      string
	Limit     int
	Used      int
	ResetDate time.Time
}

func (e *LimitExceededError) Error() string {
	return fmt.Sprintf(
		"usage limit exceeded: %d/%d analyses used this month (tier: %s, resets: %s)",
		e.Used, e.Limit, e.Tier, e.ResetDate.Format("2006-01-02"),
	)
}

// IsLimitExceeded checks if an error is a LimitExceededError.
func IsLimitExceeded(err error) bool {
	_, ok := err.(*LimitExceededError)
	return ok
}
