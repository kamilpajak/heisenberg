package analysis

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/kamilpajak/heisenberg/pkg/cluster"
	gh "github.com/kamilpajak/heisenberg/pkg/github"
	"github.com/kamilpajak/heisenberg/pkg/llm"
)

// Params configures an analysis run.
type Params struct {
	Owner        string
	Repo         string
	RunID        int64
	Verbose      bool
	Emitter      llm.ProgressEmitter
	SnapshotHTML func([]byte) ([]byte, error)
	Model        string // Gemini model override (empty = default)
}

// Run executes the full analysis pipeline: resolve run ID, fetch metadata, and
// run the LLM agent loop. It returns the analysis result or an error.
func Run(ctx context.Context, p Params) (*llm.AnalysisResult, error) {
	ghClient := gh.NewClient()

	// Resolve run ID
	if p.RunID == 0 {
		emitInfo(p.Emitter, fmt.Sprintf("Finding run to analyze for %s/%s...", p.Owner, p.Repo))
		runs, err := ghClient.ListWorkflowRuns(ctx, p.Owner, p.Repo)
		if err != nil {
			return nil, fmt.Errorf("failed to list runs: %w", err)
		}

		var shouldSkip bool
		p.RunID, shouldSkip = findRunToAnalyze(runs)

		if shouldSkip {
			return &llm.AnalysisResult{
				Text:     "Latest workflow run passed. No failures to analyze.",
				Category: llm.CategoryNoFailures,
			}, nil
		}

		if p.RunID == 0 {
			return nil, fmt.Errorf("no failed workflow runs found for %s/%s", p.Owner, p.Repo)
		}
	}

	emitInfo(p.Emitter, fmt.Sprintf("Analyzing run %d for %s/%s...", p.RunID, p.Owner, p.Repo))

	// Fetch run metadata, jobs, and artifacts
	wfRun, err := ghClient.GetWorkflowRun(ctx, p.Owner, p.Repo, p.RunID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch run: %w", err)
	}

	jobs, err := ghClient.ListJobs(ctx, p.Owner, p.Repo, p.RunID)
	if err != nil {
		return nil, fmt.Errorf("failed to list jobs: %w", err)
	}

	artifacts, err := ghClient.ListArtifacts(ctx, p.Owner, p.Repo, p.RunID)
	if err != nil {
		return nil, fmt.Errorf("failed to list artifacts: %w", err)
	}

	// Check artifact availability early
	artifactStatus := gh.CheckArtifacts(artifacts)
	if artifactStatus.AllExpired {
		runDate := formatRunDate(wfRun.CreatedAt)
		return nil, fmt.Errorf("all artifacts have expired for run %d (created %s)\n\nArtifacts expire after 90 days. Try analyzing a more recent run, or omit --run-id to use the latest failed run", p.RunID, runDate)
	}

	// Clustering gate: if many failed jobs, use per-cluster analysis
	const clusterThreshold = 6
	failedJobs := filterFailed(jobs)
	if len(failedJobs) > clusterThreshold {
		result, err := runClustered(ctx, p, ghClient, wfRun, jobs, failedJobs, artifacts)
		stampRunMeta(result, p, wfRun)
		return result, err
	}

	// Standard single-loop path (unchanged for <= threshold failures)
	result, err := runSingle(ctx, p, ghClient, wfRun, jobs, artifacts)
	stampRunMeta(result, p, wfRun)
	return result, err
}

// runSingle is the original single-agent-loop analysis path.
func runSingle(ctx context.Context, p Params, ghClient *gh.Client,
	wfRun *gh.WorkflowRun, jobs []gh.Job, artifacts []gh.Artifact) (*llm.AnalysisResult, error) {

	initialContext := buildInitialContext(wfRun, jobs, artifacts)

	var prNumber int
	if len(wfRun.PullRequests) > 0 {
		prNumber = wfRun.PullRequests[0].Number
	}

	handler := &ToolHandler{
		GitHub:       ghClient,
		Owner:        p.Owner,
		Repo:         p.Repo,
		RunID:        p.RunID,
		PRNumber:     prNumber,
		HeadSHA:      wfRun.HeadSHA,
		SnapshotHTML: p.SnapshotHTML,
		Emitter:      p.Emitter,
		artifacts:    artifacts,
	}

	llmClient, err := llm.NewClient(p.Model)
	if err != nil {
		return nil, err
	}

	return llmClient.RunAgentLoop(ctx, handler, ToolDeclarations(), initialContext, p.Verbose)
}

