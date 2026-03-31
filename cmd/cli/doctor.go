package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/fatih/color"
	"github.com/kamilpajak/heisenberg/pkg/config"
	"github.com/kamilpajak/heisenberg/pkg/trace"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check environment and configuration",
	RunE:  runDoctor,
}

// checkStatus represents the outcome of a single doctor check.
type checkStatus int

const (
	statusOK checkStatus = iota
	statusFail
	statusInfo
)

// checkResult holds the outcome of a single doctor check.
type checkResult struct {
	status  checkStatus
	message string
	detail  string // optional hint shown on failure
}

// doctorCheck is a single diagnostic check.
type doctorCheck struct {
	name string
	run  func(ctx context.Context) checkResult
}

const (
	githubAPIURL = "https://api.github.com/user"
	googleAPIURL = "https://generativelanguage.googleapis.com/v1beta/models?pageSize=1"
)

// defaultChecks returns the standard set of doctor checks.
// Tokens are resolved from env vars with config file fallback.
func defaultChecks() []doctorCheck {
	cfg, _ := config.Load()

	ghToken := os.Getenv("GITHUB_TOKEN")
	if ghToken == "" && cfg != nil {
		ghToken = cfg.GitHubToken
	}

	googleKey := os.Getenv("GOOGLE_API_KEY")
	if googleKey == "" && cfg != nil {
		googleKey = cfg.GoogleAPIKey
	}

	checks := []doctorCheck{
		{"GITHUB_TOKEN", checkGitHubTokenWith(ghToken)},
		{"GOOGLE_API_KEY", checkGoogleAPIKeyWith(googleKey)},
		{"Network: api.github.com", checkNetworkFunc("api.github.com:443")},
		{"Network: generativelanguage.googleapis.com", checkNetworkFunc("generativelanguage.googleapis.com:443")},
		{"Playwright browser", checkPlaywright},
		{"Config file", checkConfigFile},
		{"Version", checkVersion},
	}

	// Azure checks — only shown when Azure PAT is configured
	azurePAT := os.Getenv("AZURE_DEVOPS_PAT")
	if azurePAT == "" && cfg != nil {
		azurePAT = cfg.AzureDevOpsPAT
	}
	if azurePAT != "" {
		checks = append(checks,
			doctorCheck{"AZURE_DEVOPS_PAT", checkAzurePATWith(azurePAT)},
			doctorCheck{"Network: dev.azure.com", checkNetworkFunc("dev.azure.com:443")},
		)
	}

	return checks
}

func runDoctor(cmd *cobra.Command, args []string) error {
	return runDoctorWith(os.Stderr, defaultChecks())
}

func runDoctorWith(w io.Writer, checks []doctorCheck) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	bold := color.New(color.Bold)
	green := color.New(color.FgGreen)
	red := color.New(color.FgRed)
	dim := color.New(color.FgHiBlack)

	fmt.Fprintln(w)
	_, _ = bold.Fprintln(w, "  heisenberg doctor")
	fmt.Fprintln(w)

	var passed, failed int

	for _, c := range checks {
		result := c.run(ctx)

		var prefix string
		switch result.status {
		case statusOK:
			prefix = green.Sprint("[OK]")
			passed++
		case statusFail:
			prefix = red.Sprint("[FAIL]")
			failed++
		case statusInfo:
			prefix = dim.Sprint("[INFO]")
		}

		fmt.Fprintf(w, "  %s %s\n", prefix, result.message)
		if result.detail != "" {
			_, _ = dim.Fprintf(w, "       %s\n", result.detail)
		}
	}

	fmt.Fprintln(w)
	summary := fmt.Sprintf("  %d passed", passed)
	if failed > 0 {
		summary += fmt.Sprintf(", %d failed", failed)
	}
	fmt.Fprintln(w, summary)
	fmt.Fprintln(w)

	if failed > 0 {
		return fmt.Errorf("%d check(s) failed", failed)
	}
	return nil
}

// checkGitHubTokenWith returns a check that validates the given GitHub token.
func checkGitHubTokenWith(token string) func(ctx context.Context) checkResult {
	return checkGitHubTokenWithURL(token, githubAPIURL)
}

