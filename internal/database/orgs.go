package database

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Tier constants for organization subscription levels.
const (
	TierFree       = "free"
	TierTeam       = "team"
	TierEnterprise = "enterprise"
)

// Role constants for organization membership.
const (
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"
)

// Organization represents a team or company.
type Organization struct {
	ID               uuid.UUID
	Name             string
	StripeCustomerID *string
	Tier             string
	CreatedAt        time.Time
}

// OrgMember represents a user's membership in an organization.
type OrgMember struct {
	OrgID     uuid.UUID
	UserID    uuid.UUID
	Role      string
	CreatedAt time.Time
}

// CreateOrganization creates a new organization.
func (db *DB) CreateOrganization(ctx context.Context, name string) (*Organization, error) {
	var org Organization
	err := db.pool.QueryRow(ctx,
		`INSERT INTO organizations (name)
		 VALUES ($1)
		 RETURNING id, name, stripe_customer_id, tier, created_at`,
		name,
	).Scan(&org.ID, &org.Name, &org.StripeCustomerID, &org.Tier, &org.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &org, nil
}

// GetOrganizationByID retrieves an organization by ID.
func (db *DB) GetOrganizationByID(ctx context.Context, id uuid.UUID) (*Organization, error) {
	var org Organization
	err := db.pool.QueryRow(ctx,
		`SELECT id, name, stripe_customer_id, tier, created_at
		 FROM organizations WHERE id = $1`,
		id,
	).Scan(&org.ID, &org.Name, &org.StripeCustomerID, &org.Tier, &org.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &org, nil
}

// UpdateOrganizationStripe updates Stripe customer ID and tier.
func (db *DB) UpdateOrganizationStripe(ctx context.Context, id uuid.UUID, customerID, tier string) error {
	_, err := db.pool.Exec(ctx,
		`UPDATE organizations SET stripe_customer_id = $1, tier = $2 WHERE id = $3`,
		customerID, tier, id,
	)
	return err
}

// UpdateOrganizationTier updates the subscription tier.
func (db *DB) UpdateOrganizationTier(ctx context.Context, id uuid.UUID, tier string) error {
	_, err := db.pool.Exec(ctx,
		`UPDATE organizations SET tier = $1 WHERE id = $2`,
		tier, id,
	)
	return err
}

// DeleteOrganization deletes an organization by ID.
func (db *DB) DeleteOrganization(ctx context.Context, id uuid.UUID) error {
	_, err := db.pool.Exec(ctx,
		`DELETE FROM organizations WHERE id = $1`,
		id,
	)
	return err
}

// AddOrgMember adds a user to an organization.
func (db *DB) AddOrgMember(ctx context.Context, orgID, userID uuid.UUID, role string) error {
	_, err := db.pool.Exec(ctx,
		`INSERT INTO org_members (org_id, user_id, role)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (org_id, user_id) DO UPDATE SET role = $3`,
		orgID, userID, role,
	)
	return err
}

// RemoveOrgMember removes a user from an organization.
func (db *DB) RemoveOrgMember(ctx context.Context, orgID, userID uuid.UUID) error {
	_, err := db.pool.Exec(ctx,
		`DELETE FROM org_members WHERE org_id = $1 AND user_id = $2`,
		orgID, userID,
	)
	return err
}

// GetOrgMember retrieves a user's membership in an organization.
func (db *DB) GetOrgMember(ctx context.Context, orgID, userID uuid.UUID) (*OrgMember, error) {
	var member OrgMember
	err := db.pool.QueryRow(ctx,
		`SELECT org_id, user_id, role, created_at
		 FROM org_members WHERE org_id = $1 AND user_id = $2`,
		orgID, userID,
	).Scan(&member.OrgID, &member.UserID, &member.Role, &member.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &member, nil
}

// ListUserOrganizations returns all organizations a user belongs to.
func (db *DB) ListUserOrganizations(ctx context.Context, userID uuid.UUID) ([]Organization, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT o.id, o.name, o.stripe_customer_id, o.tier, o.created_at
		 FROM organizations o
		 JOIN org_members m ON o.id = m.org_id
		 WHERE m.user_id = $1
		 ORDER BY o.created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orgs []Organization
	for rows.Next() {
		var org Organization
		if err := rows.Scan(&org.ID, &org.Name, &org.StripeCustomerID, &org.Tier, &org.CreatedAt); err != nil {
			return nil, err
		}
		orgs = append(orgs, org)
	}
	return orgs, rows.Err()
}

// ListOrgMembers returns all members of an organization.
func (db *DB) ListOrgMembers(ctx context.Context, orgID uuid.UUID) ([]OrgMember, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT org_id, user_id, role, created_at
		 FROM org_members WHERE org_id = $1
		 ORDER BY created_at`,
		orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []OrgMember
	for rows.Next() {
		var m OrgMember
		if err := rows.Scan(&m.OrgID, &m.UserID, &m.Role, &m.CreatedAt); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

// CreateOrganizationWithOwner creates an organization and adds a user as owner.
func (db *DB) CreateOrganizationWithOwner(ctx context.Context, name string, ownerUserID uuid.UUID) (*Organization, error) {
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var org Organization
	err = tx.QueryRow(ctx,
		`INSERT INTO organizations (name)
		 VALUES ($1)
		 RETURNING id, name, stripe_customer_id, tier, created_at`,
		name,
	).Scan(&org.ID, &org.Name, &org.StripeCustomerID, &org.Tier, &org.CreatedAt)
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO org_members (org_id, user_id, role)
		 VALUES ($1, $2, $3)`,
		org.ID, ownerUserID, RoleOwner,
	)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return &org, nil
}
