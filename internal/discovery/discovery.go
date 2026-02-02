package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kamilpajak/heisenberg/internal/github"
)

// ReportType represents the type of test report
type ReportType string

const (
	ReportTypePlaywright ReportType = "playwright"
	ReportTypeJest       ReportType = "jest"
	ReportTypeJUnit      ReportType = "junit"
	ReportTypeUnknown    ReportType = "unknown"
)

// ArtifactDiscoveryResult represents a discovered test artifact
type ArtifactDiscoveryResult struct {
	ArtifactID   int64
	ArtifactName string
	ReportType   ReportType
	FileName     string
	WorkflowRun  github.WorkflowRun
	HasFailures  bool
	TotalTests   int
	FailedTests  int
}

// RepoDiscoveryResult represents discovery results for a repository
type RepoDiscoveryResult struct {
	Repository string
	Compatible bool
	Artifacts  []ArtifactDiscoveryResult
	Error      string
}

// Service handles test artifact discovery
type Service struct {
	client *github.Client
}

// New creates a new discovery service
func New(client *github.Client) *Service {
	return &Service{client: client}
}

// ReportPatterns are file patterns that indicate test reports
var ReportPatterns = []string{
	"*.json",
	"report.json",
	"results.json",
	"test-results.json",
	"playwright-report.json",
}

// ArtifactNamePatterns are artifact names that likely contain test reports
var ArtifactNamePatterns = []string{
	"playwright",
	"test",
	"report",
	"e2e",
	"results",
	"blob",
}

// DiscoverRepo checks a specific repository for test artifacts
func (s *Service) DiscoverRepo(ctx context.Context, owner, repo string) (*RepoDiscoveryResult, error) {
	result := &RepoDiscoveryResult{
		Repository: fmt.Sprintf("%s/%s", owner, repo),
		Artifacts:  []ArtifactDiscoveryResult{},
	}

	// Get recent workflow runs
	runs, err := s.client.ListWorkflowRuns(ctx, owner, repo)
	if err != nil {
		result.Error = err.Error()
		return result, nil
	}

	if len(runs) == 0 {
		result.Error = "no workflow runs found"
		return result, nil
	}

	// Check artifacts for each run (limit to most recent few)
	maxRuns := 5
	if len(runs) < maxRuns {
		maxRuns = len(runs)
	}

	for _, run := range runs[:maxRuns] {
		artifacts, err := s.client.ListArtifacts(ctx, owner, repo, run.ID)
		if err != nil {
			continue
		}

		for _, artifact := range artifacts {
			if artifact.Expired {
				continue
			}

			// Check if artifact name suggests test reports
			if !matchesArtifactPattern(artifact.Name) {
				continue
			}

			// Try to extract and analyze the artifact (requires auth)
			discovery := s.analyzeArtifact(ctx, owner, repo, artifact, run)
			if discovery != nil {
				result.Artifacts = append(result.Artifacts, *discovery)
				result.Compatible = true
			} else {
				// If we can't download (no auth), still mark as compatible based on name
				reportType := guessReportTypeFromName(artifact.Name)
				if reportType != ReportTypeUnknown {
					result.Artifacts = append(result.Artifacts, ArtifactDiscoveryResult{
						ArtifactID:   artifact.ID,
						ArtifactName: artifact.Name,
						ReportType:   reportType,
						WorkflowRun:  run,
						HasFailures:  run.Conclusion == "failure",
					})
					result.Compatible = true
				}
			}
		}

		// If we found artifacts, no need to check more runs
		if len(result.Artifacts) > 0 {
			break
		}
	}

	if len(result.Artifacts) == 0 {
		result.Error = "no compatible test artifacts found"
	}

	return result, nil
}

// guessReportTypeFromName attempts to determine report type from artifact name
func guessReportTypeFromName(name string) ReportType {
	nameLower := strings.ToLower(name)
	if strings.Contains(nameLower, "playwright") || strings.Contains(nameLower, "blob-report") {
		return ReportTypePlaywright
	}
	if strings.Contains(nameLower, "jest") {
		return ReportTypeJest
	}
	if strings.Contains(nameLower, "junit") {
		return ReportTypeJUnit
	}
	// Generic test artifacts
	if strings.Contains(nameLower, "test-results") || strings.Contains(nameLower, "e2e") {
		return ReportTypePlaywright // Assume Playwright for e2e
	}
	return ReportTypeUnknown
}

