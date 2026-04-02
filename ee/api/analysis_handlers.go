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

const (
	errDatabase         = "database error"
	errAnalysisNotFound = "analysis not found"
)

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

	analysis, ok := s.requireOrgAnalysis(w, r, oc)
	if !ok {
		return
	}

	repo, err := s.db.GetRepositoryByID(r.Context(), analysis.RepoID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errDatabase)
		return
	}
	if repo == nil {
		writeError(w, http.StatusNotFound, errAnalysisNotFound)
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

	var req createAnalysisRequest
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

	analysis, err := s.db.CreateAnalysis(r.Context(), buildAnalysisParams(repo.ID, req))
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
		s.bgTasks.Add(1)
		go func() {
			defer s.bgTasks.Done()
			s.generateEmbeddings(context.Background(), analysis, oc.OrgID)
		}()
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

// requireOrgAnalysis validates the analysis exists and belongs to the request's org.
func (s *Server) requireOrgAnalysis(w http.ResponseWriter, r *http.Request, oc *orgContext) (*database.Analysis, bool) {
	analysisID, err := parseAnalysisID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid analysis ID")
		return nil, false
	}

	analysis, err := s.db.GetAnalysisByID(r.Context(), analysisID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errDatabase)
		return nil, false
	}
	if analysis == nil {
		writeError(w, http.StatusNotFound, errAnalysisNotFound)
		return nil, false
	}

	repo, err := s.db.GetRepositoryByID(r.Context(), analysis.RepoID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errDatabase)
		return nil, false
	}
	if repo == nil || repo.OrgID != oc.OrgID {
		writeError(w, http.StatusNotFound, errAnalysisNotFound)
		return nil, false
	}

	return analysis, true
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

	analysis, ok := s.requireOrgAnalysis(w, r, oc)
	if !ok {
		return
	}

	limit, threshold := parseSimilarParams(r)

	embeddings, err := s.db.GetRCAEmbeddingsByAnalysis(r.Context(), analysis.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errDatabase)
		return
	}
	if len(embeddings) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"similar": []any{}})
		return
	}

	allSimilar := s.collectSimilar(r.Context(), oc.OrgID, analysis.ID, embeddings, limit, threshold)

	writeJSON(w, http.StatusOK, map[string]any{"similar": allSimilar})
}

// collectSimilar gathers deduplicated similar RCAs across multiple embeddings.
func (s *Server) collectSimilar(ctx context.Context, orgID, excludeID uuid.UUID,
	embeddings []database.RCAEmbedding, limit int, threshold float64) []database.SimilarRCA {

	seen := make(map[uuid.UUID]struct{})
	all := make([]database.SimilarRCA, 0)
	for _, emb := range embeddings {
		similar, err := s.db.FindSimilarRCAs(ctx, orgID, emb.Embedding, excludeID, limit, threshold)
		if err != nil {
			log.Printf("FindSimilarRCAs failed for embedding %s: %v", emb.ID, err)
			continue
		}
		for _, sr := range similar {
			if _, exists := seen[sr.ID]; !exists {
				seen[sr.ID] = struct{}{}
				all = append(all, sr)
			}
		}
	}
	if len(all) > limit {
		all = all[:limit]
	}
	return all
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

	results, err := s.db.FindSimilarRCAs(r.Context(), oc.OrgID, pgvector.NewVector(embedding), uuid.Nil, limit, 0.5)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errDatabase)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// createAnalysisRequest holds the JSON body for analysis creation.
type createAnalysisRequest struct {
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

func buildAnalysisParams(repoID uuid.UUID, req createAnalysisRequest) database.CreateAnalysisParams {
	params := database.CreateAnalysisParams{
		RepoID:   repoID,
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
	return params
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