// runClustered pre-clusters failures by error signature, then runs one LLM
// agent loop per cluster with focused context.
func runClustered(ctx context.Context, p Params, ghClient *gh.Client,
	wfRun *gh.WorkflowRun, allJobs []gh.Job, failedJobs []gh.Job, artifacts []gh.Artifact) (*llm.AnalysisResult, error) {

	emitInfo(p.Emitter, fmt.Sprintf("Clustering %d failed jobs...", len(failedJobs)))

	// Fetch logs for all failed jobs in parallel
	failures := fetchFailureLogs(ctx, ghClient, p, failedJobs)

	// Cluster by error signature
	cr := cluster.ClusterFailures(failures)

	// If clustering produced 1 cluster or failed, fall back to single path
	if len(cr.Clusters) <= 1 {
		return runSingle(ctx, p, ghClient, wfRun, allJobs, artifacts)
	}

	emitInfo(p.Emitter, fmt.Sprintf("Found %d failure clusters (%s)", len(cr.Clusters), cr.Method))

	// Run LLM agent loop per cluster
	var results []clusterAnalysis
	llmClient, err := llm.NewClient(p.Model)
	if err != nil {
		return nil, err
	}

	var prNumber int
	if len(wfRun.PullRequests) > 0 {
		prNumber = wfRun.PullRequests[0].Number
	}

	for i, c := range cr.Clusters {
		emitInfo(p.Emitter, fmt.Sprintf("[Cluster %d/%d] Analyzing %d jobs (%s)...",
			i+1, len(cr.Clusters), len(c.Failures), truncate(c.Signature.RawExcerpt, 60)))

		clusterCtx := buildClusterContext(wfRun, c, i+1, len(cr.Clusters), allJobs, artifacts)

		handler := &ToolHandler{
			GitHub:       ghClient,
			Owner:        p.Owner,
			Repo:         p.Repo,
			RunID:        p.RunID,
			PRNumber:     prNumber,
			HeadSHA:      wfRun.HeadSHA,
			SnapshotHTML: p.SnapshotHTML,
			Emitter:      p.Emitter,
			artifacts:    artifacts,
		}

		result, err := llmClient.RunAgentLoop(ctx, handler, ToolDeclarations(), clusterCtx, p.Verbose)
		if err != nil {
			return nil, fmt.Errorf("cluster %d: %w", i+1, err)
		}

		results = append(results, clusterAnalysis{Cluster: c, Result: result})
	}

	// Handle unclustered failures — create a synthetic cluster and analyze
	if len(cr.Unclustered) > 0 {
		emitInfo(p.Emitter, fmt.Sprintf("[Other] Analyzing %d unclustered jobs...", len(cr.Unclustered)))

		uc := buildUnclusteredCluster(cr.Unclustered, len(cr.Clusters)+1)
		clusterCtx := buildClusterContext(wfRun, uc, uc.ID, len(cr.Clusters)+1, allJobs, artifacts)
		ucHandler := &ToolHandler{
			GitHub: ghClient, Owner: p.Owner, Repo: p.Repo,
			RunID: p.RunID, PRNumber: prNumber, HeadSHA: wfRun.HeadSHA,
			SnapshotHTML: p.SnapshotHTML, Emitter: p.Emitter, artifacts: artifacts,
		}
		ucResult, ucErr := llmClient.RunAgentLoop(ctx, ucHandler, ToolDeclarations(), clusterCtx, p.Verbose)
		if ucErr == nil && ucResult != nil {
			results = append(results, clusterAnalysis{Cluster: uc, Result: ucResult})
		}
	}

	merged := mergeClusterResults(results)
	if merged.Eval == nil {
		merged.Eval = &llm.EvalMeta{}
	}
	merged.Eval.Clustered = true
	merged.Eval.ClusterCount = len(cr.Clusters)
	merged.Eval.ClusterMethod = cr.Method

	return merged, nil
}

