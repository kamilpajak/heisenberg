package database

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// User represents a Heisenberg user.
type User struct {
	ID        uuid.UUID
	ClerkID   string
	Email     string
	CreatedAt time.Time
}

// CreateUser creates a new user.
func (db *DB) CreateUser(ctx context.Context, clerkID, email string) (*User, error) {
	var user User
	err := db.pool.QueryRow(ctx,
		`INSERT INTO users (clerk_id, email)
		 VALUES ($1, $2)
		 RETURNING id, clerk_id, email, created_at`,
		clerkID, email,
	).Scan(&user.ID, &user.ClerkID, &user.Email, &user.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// GetUserByClerkID retrieves a user by their Clerk ID.
func (db *DB) GetUserByClerkID(ctx context.Context, clerkID string) (*User, error) {
	var user User
	err := db.pool.QueryRow(ctx,
		`SELECT id, clerk_id, email, created_at
		 FROM users WHERE clerk_id = $1`,
		clerkID,
	).Scan(&user.ID, &user.ClerkID, &user.Email, &user.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// GetUserByID retrieves a user by their ID.
func (db *DB) GetUserByID(ctx context.Context, id uuid.UUID) (*User, error) {
	var user User
	err := db.pool.QueryRow(ctx,
		`SELECT id, clerk_id, email, created_at
		 FROM users WHERE id = $1`,
		id,
	).Scan(&user.ID, &user.ClerkID, &user.Email, &user.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// GetOrCreateUser returns the user with the given Clerk ID, creating one if necessary.
func (db *DB) GetOrCreateUser(ctx context.Context, clerkID, email string) (*User, error) {
	user, err := db.GetUserByClerkID(ctx, clerkID)
	if err != nil {
		return nil, err
	}
	if user != nil {
		return user, nil
	}
	return db.CreateUser(ctx, clerkID, email)
}

// UpdateUserEmail updates a user's email.
func (db *DB) UpdateUserEmail(ctx context.Context, id uuid.UUID, email string) error {
	_, err := db.pool.Exec(ctx,
		`UPDATE users SET email = $1 WHERE id = $2`,
		email, id,
	)
	return err
}

// DeleteUser deletes a user by ID.
func (db *DB) DeleteUser(ctx context.Context, id uuid.UUID) error {
	_, err := db.pool.Exec(ctx,
		`DELETE FROM users WHERE id = $1`,
		id,
	)
	return err
}
