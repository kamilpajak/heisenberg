package heisenberg

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/kamilpajak/heisenberg/internal/discovery"
	"github.com/kamilpajak/heisenberg/internal/github"
	"github.com/spf13/cobra"
)

var (
	discoverLimit    int
	discoverMinStars int
	discoverRepo     string
	discoverFormat   string
	discoverQuery    string
)

var discoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "Discover GitHub repos with test artifacts",
	Long: `Search GitHub for repositories with Playwright or other test artifacts.

Without --repo, searches for compatible repositories using --query.
With --repo, checks if a specific repository is compatible.

Examples:
  heisenberg discover --repo playwright/playwright
  heisenberg discover --query "playwright test" --limit 10
  heisenberg discover --query "language:typescript playwright" --min-stars 500`,
	RunE: runDiscover,
}

func init() {
	discoverCmd.Flags().IntVarP(&discoverLimit, "limit", "l", 30, "Maximum repos to analyze")
	discoverCmd.Flags().IntVar(&discoverMinStars, "min-stars", 100, "Minimum stars filter")
	discoverCmd.Flags().StringVar(&discoverRepo, "repo", "", "Check specific repo (owner/repo)")
	discoverCmd.Flags().StringVarP(&discoverFormat, "format", "f", "text", "Output format (text, json)")
	discoverCmd.Flags().StringVarP(&discoverQuery, "query", "q", "playwright test", "Search query for discovering repos")
}

func runDiscover(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Create GitHub client
	ghClient := github.NewClient("")

	// Create discovery service
	svc := discovery.New(ghClient)

	if discoverRepo != "" {
		return discoverSingleRepo(ctx, svc, discoverRepo)
	}

	return discoverMultipleRepos(ctx, svc)
}

func discoverSingleRepo(ctx context.Context, svc *discovery.Service, repo string) error {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid repo format, expected owner/repo: %s", repo)
	}

	fmt.Fprintf(os.Stderr, "Checking repository: %s\n", repo)

	result, err := svc.DiscoverRepo(ctx, parts[0], parts[1])
	if err != nil {
		return err
	}

	if discoverFormat == "json" {
		return outputDiscoverJSON([]*discovery.RepoDiscoveryResult{result})
	}

	return outputDiscoverText([]*discovery.RepoDiscoveryResult{result})
}

func discoverMultipleRepos(ctx context.Context, svc *discovery.Service) error {
	fmt.Fprintf(os.Stderr, "Searching for repos: query=%q min-stars=%d limit=%d\n",
		discoverQuery, discoverMinStars, discoverLimit)

	results, err := svc.SearchAndDiscover(ctx, discoverQuery, discoverMinStars, discoverLimit)
	if err != nil {
		return err
	}

	// Convert to pointer slice for output
	resultPtrs := make([]*discovery.RepoDiscoveryResult, len(results))
	for i := range results {
		resultPtrs[i] = &results[i]
	}

	if discoverFormat == "json" {
		return outputDiscoverJSON(resultPtrs)
	}

	return outputDiscoverText(resultPtrs)
}

func outputDiscoverJSON(results []*discovery.RepoDiscoveryResult) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(results)
}

func outputDiscoverText(results []*discovery.RepoDiscoveryResult) error {
	compatible := 0
	withFailures := 0

	for _, r := range results {
		if r.Compatible {
			compatible++
			for _, a := range r.Artifacts {
				if a.HasFailures {
					withFailures++
					break
				}
			}
		}
	}

	fmt.Printf("\n## Discovery Results\n\n")
	fmt.Printf("Total repos checked: %d\n", len(results))
	fmt.Printf("Compatible repos: %d\n", compatible)
	fmt.Printf("Repos with failures: %d\n\n", withFailures)

	if compatible == 0 {
		fmt.Println("No compatible repositories found.")
		return nil
	}

	fmt.Println("### Compatible Repositories")

	for _, r := range results {
		if !r.Compatible {
			continue
		}

		fmt.Printf("**%s**\n", r.Repository)

		for _, a := range r.Artifacts {
			status := "passing"
			if a.HasFailures {
				status = fmt.Sprintf("FAILING (%d/%d)", a.FailedTests, a.TotalTests)
			} else if a.TotalTests > 0 {
				status = fmt.Sprintf("passing (%d tests)", a.TotalTests)
			}

			fmt.Printf("  - Artifact: %s (%s)\n", a.ArtifactName, a.ReportType)
			fmt.Printf("    Status: %s\n", status)
			fmt.Printf("    Run: %s (#%d)\n", a.WorkflowRun.Name, a.WorkflowRun.RunNumber)
		}
		fmt.Println()
	}

	// Show incompatible repos with errors
	hasErrors := false
	for _, r := range results {
		if !r.Compatible && r.Error != "" {
			if !hasErrors {
				fmt.Println("### Incompatible Repositories")
				hasErrors = true
			}
			fmt.Printf("- %s: %s\n", r.Repository, r.Error)
		}
	}

	return nil
}
