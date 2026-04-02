package database

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pgvector/pgvector-go"
)

// RCAEmbedding represents a stored vector embedding for a single RCA.
type RCAEmbedding struct {
	ID            uuid.UUID
	AnalysisID    uuid.UUID
	RCAIndex      int
	OrgID         uuid.UUID
	FailureType   *string
	EmbeddingText string
	Embedding     pgvector.Vector
	CreatedAt     time.Time
}

// CreateEmbeddingParams contains parameters for creating an RCA embedding.
type CreateEmbeddingParams struct {
	AnalysisID    uuid.UUID
	RCAIndex      int
	OrgID         uuid.UUID
	FailureType   *string
	EmbeddingText string
	Embedding     pgvector.Vector
}

// SimilarRCA represents a similarity search result with joined analysis/repo data.
type SimilarRCA struct {
	ID            uuid.UUID
	AnalysisID    uuid.UUID
	RCAIndex      int
	FailureType   *string
	EmbeddingText string
	Similarity    float64
	RunID         int64
	Branch        *string
	RepoFullName  string
	CreatedAt     time.Time
}

// CreateRCAEmbedding stores a new RCA embedding.
func (db *DB) CreateRCAEmbedding(ctx context.Context, params CreateEmbeddingParams) (*RCAEmbedding, error) {
	var e RCAEmbedding
	err := db.pool.QueryRow(ctx,
		`INSERT INTO rca_embeddings (analysis_id, rca_index, org_id, failure_type, embedding_text, embedding)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, analysis_id, rca_index, org_id, failure_type, embedding_text, embedding, created_at`,
		params.AnalysisID, params.RCAIndex, params.OrgID, params.FailureType,
		params.EmbeddingText, params.Embedding,
	).Scan(&e.ID, &e.AnalysisID, &e.RCAIndex, &e.OrgID, &e.FailureType,
		&e.EmbeddingText, &e.Embedding, &e.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// GetRCAEmbeddingsByAnalysis returns all embeddings for a given analysis.
func (db *DB) GetRCAEmbeddingsByAnalysis(ctx context.Context, analysisID uuid.UUID) ([]RCAEmbedding, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT id, analysis_id, rca_index, org_id, failure_type, embedding_text, embedding, created_at
		 FROM rca_embeddings
		 WHERE analysis_id = $1
		 ORDER BY rca_index`,
		analysisID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEmbeddings(rows)
}

// FindSimilarRCAs finds RCA embeddings similar to the given vector within an organization.
// Results are ordered by cosine similarity descending. The excludeAnalysisID parameter
// prevents an analysis from matching itself. Pass uuid.Nil to skip exclusion (e.g. for
// free-text search where there is no source analysis).
func (db *DB) FindSimilarRCAs(ctx context.Context, orgID uuid.UUID, embedding pgvector.Vector,
	excludeAnalysisID uuid.UUID, limit int, threshold float64) ([]SimilarRCA, error) {

	if limit <= 0 {
		limit = 5
	}

	rows, err := db.pool.Query(ctx,
		`SELECT e.id, e.analysis_id, e.rca_index, e.failure_type, e.embedding_text,
		        1 - (e.embedding <=> $1::vector) AS similarity,
		        a.run_id, a.branch, r.full_name, a.created_at
		 FROM rca_embeddings e
		 JOIN analyses a ON e.analysis_id = a.id
		 JOIN repositories r ON a.repo_id = r.id
		 WHERE e.org_id = $2
		   AND e.embedding IS NOT NULL
		   AND e.analysis_id != $3
		   AND 1 - (e.embedding <=> $1::vector) >= $4
		 ORDER BY e.embedding <=> $1::vector
		 LIMIT $5`,
		embedding, orgID, excludeAnalysisID, threshold, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SimilarRCA
	for rows.Next() {
		var s SimilarRCA
		if err := rows.Scan(&s.ID, &s.AnalysisID, &s.RCAIndex, &s.FailureType,
			&s.EmbeddingText, &s.Similarity, &s.RunID, &s.Branch,
			&s.RepoFullName, &s.CreatedAt); err != nil {
			return nil, err
		}
		results = append(results, s)
	}
	return results, rows.Err()
}

func scanEmbeddings(rows pgx.Rows) ([]RCAEmbedding, error) {
	var embeddings []RCAEmbedding
	for rows.Next() {
		var e RCAEmbedding
		if err := rows.Scan(&e.ID, &e.AnalysisID, &e.RCAIndex, &e.OrgID,
			&e.FailureType, &e.EmbeddingText, &e.Embedding, &e.CreatedAt); err != nil {
			return nil, err
		}
		embeddings = append(embeddings, e)
	}
	return embeddings, rows.Err()
}