// fetchFailureLogs fetches logs for failed jobs in parallel with a concurrency limit.
func fetchFailureLogs(ctx context.Context, ghClient *gh.Client, p Params, failedJobs []gh.Job) []cluster.FailureInfo {
	const maxConcurrent = 5
	sem := make(chan struct{}, maxConcurrent)

	failures := make([]cluster.FailureInfo, len(failedJobs))

	var wg sync.WaitGroup
	for i, job := range failedJobs {
		wg.Add(1)
		go func(idx int, j gh.Job) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			logText, err := ghClient.GetJobLogs(ctx, p.Owner, p.Repo, j.ID)
			if err != nil {
				logText = fmt.Sprintf("(failed to fetch logs: %s)", err)
			}

			sig := cluster.ExtractSignature(logText)

			// Keep last 10KB for LLM context
			logTail := logText
			if len(logTail) > 10000 {
				logTail = logTail[len(logTail)-10000:]
			}

			// Each goroutine writes to its own pre-allocated index — no mutex needed
			failures[idx] = cluster.FailureInfo{
				JobID:      j.ID,
				JobName:    j.Name,
				Conclusion: j.Conclusion,
				Signature:  sig,
				LogTail:    logTail,
			}
		}(i, job)
	}
	wg.Wait()
	return failures
}

// filterFailed returns jobs with conclusion "failure".
func filterFailed(jobs []gh.Job) []gh.Job {
	var failed []gh.Job
	for _, j := range jobs {
		if j.Conclusion == "failure" {
			failed = append(failed, j)
		}
	}
	return failed
}

// buildClusterContext creates a focused initial context for one cluster.
func buildClusterContext(run *gh.WorkflowRun, c cluster.Cluster,
	clusterNum, totalClusters int, allJobs []gh.Job, artifacts []gh.Artifact) string {

	var b strings.Builder

	b.WriteString("## Workflow Run\n")
	fmt.Fprintf(&b, "- Run ID: %d\n", run.ID)
	fmt.Fprintf(&b, "- Name: %s\n", run.Name)
	fmt.Fprintf(&b, "- Branch: %s\n", run.HeadBranch)
	fmt.Fprintf(&b, "- Commit: %s\n", run.HeadSHA)
	fmt.Fprintf(&b, "- Conclusion: %s\n", run.Conclusion)

	fmt.Fprintf(&b, "\n## Cluster %d of %d — %d jobs\n", clusterNum, totalClusters, len(c.Failures))
	fmt.Fprintf(&b, "Error pattern: %s\n", c.Signature.RawExcerpt)
	b.WriteString("\nYou are analyzing a specific cluster of related failures. ")
	b.WriteString("Do not assume these are the only failures in this run. ")
	b.WriteString("Focus on the root cause shared by this cluster.\n")

	b.WriteString("\n### Affected Jobs\n")
	for _, f := range c.Failures {
		fmt.Fprintf(&b, "- %s (id=%d, %s)\n", f.JobName, f.JobID, f.Conclusion)
	}

	b.WriteString("\n### Representative Error\n")
	fmt.Fprintf(&b, "From: %s\n```\n%s\n```\n", c.Representative.JobName, c.Representative.LogTail)

	b.WriteString("\n### Artifacts\n")
	hasTestArtifacts := false
	for _, a := range artifacts {
		if a.Expired {
			continue
		}
		label := ""
		if isTestArtifact(a.Name) {
			label = " [TEST REPORT]"
			hasTestArtifacts = true
		}
		fmt.Fprintf(&b, "- %s (%d bytes)%s\n", a.Name, a.SizeBytes, label)
	}

	b.WriteString("\n### Instructions\n")
	b.WriteString("Analyze this cluster of related failures to determine their shared root cause.\n")
	if hasTestArtifacts {
		b.WriteString("Test report artifacts are available. Fetch them first.\n")
	}
	b.WriteString("When done, call the 'done' tool with your analysis.\n")

	return b.String()
}

// buildUnclusteredCluster creates a synthetic cluster from failures that
// couldn't be fingerprinted, picking the longest log as representative.
func buildUnclusteredCluster(failures []cluster.FailureInfo, id int) cluster.Cluster {
	c := cluster.Cluster{
		ID:             id,
		Failures:       failures,
		Representative: failures[0],
	}
	for _, f := range failures[1:] {
		if len(f.LogTail) > len(c.Representative.LogTail) {
			c.Representative = f
		}
	}
	return c
}