// analyzeArtifact downloads and analyzes an artifact to detect report type
func (s *Service) analyzeArtifact(ctx context.Context, owner, repo string, artifact github.Artifact, run github.WorkflowRun) *ArtifactDiscoveryResult {
	files, err := s.client.ExtractArtifact(ctx, owner, repo, artifact.ID, ReportPatterns)
	if err != nil {
		return nil
	}

	for _, file := range files {
		reportType, hasFailures, total, failed := detectReportType(file.Content)
		if reportType != ReportTypeUnknown {
			return &ArtifactDiscoveryResult{
				ArtifactID:   artifact.ID,
				ArtifactName: artifact.Name,
				ReportType:   reportType,
				FileName:     file.Name,
				WorkflowRun:  run,
				HasFailures:  hasFailures,
				TotalTests:   total,
				FailedTests:  failed,
			}
		}
	}

	return nil
}

// matchesArtifactPattern checks if artifact name matches known patterns
func matchesArtifactPattern(name string) bool {
	nameLower := strings.ToLower(name)
	for _, pattern := range ArtifactNamePatterns {
		if strings.Contains(nameLower, pattern) {
			return true
		}
	}
	return false
}

// detectReportType analyzes JSON content to determine report type
func detectReportType(content []byte) (ReportType, bool, int, int) {
	var generic map[string]interface{}
	if err := json.Unmarshal(content, &generic); err != nil {
		return ReportTypeUnknown, false, 0, 0
	}

	// Check for Playwright report structure
	if _, hasSuites := generic["suites"]; hasSuites {
		if _, hasConfig := generic["config"]; hasConfig {
			hasFailures, total, failed := analyzePlaywrightReport(generic)
			return ReportTypePlaywright, hasFailures, total, failed
		}
		// Might be blob format
		if stats, hasStats := generic["stats"].(map[string]interface{}); hasStats {
			hasFailures, total, failed := analyzePlaywrightStats(stats)
			return ReportTypePlaywright, hasFailures, total, failed
		}
	}

	// Check for Jest report structure
	if _, hasTestResults := generic["testResults"]; hasTestResults {
		return ReportTypeJest, false, 0, 0
	}

	// Check for JUnit structure (typically has testsuite/testsuites)
	if _, hasTestsuite := generic["testsuite"]; hasTestsuite {
		return ReportTypeJUnit, false, 0, 0
	}
	if _, hasTestsuites := generic["testsuites"]; hasTestsuites {
		return ReportTypeJUnit, false, 0, 0
	}

	return ReportTypeUnknown, false, 0, 0
}

// analyzePlaywrightReport extracts failure info from Playwright report
func analyzePlaywrightReport(report map[string]interface{}) (bool, int, int) {
	stats, ok := report["stats"].(map[string]interface{})
	if !ok {
		return false, 0, 0
	}

	return analyzePlaywrightStats(stats)
}

// analyzePlaywrightStats extracts failure info from Playwright stats
func analyzePlaywrightStats(stats map[string]interface{}) (bool, int, int) {
	var total, failed int

	// Try different stat formats
	if expected, ok := stats["expected"].(float64); ok {
		total += int(expected)
	}
	if unexpected, ok := stats["unexpected"].(float64); ok {
		failed = int(unexpected)
		total += failed
	}
	if flaky, ok := stats["flaky"].(float64); ok {
		total += int(flaky)
	}
	if skipped, ok := stats["skipped"].(float64); ok {
		total += int(skipped)
	}

	// Blob format uses passed/failed
	if passed, ok := stats["passed"].(float64); ok {
		total = int(passed)
	}
	if failedCount, ok := stats["failed"].(float64); ok {
		failed = int(failedCount)
		total += failed
	}
	if totalCount, ok := stats["total"].(float64); ok {
		total = int(totalCount)
	}

	return failed > 0, total, failed
}

// SearchAndDiscover searches for repos and checks them for test artifacts
func (s *Service) SearchAndDiscover(ctx context.Context, query string, minStars, limit int) ([]RepoDiscoveryResult, error) {
	repos, err := s.client.SearchRepositories(ctx, query, minStars, limit)
	if err != nil {
		return nil, err
	}

	var results []RepoDiscoveryResult
	for _, repo := range repos {
		parts := strings.Split(repo.FullName, "/")
		if len(parts) != 2 {
			continue
		}

		result, err := s.DiscoverRepo(ctx, parts[0], parts[1])
		if err != nil {
			result = &RepoDiscoveryResult{
				Repository: repo.FullName,
				Error:      err.Error(),
			}
		}
		results = append(results, *result)
	}

	return results, nil
}
