package analysis

import (
	"context"
	"fmt"
	"strings"

	gh "github.com/kamilpajak/heisenberg/internal/github"
	"github.com/kamilpajak/heisenberg/internal/llm"
)

// Params configures an analysis run.
type Params struct {
	Owner        string
	Repo         string
	RunID        int64
	Verbose      bool
	Emitter      llm.ProgressEmitter
	SnapshotHTML func([]byte) ([]byte, error)
}

// Run executes the full analysis pipeline: resolve run ID, fetch metadata, and
// run the LLM agent loop. It returns the analysis result or an error.
func Run(ctx context.Context, p Params) (*llm.AnalysisResult, error) {
	ghClient := gh.NewClient()

	// Resolve run ID
	resolvedRunID := p.RunID
	if resolvedRunID == 0 {
		emitInfo(p.Emitter, fmt.Sprintf("Finding latest failed run for %s/%s...", p.Owner, p.Repo))
		runs, err := ghClient.ListWorkflowRuns(ctx, p.Owner, p.Repo)
		if err != nil {
			return nil, fmt.Errorf("failed to list runs: %w", err)
		}
		for _, r := range runs {
			if r.Conclusion == "failure" {
				resolvedRunID = r.ID
				break
			}
		}
		if resolvedRunID == 0 {
			return nil, fmt.Errorf("no failed workflow runs found for %s/%s", p.Owner, p.Repo)
		}
	}

	emitInfo(p.Emitter, fmt.Sprintf("Analyzing run %d for %s/%s...", resolvedRunID, p.Owner, p.Repo))

	// Fetch run metadata, jobs, and artifacts
	wfRun, err := ghClient.GetWorkflowRun(ctx, p.Owner, p.Repo, resolvedRunID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch run: %w", err)
	}

	jobs, err := ghClient.ListJobs(ctx, p.Owner, p.Repo, resolvedRunID)
	if err != nil {
		return nil, fmt.Errorf("failed to list jobs: %w", err)
	}

	artifacts, err := ghClient.ListArtifacts(ctx, p.Owner, p.Repo, resolvedRunID)
	if err != nil {
		return nil, fmt.Errorf("failed to list artifacts: %w", err)
	}

	initialContext := buildInitialContext(wfRun, jobs, artifacts)

	handler := &llm.ToolHandler{
		GitHub:       ghClient,
		Owner:        p.Owner,
		Repo:         p.Repo,
		RunID:        resolvedRunID,
		SnapshotHTML: p.SnapshotHTML,
		Emitter:      p.Emitter,
	}

	llmClient, err := llm.NewClient()
	if err != nil {
		return nil, err
	}

	return llmClient.RunAgentLoop(ctx, handler, initialContext, p.Verbose)
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
	for _, a := range artifacts {
		expired := ""
		if a.Expired {
			expired = " [EXPIRED]"
		}
		fmt.Fprintf(&b, "- %s (%d bytes)%s\n", a.Name, a.SizeBytes, expired)
	}

	b.WriteString("\n## Instructions\n")
	b.WriteString("Analyze this workflow run to determine why it failed.\n")
	b.WriteString("Use the available tools to fetch artifacts, logs, and source files as needed.\n")
	b.WriteString("When you have enough information, call the 'done' tool, then provide your final root cause analysis.\n")

	return b.String()
}
