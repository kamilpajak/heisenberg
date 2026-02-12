package trace

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/kamilpajak/heisenberg/internal/artifactserver"
	"github.com/playwright-community/playwright-go"
)

// CommandRunner abstracts command execution for testing.
type CommandRunner interface {
	Run(name string, args []string, env []string) ([]byte, error)
}

// ExecRunner is the default implementation using os/exec.
type ExecRunner struct{}

// Run executes a command and returns combined output.
func (ExecRunner) Run(name string, args []string, env []string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	return cmd.CombinedOutput()
}

// DefaultRunner is used when no runner is provided.
var DefaultRunner CommandRunner = ExecRunner{}

// Snapshot opens a URL in headless browser and captures page content
func Snapshot(url string) ([]byte, error) {
	pw, err := playwright.Run()
	if err != nil {
		return nil, fmt.Errorf("could not start playwright: %w", err)
	}
	defer pw.Stop() //nolint:errcheck // best-effort cleanup

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
	})
	if err != nil {
		return nil, fmt.Errorf("could not launch browser: %w", err)
	}
	defer browser.Close() //nolint:errcheck // best-effort cleanup

	page, err := browser.NewPage()
	if err != nil {
		return nil, fmt.Errorf("could not create page: %w", err)
	}

	if _, err = page.Goto(url, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
	}); err != nil {
		return nil, fmt.Errorf("could not navigate: %w", err)
	}

	page.WaitForTimeout(1000) //nolint:staticcheck // intentional delay for page render in browser automation

	// Try to expand failed test sections (common patterns in test reporters)
	expandSelectors := []string{
		".failed",
		"[data-testid='expand']",
		"[aria-expanded='false']",
		"details:not([open]) summary",
	}

	for _, selector := range expandSelectors {
		entries, _ := page.Locator(selector).All()
		for i, entry := range entries {
			if i >= 20 {
				break
			}
			entry.Click(playwright.LocatorClickOptions{ //nolint:errcheck // best-effort UI expansion
				Timeout: playwright.Float(500),
			})
		}
	}

	page.WaitForTimeout(500) //nolint:staticcheck // intentional delay for click effects in browser automation

	content, err := page.Locator("body").InnerText()
	if err != nil {
		return nil, fmt.Errorf("could not get page content: %w", err)
	}

	if len(content) > 50000 {
		content = content[:50000]
	}

	return []byte(content), nil
}

// MergeBlobReports merges Playwright blob report ZIPs into an HTML report.
// Returns the path to a temp directory containing the merged HTML report.
func MergeBlobReports(blobZips [][]byte) (string, error) {
	return MergeBlobReportsWithRunner(blobZips, DefaultRunner)
}

// MergeBlobReportsWithRunner merges blob reports using a custom command runner.
func MergeBlobReportsWithRunner(blobZips [][]byte, runner CommandRunner) (string, error) {
	// Create temp dir for blob reports
	blobDir, err := os.MkdirTemp("", "heisenberg-blobs-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}

	// Extract all blob zips directly into the blob dir
	for i, zipData := range blobZips {
		if err := extractSnapshotZip(zipData, blobDir); err != nil {
			os.RemoveAll(blobDir)
			return "", fmt.Errorf("failed to extract blob %d: %w", i, err)
		}
	}

	// Create output dir for merged HTML report
	outputDir, err := os.MkdirTemp("", "heisenberg-report-*")
	if err != nil {
		os.RemoveAll(blobDir)
		return "", fmt.Errorf("failed to create output dir: %w", err)
	}

	// Run npx playwright merge-reports
	npx := "npx"
	if runtime.GOOS == "windows" {
		npx = "npx.cmd"
	}
	args := []string{"playwright", "merge-reports", "--reporter", "html", blobDir}
	env := []string{"PLAYWRIGHT_HTML_OPEN=never", "PLAYWRIGHT_HTML_OUTPUT_DIR=" + outputDir}

	output, err := runner.Run(npx, args, env)
	os.RemoveAll(blobDir)
	if err != nil {
		os.RemoveAll(outputDir)
		return "", fmt.Errorf("merge-reports failed: %w\nOutput: %s", err, string(output))
	}

	return outputDir, nil
}

func extractSnapshotZip(zipData []byte, destDir string) error {
	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return err
	}

	// Clean destDir for consistent comparison
	cleanDest := filepath.Clean(destDir) + string(os.PathSeparator)

	for _, f := range reader.File {
		path := filepath.Join(destDir, f.Name)

		// Prevent zip slip: ensure path is within destDir
		if !strings.HasPrefix(filepath.Clean(path)+string(os.PathSeparator), cleanDest) {
			return fmt.Errorf("illegal file path in zip: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(path, 0755)
			continue
		}

		os.MkdirAll(filepath.Dir(path), 0755)

		rc, err := f.Open()
		if err != nil {
			return err
		}

		out, err := os.Create(path)
		if err != nil {
			rc.Close()
			return err
		}

		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
	}

	return nil
}

// InstallPlaywright installs playwright browsers
func InstallPlaywright() error {
	return playwright.Install()
}

// SnapshotHTML serves HTML content locally and captures page text via headless browser.
func SnapshotHTML(htmlContent []byte) ([]byte, error) {
	if !IsPlaywrightAvailable() {
		return nil, fmt.Errorf("playwright not installed. Run: go run github.com/playwright-community/playwright-go/cmd/playwright install chromium")
	}

	srv, err := artifactserver.Start(htmlContent, "index.html")
	if err != nil {
		return nil, fmt.Errorf("failed to start server: %w", err)
	}
	defer srv.Stop()

	snapshot, err := Snapshot(srv.URL("index.html"))
	if err != nil {
		return nil, fmt.Errorf("failed to capture snapshot: %w", err)
	}

	return snapshot, nil
}

// IsPlaywrightAvailable checks if playwright browsers are installed
func IsPlaywrightAvailable() bool {
	pw, err := playwright.Run()
	if err != nil {
		return false
	}
	pw.Stop() //nolint:errcheck // best-effort cleanup
	return true
}