// checkGitHubTokenWithURL is the testable core — accepts a custom API URL.
func checkGitHubTokenWithURL(token, apiURL string) func(ctx context.Context) checkResult {
	return func(ctx context.Context) checkResult {
		if token == "" {
			return checkResult{
				status:  statusFail,
				message: "GITHUB_TOKEN is not set",
				detail:  "Set GITHUB_TOKEN environment variable or github_token in config file",
			}
		}

		req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
		if err != nil {
			return checkResult{status: statusOK, message: "GITHUB_TOKEN is set (not validated)"}
		}
		req.Header.Set("Authorization", "Bearer "+token)

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return checkResult{status: statusOK, message: "GITHUB_TOKEN is set (validation failed: network error)"}
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return checkResult{
				status:  statusFail,
				message: fmt.Sprintf("GITHUB_TOKEN is set but invalid (HTTP %d)", resp.StatusCode),
				detail:  "Check that your token is valid and has not expired",
			}
		}

		var user struct {
			Login string `json:"login"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&user); err == nil && user.Login != "" {
			return checkResult{status: statusOK, message: fmt.Sprintf("GITHUB_TOKEN is set (authenticated as @%s)", user.Login)}
		}
		return checkResult{status: statusOK, message: "GITHUB_TOKEN is set (validated)"}
	}
}

// checkGoogleAPIKeyWith returns a check that validates the given Google API key.
func checkGoogleAPIKeyWith(key string) func(ctx context.Context) checkResult {
	return checkGoogleAPIKeyWithURL(key, googleAPIURL)
}

// checkGoogleAPIKeyWithURL is the testable core — accepts a custom API URL.
func checkGoogleAPIKeyWithURL(key, apiURL string) func(ctx context.Context) checkResult {
	return func(ctx context.Context) checkResult {
		if key == "" {
			return checkResult{
				status:  statusFail,
				message: "GOOGLE_API_KEY is not set",
				detail:  "Set GOOGLE_API_KEY environment variable or google_api_key in config file",
			}
		}

		req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
		if err != nil {
			return checkResult{status: statusOK, message: "GOOGLE_API_KEY is set (not validated)"}
		}
		req.Header.Set("x-goog-api-key", key)

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return checkResult{status: statusOK, message: "GOOGLE_API_KEY is set (validation failed: network error)"}
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return checkResult{
				status:  statusFail,
				message: fmt.Sprintf("GOOGLE_API_KEY is set but invalid (HTTP %d)", resp.StatusCode),
				detail:  "Check that your API key is valid at https://aistudio.google.com/apikey",
			}
		}

		return checkResult{status: statusOK, message: "GOOGLE_API_KEY is set (validated)"}
	}
}

// checkAzurePATWith returns a check that validates the Azure DevOps PAT.
func checkAzurePATWith(pat string) func(ctx context.Context) checkResult {
	return func(_ context.Context) checkResult {
		if pat == "" {
			return checkResult{
				status:  statusFail,
				message: "AZURE_DEVOPS_PAT is not set",
				detail:  "Set AZURE_DEVOPS_PAT environment variable or azure_devops_pat in config file",
			}
		}

		masked := pat[:4] + "..." + pat[len(pat)-4:]
		if len(pat) < 10 {
			masked = "***"
		}
		return checkResult{status: statusOK, message: fmt.Sprintf("AZURE_DEVOPS_PAT is set (%s)", masked)}
	}
}

func checkNetworkFunc(addr string) func(ctx context.Context) checkResult {
	return func(ctx context.Context) checkResult {
		host, _, _ := net.SplitHostPort(addr)
		dialer := net.Dialer{Timeout: 5 * time.Second}
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			return checkResult{
				status:  statusFail,
				message: fmt.Sprintf("Network: %s unreachable", host),
				detail:  fmt.Sprintf("Could not connect: %v", err),
			}
		}
		_ = conn.Close()
		return checkResult{status: statusOK, message: fmt.Sprintf("Network: %s reachable", host)}
	}
}

func checkPlaywright(_ context.Context) checkResult {
	if trace.IsPlaywrightAvailable() {
		return checkResult{status: statusOK, message: "Playwright browser installed"}
	}
	return checkResult{
		status:  statusFail,
		message: "Playwright browser not installed",
		detail:  "Run: go run github.com/playwright-community/playwright-go/cmd/playwright install chromium",
	}
}

func checkConfigFile(_ context.Context) checkResult {
	path := config.Path()
	cfg, err := config.Load()
	if err != nil {
		return checkResult{
			status:  statusFail,
			message: fmt.Sprintf("Config file: %s (invalid)", path),
			detail:  err.Error(),
		}
	}

	if cfg.Model == "" && cfg.GitHubToken == "" && cfg.GoogleAPIKey == "" {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return checkResult{status: statusInfo, message: fmt.Sprintf("Config file: %s (not found, using defaults)", path)}
		}
		return checkResult{status: statusOK, message: fmt.Sprintf("Config file: %s (empty)", path)}
	}

	msg := fmt.Sprintf("Config file: %s", path)
	if cfg.Model != "" {
		msg += fmt.Sprintf(" (model: %s)", cfg.Model)
	}
	return checkResult{status: statusOK, message: msg}
}

func checkVersion(_ context.Context) checkResult {
	msg := fmt.Sprintf("heisenberg %s", version)
	if commit != "none" {
		msg += fmt.Sprintf(" (commit: %s)", commit)
	}
	return checkResult{status: statusInfo, message: msg}
}
