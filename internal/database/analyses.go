package database

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/kamilpajak/heisenberg/pkg/llm"
)

// Analysis represents a stored test failure analysis.
type Analysis struct {
	ID          uuid.UUID
	RepoID      uuid.UUID
	RunID       int64
	Branch      *string
	CommitSHA   *string
	Category    string
	Confidence  *int
	Sensitivity *string
	RCA         *llm.RootCauseAnalysis
	Text        string
	CreatedAt   time.Time
}

// CreateAnalysisParams contains parameters for creating an analysis.
type CreateAnalysisParams struct {
	RepoID      uuid.UUID
	RunID       int64
	Branch      *string
	CommitSHA   *string
	Category    string
	Confidence  *int
	Sensitivity *string
	RCA         *llm.RootCauseAnalysis
	Text        string
}

// analysisColumns is the standard column list for analysis queries.
const analysisColumns = `id, repo_id, run_id, branch, commit_sha, category, confidence, sensitivity, rca, text, created_at`

// scanAnalysis scans a row into an Analysis struct and unmarshals the RCA JSON.
func scanAnalysis(row pgx.Row) (*Analysis, error) {
	var a Analysis
	var rcaJSON []byte
	err := row.Scan(
		&a.ID, &a.RepoID, &a.RunID, &a.Branch, &a.CommitSHA,
		&a.Category, &a.Confidence, &a.Sensitivity, &rcaJSON, &a.Text, &a.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := unmarshalRCA(rcaJSON, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// unmarshalRCA unmarshals RCA JSON into an Analysis if present.
func unmarshalRCA(rcaJSON []byte, a *Analysis) error {
	if rcaJSON != nil {
		a.RCA = &llm.RootCauseAnalysis{}
		return json.Unmarshal(rcaJSON, a.RCA)
	}
	return nil
}

// CreateAnalysis stores a new analysis result.
func (db *DB) CreateAnalysis(ctx context.Context, params CreateAnalysisParams) (*Analysis, error) {
	var rcaJSON []byte
	var err error
	if params.RCA != nil {
		rcaJSON, err = json.Marshal(params.RCA)
		if err != nil {
			return nil, err
		}
	}

	row := db.pool.QueryRow(ctx,
		`INSERT INTO analyses (repo_id, run_id, branch, commit_sha, category, confidence, sensitivity, rca, text)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING `+analysisColumns,
		params.RepoID, params.RunID, params.Branch, params.CommitSHA, params.Category,
		params.Confidence, params.Sensitivity, rcaJSON, params.Text,
	)
	return scanAnalysis(row)
}

// GetAnalysisByID retrieves an analysis by ID.
func (db *DB) GetAnalysisByID(ctx context.Context, id uuid.UUID) (*Analysis, error) {
	row := db.pool.QueryRow(ctx,
		`SELECT `+analysisColumns+` FROM analyses WHERE id = $1`,
		id,
	)
	return scanAnalysis(row)
}

// GetAnalysisByRunID retrieves an analysis by repository and run ID.
func (db *DB) GetAnalysisByRunID(ctx context.Context, repoID uuid.UUID, runID int64) (*Analysis, error) {
	row := db.pool.QueryRow(ctx,
		`SELECT `+analysisColumns+` FROM analyses WHERE repo_id = $1 AND run_id = $2`,
		repoID, runID,
	)
	return scanAnalysis(row)
}

// ListRepoAnalysesParams contains parameters for listing analyses.
type ListRepoAnalysesParams struct {
	RepoID   uuid.UUID
	Limit    int
	Offset   int
	Category *string
}

// ListRepoAnalyses returns analyses for a repository, ordered by creation date descending.
func (db *DB) ListRepoAnalyses(ctx context.Context, params ListRepoAnalysesParams) ([]Analysis, error) {
	if params.Limit <= 0 {
		params.Limit = 50
	}

	var rows pgx.Rows
	var err error

	if params.Category != nil {
		rows, err = db.pool.Query(ctx,
			`SELECT `+analysisColumns+` FROM analyses
			 WHERE repo_id = $1 AND category = $2
			 ORDER BY created_at DESC
			 LIMIT $3 OFFSET $4`,
			params.RepoID, *params.Category, params.Limit, params.Offset,
		)
	} else {
		rows, err = db.pool.Query(ctx,
			`SELECT `+analysisColumns+` FROM analyses
			 WHERE repo_id = $1
			 ORDER BY created_at DESC
			 LIMIT $2 OFFSET $3`,
			params.RepoID, params.Limit, params.Offset,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var analyses []Analysis
	for rows.Next() {
		var a Analysis
		var rcaJSON []byte
		if err := rows.Scan(
			&a.ID, &a.RepoID, &a.RunID, &a.Branch, &a.CommitSHA,
			&a.Category, &a.Confidence, &a.Sensitivity, &rcaJSON, &a.Text, &a.CreatedAt,
		); err != nil {
			return nil, err
		}
		if err := unmarshalRCA(rcaJSON, &a); err != nil {
			return nil, err
		}
		analyses = append(analyses, a)
	}
	return analyses, rows.Err()
}

// CountRepoAnalyses returns the total number of analyses for a repository.
func (db *DB) CountRepoAnalyses(ctx context.Context, repoID uuid.UUID) (int, error) {
	var count int
	err := db.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM analyses WHERE repo_id = $1`,
		repoID,
	).Scan(&count)
	return count, err
}

// CountOrgAnalysesSince returns the number of analyses for an organization since a given time.
func (db *DB) CountOrgAnalysesSince(ctx context.Context, orgID uuid.UUID, since time.Time) (int, error) {
	var count int
	err := db.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM analyses a
		 JOIN repositories r ON a.repo_id = r.id
		 WHERE r.org_id = $1 AND a.created_at >= $2`,
		orgID, since,
	).Scan(&count)
	return count, err
}

// DeleteAnalysis deletes an analysis by ID.
func (db *DB) DeleteAnalysis(ctx context.Context, id uuid.UUID) error {
	_, err := db.pool.Exec(ctx,
		`DELETE FROM analyses WHERE id = $1`,
		id,
	)
	return err
}

// DeleteOldAnalyses deletes analyses older than the specified duration.
func (db *DB) DeleteOldAnalyses(ctx context.Context, olderThan time.Time) (int64, error) {
	result, err := db.pool.Exec(ctx,
		`DELETE FROM analyses WHERE created_at < $1`,
		olderThan,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}
