package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pgvector/pgvector-go"

	"github.com/kamilpajak/heisenberg/ee/database"
	eepatterns "github.com/kamilpajak/heisenberg/ee/patterns"
	"github.com/kamilpajak/heisenberg/pkg/llm"
)

const errDatabase = "database error"

// handleListAnalyses returns analyses for a repository.
func (s *Server) handleListAnalyses(w http.ResponseWriter, r *http.Request) {
	oc, ok := s.requireOrgMember(w, r)
	if !ok {
		return
	}

	repoID, err := parseRepoID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repository ID")
		return
	}

	// Verify repo belongs to org
	repo, err := s.db.GetRepositoryByID(r.Context(), repoID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errDatabase)
		return
	}
	if repo == nil || repo.OrgID != oc.OrgID {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	// Parse query params
	limit, offset := parsePagination(r)
	var category *string
	if c := r.URL.Query().Get("category"); c != "" {
		category = &c
	}

	analyses, err := s.db.ListRepoAnalyses(r.Context(), database.ListRepoAnalysesParams{
		RepoID:   repoID,
		Limit:    limit,
		Offset:   offset,
		Category: category,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list analyses")
		return
	}

	total, err := s.db.CountRepoAnalyses(r.Context(), repoID, category)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count analyses")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"analyses": analyses,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
	})
}

// handleGetAnalysis returns a single analysis.
func (s *Server) handleGetAnalysis(w http.ResponseWriter, r *http.Request) {
	oc, ok := s.requireOrgMember(w, r)
	if !ok {
		return
	}

	analysisID, err := parseAnalysisID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid analysis ID")
		return
	}

	analysis, err := s.db.GetAnalysisByID(r.Context(), analysisID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errDatabase)
		return
	}
	if analysis == nil {
		writeError(w, http.StatusNotFound, "analysis not found")
		return
	}

	// Verify analysis belongs to a repo in this org
	repo, err := s.db.GetRepositoryByID(r.Context(), analysis.RepoID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errDatabase)
		return
	}
	if repo == nil || repo.OrgID != oc.OrgID {
		writeError(w, http.StatusNotFound, "analysis not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"analysis":   analysis,
		"repository": repo,
	})
}

// parsePagination extracts limit and offset from query parameters with defaults.
func parsePagination(r *http.Request) (limit, offset int) {
	limit = 50
	offset = 0

	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	return limit, offset
}

