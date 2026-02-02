package heisenberg

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	discoverLimit    int
	discoverMinStars int
	discoverRepo     string
)

var discoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "Discover GitHub repos with test artifacts",
	Long: `Search GitHub for repositories with Playwright or other test artifacts.

Without --repo, searches for compatible repositories.
With --repo, checks if a specific repository is compatible.`,
	RunE: runDiscover,
}

func init() {
	discoverCmd.Flags().IntVarP(&discoverLimit, "limit", "l", 30, "Maximum repos to analyze")
	discoverCmd.Flags().IntVar(&discoverMinStars, "min-stars", 100, "Minimum stars filter")
	discoverCmd.Flags().StringVar(&discoverRepo, "repo", "", "Check specific repo (owner/repo)")
}

func runDiscover(cmd *cobra.Command, args []string) error {
	if discoverRepo != "" {
		fmt.Printf("Checking repo: %s\n", discoverRepo)
	} else {
		fmt.Printf("Discovering repos (limit=%d, min-stars=%d)\n", discoverLimit, discoverMinStars)
	}

	// TODO: Implement discovery
	return nil
}
