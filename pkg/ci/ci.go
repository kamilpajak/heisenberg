// Package ci defines the provider interface for CI/CD platform integration.
// Each CI platform (GitHub Actions, Azure Pipelines, etc.) implements the
// Provider interface to supply run metadata, logs, artifacts, and diffs.
package ci

import "context"

// Normalized run/job status values. CI providers must map platform-specific
// statuses to these constants (e.g., Azure "inProgress" → StatusInProgress).
const (
	StatusCompleted  = "completed"
	StatusInProgress = "in_progress"
	StatusQueued     = "queued"
)

// Normalized conclusion values.
const (
	ConclusionSuccess = "success"
	ConclusionFailure = "failure"
)

// Provider abstracts CI platform operations needed for test failure analysis.
// Implementations bind platform-specific identifiers (e.g., owner/repo for GitHub,
// org/project for Azure DevOps) at construction time.
type Provider interface {
	// Name returns the provider identifier (e.g., "github", "azure").
	Name() string

	// AnalysisHints returns provider-specific strategy hints for the LLM
	// system prompt. These guide the model toward the most effective
	// data-gathering approach for each platform.
	AnalysisHints() string

	// ListRuns returns recent completed CI runs matching the filter.
	ListRuns(ctx context.Context, filter RunFilter) ([]Run, error)

	// GetRun fetches a single CI run by ID.
	GetRun(ctx context.Context, runID int64) (*Run, error)

	// ListJobs returns jobs (or equivalent) within a CI run.
	ListJobs(ctx context.Context, runID int64) ([]Job, error)

	// ListArtifacts returns artifacts attached to a CI run.
	ListArtifacts(ctx context.Context, runID int64) ([]Artifact, error)

	// GetJobLogs fetches the plain-text log output for a job.
	GetJobLogs(ctx context.Context, jobID int64) (string, error)

	// DownloadArtifact downloads an artifact as raw bytes (typically a ZIP).
	DownloadArtifact(ctx context.Context, artifactID int64) ([]byte, error)

	// DownloadAndExtract downloads an artifact ZIP and extracts its first file.
	DownloadAndExtract(ctx context.Context, artifactID int64) ([]byte, error)

	// GetRepoFile fetches a file from the repository. When ref is non-empty,
	// the file is fetched at that specific commit SHA; otherwise the default branch is used.
	GetRepoFile(ctx context.Context, path, ref string) (string, error)

	// ListDirectory returns file/directory names at the given path.
	// Directory names have a trailing "/".
	ListDirectory(ctx context.Context, path string) ([]string, error)

	// GetChangedFiles returns files changed in a PR or commit range.
	GetChangedFiles(ctx context.Context, ref ChangeRef) ([]ChangedFile, error)
}

// TestResultsProvider is an optional capability for CI platforms that expose
// structured test results via API (e.g., Azure DevOps). Implementations that
// support this interface enable the get_test_results tool.
type TestResultsProvider interface {
	GetTestRuns(ctx context.Context, buildID int64) ([]TestRun, error)
	GetTestResults(ctx context.Context, testRunID int64) ([]TestResult, error)
}

// RunFilter configures which CI runs to list.
type RunFilter struct {
	Status  string // e.g., "completed"
	PerPage int
}

// Run represents a CI pipeline run (GitHub workflow run, Azure build, etc.).
type Run struct {
	ID           int64
	Name         string
	Status       string // "completed", "in_progress", "queued", etc.
	Conclusion   string // "success", "failure", etc.
	Branch       string
	CommitSHA    string
	Event        string // "push", "pull_request", "manual", etc.
	Path         string // workflow/pipeline definition file path
	DisplayTitle string
	CreatedAt    string
	PRNumbers    []int // associated pull request numbers
}

// IsCompleted returns true if the run has finished (not in progress or queued).
func (r *Run) IsCompleted() bool {
	return r.Status == StatusCompleted
}

// Job represents a unit of work within a CI run.
type Job struct {
	ID         int64
	Name       string
	Status     string // "completed", "in_progress", etc.
	Conclusion string // "success", "failure", etc.
}

// Artifact represents a file artifact attached to a CI run.
type Artifact struct {
	ID        int64
	Name      string
	SizeBytes int64
	Expired   bool
}

// ArtifactStatus describes the availability of artifacts for a run.
type ArtifactStatus struct {
	Total      int
	Expired    int
	Available  int
	HasUsable  bool
	AllExpired bool
}

// CheckArtifacts analyzes artifact availability.
func CheckArtifacts(artifacts []Artifact) ArtifactStatus {
	status := ArtifactStatus{Total: len(artifacts)}
	for _, a := range artifacts {
		if a.Expired {
			status.Expired++
		} else {
			status.Available++
		}
	}
	status.HasUsable = status.Available > 0
	status.AllExpired = status.Total > 0 && status.Expired == status.Total
	return status
}

// ArtifactType represents the format of a test artifact.
type ArtifactType string

const (
	ArtifactHTML ArtifactType = "html"
	ArtifactJSON ArtifactType = "json"
	ArtifactBlob ArtifactType = "blob"
)

// ChangedFile represents a file modified in a PR or commit range.
type ChangedFile struct {
	Path      string
	Status    string
	Additions int
	Deletions int
	Patch     string
}

// ChangeRef identifies the source of changed files.
type ChangeRef struct {
	PRNumber   int
	HeadSHA    string
	BaseBranch string
}

// TestRun represents a test run within a CI build (Azure DevOps concept).
type TestRun struct {
	ID          int64
	Name        string
	TotalTests  int
	PassedTests int
	FailedTests int
}

// CrossRepoProvider is an optional capability for CI platforms that can
// discover and access files from additional repositories used by a build
// pipeline (e.g., test repos checked out via resources.repositories).
type CrossRepoProvider interface {
	// DiscoverRepos returns additional repository references used by the build pipeline.
	DiscoverRepos(ctx context.Context, buildID int64) ([]RepoRef, error)
	// GetFileFromRepo fetches a file from a specific repository.
	// When ref is non-empty, the file is fetched at that specific commit SHA.
	GetFileFromRepo(ctx context.Context, repo RepoRef, path, ref string) (string, error)
}

// RepoRef identifies a repository within a CI platform.
type RepoRef struct {
	Project string // Azure: project name; GitHub: owner
	Repo    string // repository name or ID
}

// TestResult represents a single test result.
type TestResult struct {
	ID           int64
	TestName     string
	Outcome      string // "Passed", "Failed", etc.
	ErrorMessage string
	StackTrace   string
	DurationMs   float64
}