// handleCreateAnalysis accepts an analysis result from the CLI and persists it.
func (s *Server) handleCreateAnalysis(w http.ResponseWriter, r *http.Request) {
	oc, ok := s.requireOrgMember(w, r)
	if !ok {
		return
	}

	var req struct {
		Owner       string                  `json:"owner"`
		Repo        string                  `json:"repo"`
		RunID       int64                   `json:"run_id"`
		Branch      string                  `json:"branch"`
		CommitSHA   string                  `json:"commit_sha"`
		Category    string                  `json:"category"`
		Confidence  *int                    `json:"confidence"`
		Sensitivity string                  `json:"sensitivity"`
		RCAs        []llm.RootCauseAnalysis `json:"analyses"`
		Text        string                  `json:"text"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Owner == "" || req.Repo == "" || req.RunID == 0 || req.Category == "" || req.Text == "" {
		writeError(w, http.StatusBadRequest, "owner, repo, run_id, category, and text are required")
		return
	}

	// Check usage limit
	canAnalyze, err := s.usageChecker.CanAnalyze(r.Context(), oc.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check usage")
		return
	}
	if !canAnalyze {
		writeError(w, http.StatusTooManyRequests, "monthly analysis limit exceeded")
		return
	}

	// Get or create repository
	repo, err := s.db.GetOrCreateRepository(r.Context(), oc.OrgID, req.Owner, req.Repo)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve repository")
		return
	}

	// Build params
	params := database.CreateAnalysisParams{
		RepoID:   repo.ID,
		RunID:    req.RunID,
		Category: req.Category,
		Text:     req.Text,
		RCAs:     req.RCAs,
	}
	if req.Branch != "" {
		params.Branch = &req.Branch
	}
	if req.CommitSHA != "" {
		params.CommitSHA = &req.CommitSHA
	}
	if req.Confidence != nil {
		params.Confidence = req.Confidence
	}
	if req.Sensitivity != "" {
		params.Sensitivity = &req.Sensitivity
	}

	analysis, err := s.db.CreateAnalysis(r.Context(), params)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "analysis for this run already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create analysis")
		return
	}

	// Generate embeddings asynchronously for pattern matching
	if req.Category == llm.CategoryDiagnosis && s.embeddingClient != nil {
		go s.generateEmbeddings(context.Background(), analysis, oc.OrgID)
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id": analysis.ID,
	})
}

// generateEmbeddings creates vector embeddings for each RCA in an analysis.
// Runs asynchronously — failures are logged but do not affect the analysis.
func (s *Server) generateEmbeddings(ctx context.Context, analysis *database.Analysis, orgID uuid.UUID) {
	for i, rca := range analysis.RCAs {
		text := eepatterns.ComputeEmbeddingText(&rca)
		embedding, err := s.embeddingClient.Embed(ctx, text)
		if err != nil {
			log.Printf("embedding failed for analysis %s rca %d: %v", analysis.ID, i, err)
			continue
		}
		_, err = s.db.CreateRCAEmbedding(ctx, database.CreateEmbeddingParams{
			AnalysisID:    analysis.ID,
			RCAIndex:      i,
			OrgID:         orgID,
			FailureType:   &rca.FailureType,
			EmbeddingText: text,
			Embedding:     pgvector.NewVector(embedding),
		})
		if err != nil {
			log.Printf("failed to store embedding for analysis %s rca %d: %v", analysis.ID, i, err)
		}
	}
}

// handleSimilarAnalyses finds analyses with RCAs similar to the given analysis.
func (s *Server) handleSimilarAnalyses(w http.ResponseWriter, r *http.Request) {
	oc, ok := s.requireOrgMember(w, r)
	if !ok {
		return
	}

	if s.embeddingClient == nil {
		writeError(w, http.StatusServiceUnavailable, "embeddings not configured")
		return
	}

	analysisID, err := parseAnalysisID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid analysis ID")
		return
	}

	// Verify analysis belongs to this org
	analysis, err := s.db.GetAnalysisByID(r.Context(), analysisID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errDatabase)
		return
	}
	if analysis == nil {
		writeError(w, http.StatusNotFound, "analysis not found")
		return
	}
	repo, err := s.db.GetRepositoryByID(r.Context(), analysis.RepoID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errDatabase)
		return
	}
	if repo == nil || repo.OrgID != oc.OrgID {
		writeError(w, http.StatusNotFound, "analysis not found")
		return
	}

	limit, threshold := parseSimilarParams(r)

	// Get embeddings for this analysis
	embeddings, err := s.db.GetRCAEmbeddingsByAnalysis(r.Context(), analysisID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errDatabase)
		return
	}
	if len(embeddings) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"similar": []any{}})
		return
	}

	// Find similar RCAs across the org for each embedding
	seen := make(map[uuid.UUID]struct{})
	var allSimilar []database.SimilarRCA
	for _, emb := range embeddings {
		similar, err := s.db.FindSimilarRCAs(r.Context(), oc.OrgID, emb.Embedding, analysisID, limit, threshold)
		if err != nil {
			writeError(w, http.StatusInternalServerError, errDatabase)
			return
		}
		for _, s := range similar {
			if _, ok := seen[s.ID]; !ok {
				seen[s.ID] = struct{}{}
				allSimilar = append(allSimilar, s)
			}
		}
	}

	// Cap results
	if len(allSimilar) > limit {
		allSimilar = allSimilar[:limit]
	}

	writeJSON(w, http.StatusOK, map[string]any{"similar": allSimilar})
}

// handlePatternSearch searches historical patterns by free-text query.
func (s *Server) handlePatternSearch(w http.ResponseWriter, r *http.Request) {
	oc, ok := s.requireOrgMember(w, r)
	if !ok {
		return
	}

	if s.embeddingClient == nil {
		writeError(w, http.StatusServiceUnavailable, "embeddings not configured")
		return
	}

	query := r.URL.Query().Get("q")
	if query == "" {
		writeError(w, http.StatusBadRequest, "q parameter required")
		return
	}

	limit := 10
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 50 {
			limit = parsed
		}
	}

	embedding, err := s.embeddingClient.Embed(r.Context(), query)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate embedding")
		return
	}

	results, err := s.db.FindSimilarRCAsByText(r.Context(), oc.OrgID, pgvector.NewVector(embedding), limit, 0.5)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errDatabase)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// parseSimilarParams extracts limit and threshold from query parameters.
func parseSimilarParams(r *http.Request) (limit int, threshold float64) {
	limit = 5
	threshold = 0.7

	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 50 {
			limit = parsed
		}
	}
	if t := r.URL.Query().Get("threshold"); t != "" {
		if parsed, err := strconv.ParseFloat(t, 64); err == nil && parsed >= 0 && parsed <= 1 {
			threshold = parsed
		}
	}
	return limit, threshold
}
