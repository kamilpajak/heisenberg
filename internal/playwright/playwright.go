package playwright

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/kamilpajak/heisenberg/internal/server"
	"github.com/playwright-community/playwright-go"
)

// Snapshot opens a URL in headless browser and captures page content
func Snapshot(url string) ([]byte, error) {
	pw, err := playwright.Run()
	if err != nil {
		return nil, fmt.Errorf("could not start playwright: %w", err)
	}
	defer pw.Stop()

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
	})
	if err != nil {
		return nil, fmt.Errorf("could not launch browser: %w", err)
	}
	defer browser.Close()

	page, err := browser.NewPage()
	if err != nil {
		return nil, fmt.Errorf("could not create page: %w", err)
	}

	if _, err = page.Goto(url, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
	}); err != nil {
		return nil, fmt.Errorf("could not navigate: %w", err)
	}

	page.WaitForTimeout(1000)

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
			entry.Click(playwright.LocatorClickOptions{
				Timeout: playwright.Float(500),
			})
		}
	}

	page.WaitForTimeout(500)

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
	// Create temp dir for blob reports
	blobDir, err := os.MkdirTemp("", "heisenberg-blobs-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}

	// Extract all blob zips directly into the blob dir
	for i, zipData := range blobZips {
		if err := extractZip(zipData, blobDir); err != nil {
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
	cmd := exec.Command("npx", "playwright", "merge-reports",
		"--reporter", "html",
		blobDir,
	)
	cmd.Env = append(os.Environ(), "PLAYWRIGHT_HTML_OPEN=never", "PLAYWRIGHT_HTML_OUTPUT_DIR="+outputDir)

	output, err := cmd.CombinedOutput()
	os.RemoveAll(blobDir)
	if err != nil {
		os.RemoveAll(outputDir)
		return "", fmt.Errorf("merge-reports failed: %w\nOutput: %s", err, string(output))
	}

	return outputDir, nil
}

func extractZip(zipData []byte, destDir string) error {
	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return err
	}

	for _, f := range reader.File {
		path := filepath.Join(destDir, f.Name)

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

// Install installs playwright browsers
func Install() error {
	return playwright.Install()
}

// SnapshotHTML serves HTML content locally and captures page text via headless browser.
func SnapshotHTML(htmlContent []byte) ([]byte, error) {
	if !IsAvailable() {
		return nil, fmt.Errorf("playwright not installed. Run: go run github.com/playwright-community/playwright-go/cmd/playwright install chromium")
	}

	srv, err := server.Start(htmlContent, "index.html")
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

// IsAvailable checks if playwright browsers are installed
func IsAvailable() bool {
	pw, err := playwright.Run()
	if err != nil {
		return false
	}
	pw.Stop()
	return true
}
