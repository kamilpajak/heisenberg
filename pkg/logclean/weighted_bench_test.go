package logclean

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestWeighted_SizeReduction(t *testing.T) {
	fixtures := []string{"github_brokk.txt", "github_dms_workspace.txt", "github_monaco.txt"}
	budgets := []int{5000, 10000, 30000, 80000}

	for _, name := range fixtures {
		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		input := string(data)

		for _, budget := range budgets {
			t.Run(fmt.Sprintf("%s/budget=%d", name, budget), func(t *testing.T) {
				_ = os.Unsetenv(weightedFlagEnv)
				baseline, bStats := Extract(input, budget)

				t.Setenv(weightedFlagEnv, "1")
				weighted, wStats := Extract(input, budget)

				t.Logf("input=%d bytes, budget=%d", len(input), budget)
				t.Logf("  baseline: %d bytes, %d lines (dropped=%d, fallback=%v)",
					len(baseline), bStats.OutputLines, bStats.DroppedLines, bStats.FallbackUsed)
				t.Logf("  weighted: %d bytes, %d lines (dropped=%d, fallback=%v)",
					len(weighted), wStats.OutputLines, wStats.DroppedLines, wStats.FallbackUsed)
			})
		}
	}
}
