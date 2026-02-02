package heisenberg

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "heisenberg",
	Short: "AI Root Cause Analysis for Flaky Tests",
	Long: `Heisenberg analyzes test failures from GitHub Actions artifacts
and uses AI to diagnose root causes.

Supports Playwright, JUnit, pytest, and other test frameworks.`,
}

// Execute runs the root command
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(analyzeCmd)
	rootCmd.AddCommand(discoverCmd)
	rootCmd.AddCommand(versionCmd)
}