// truncate returns s truncated to maxLen with "..." suffix.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func stampRunMeta(result *llm.AnalysisResult, p Params, wfRun *gh.WorkflowRun) {
	if result == nil {
		return
	}
	result.RunID = p.RunID
	result.Owner = p.Owner
	result.Repo = p.Repo
	result.Branch = wfRun.HeadBranch
	result.CommitSHA = wfRun.HeadSHA
	result.Event = wfRun.Event
}

func emitInfo(e llm.ProgressEmitter, msg string) {
	if e != nil {
		e.Emit(llm.ProgressEvent{Type: "info", Message: msg})
	}
}

func buildInitialContext(run *gh.WorkflowRun, jobs []gh.Job, artifacts []gh.Artifact) string {
	var b strings.Builder

	b.WriteString("## Workflow Run\n")
	fmt.Fprintf(&b, "- Run ID: %d\n", run.ID)
	fmt.Fprintf(&b, "- Name: %s\n", run.Name)
	fmt.Fprintf(&b, "- Title: %s\n", run.DisplayTitle)
	fmt.Fprintf(&b, "- Branch: %s\n", run.HeadBranch)
	fmt.Fprintf(&b, "- Commit: %s\n", run.HeadSHA)
	fmt.Fprintf(&b, "- Event: %s\n", run.Event)
	fmt.Fprintf(&b, "- Conclusion: %s\n", run.Conclusion)
	if run.Path != "" {
		fmt.Fprintf(&b, "- Workflow file: %s\n", run.Path)
	}

	b.WriteString("\n## Jobs\n")
	for _, j := range jobs {
		fmt.Fprintf(&b, "- %s (id=%d, status=%s, conclusion=%s)\n", j.Name, j.ID, j.Status, j.Conclusion)
	}

	b.WriteString("\n## Artifacts\n")
	if len(artifacts) == 0 {
		b.WriteString("No artifacts found.\n")
	}
	hasTestArtifacts := false
	for _, a := range artifacts {
		expired := ""
		if a.Expired {
			expired = " [EXPIRED]"
		}
		label := ""
		if !a.Expired && isTestArtifact(a.Name) {
			label = " [TEST REPORT]"
			hasTestArtifacts = true
		}
		fmt.Fprintf(&b, "- %s (%d bytes)%s%s\n", a.Name, a.SizeBytes, label, expired)
	}

	b.WriteString("\n## Instructions\n")
	b.WriteString("Analyze this workflow run to determine why it failed.\n")
	if hasTestArtifacts {
		b.WriteString("IMPORTANT: Test report artifacts are available. Start by fetching them using get_artifact to understand which tests failed and why. Only read source files after analyzing test reports.\n")
	}
	b.WriteString("When you have enough information, you MUST call the 'done' tool first, then provide your final root cause analysis.\n")

	return b.String()
}

// isTestArtifact returns true if the artifact name suggests it contains test results.
func isTestArtifact(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "report") || strings.Contains(lower, "test-results") ||
		strings.Contains(lower, "blob")
}

// findRunToAnalyze determines which run to analyze based on smart selection logic.
// Returns (runID, shouldSkip). If shouldSkip is true, the latest run passed and
// there's nothing to analyze. If runID is 0 and shouldSkip is false, no failed runs exist.
func findRunToAnalyze(runs []gh.WorkflowRun) (runID int64, shouldSkip bool) {
	if len(runs) == 0 {
		return 0, false
	}

	// Check if the latest completed run is a success
	latest := runs[0]
	if latest.Conclusion == "success" {
		return 0, true
	}

	// Find the first failure
	for _, r := range runs {
		if r.Conclusion == "failure" {
			return r.ID, false
		}
	}

	return 0, false
}

// formatRunDate parses a GitHub timestamp and returns a human-readable date.
func formatRunDate(timestamp string) string {
	if timestamp == "" {
		return "unknown date"
	}
	t, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return timestamp
	}
	return t.Format("2006-01-02")
}
