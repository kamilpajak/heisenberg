package logclean

import (
	"regexp"
	"strings"
)

// lineClass represents whether a log line is signal or noise.
type lineClass int

const (
	lineSignal lineClass = iota
	lineNoise
)

// Static noise prefixes — checked with strings.HasPrefix for speed.
var noisePrefixes = []string{
	// Runner metadata
	"Current runner version:",
	"Runner Image",
	"Runner name",
	"Runner group name",
	"Machine name",
	"Hosted Compute Agent",
	"GITHUB_TOKEN Permissions",
	"Secret source:",
	"Prepare workflow directory",
	"Prepare all required actions",
	"Getting action download info",
	"Operating System",
	// Git operations
	"[command]/usr/bin/git",
	"Fetching the repository",
	"Initializing the repository",
	"Setting up auth",
	"Adding repository directory to the temporary git global config",
	"Temporarily overriding HOME=",
	// Post-job cleanup
	"Post job cleanup.",
	"Cleaning up orphan processes",
	"Terminate orphan process:",
	"Complete job name:",
	// Action setup
	"Download action repository",
	// GHA commands (non-error)
	"##[group]",
	"##[endgroup]",
	"##[warning]",
	// Azure DevOps noise
	"##vso[",
	"Finishing: Checkout",
	"Finishing: Initialize",
}

// Static noise substrings — checked with strings.Contains.
var noiseContains = []string{
	"safe.directory",
	"http.https://github.com/.extraheader",
}

// Noise patterns requiring regex.
var noiseRegexes = []*regexp.Regexp{
	regexp.MustCompile(`^\* \[new (branch|tag)\]`),
	regexp.MustCompile(`^git version \d`),
	regexp.MustCompile(`^git submodule foreach`),
	regexp.MustCompile(`^Node\.js \d+ actions are deprecated`),
	regexp.MustCompile(`^\(node:\d+\) \[DEP\d+\] DeprecationWarning:`),
}

// Signal patterns — always preserved, override noise.
var signalPrefixes = []string{
	"##[error]",
}

var signalRegexes = []*regexp.Regexp{
	// Test framework keywords
	regexp.MustCompile(`(?i)(?:^|\s)(?:FAIL|FAILED|ERROR|error:|panic:)`),
	// Go stack trace
	regexp.MustCompile(`^\t.+\.go:\d+`),
	// JS/TS stack trace
	regexp.MustCompile(`\([^)]+\.[jt]sx?:\d+(?::\d+)?\)`),
	// Python stack trace
	regexp.MustCompile(`File ".+", line \d+`),
	// Java stack trace
	regexp.MustCompile(`at .+\(.+\.java:\d+\)`),
	// Rust panic
	regexp.MustCompile(`panicked at`),
	// Build failures
	regexp.MustCompile(`(?i)(?:BUILD FAILED|^FAILURE:)`),
	// Exit codes
	regexp.MustCompile(`(?i)(?:exit code|exited with|exit status)\s+\d+`),
}

// classifyLine determines if a log line (with timestamp already stripped)
// is noise or signal. Signal patterns take priority over noise patterns.
// Default (no match) is signal.
func classifyLine(line string) lineClass {
	// Signal check first — overrides noise
	for _, prefix := range signalPrefixes {
		if strings.HasPrefix(line, prefix) {
			return lineSignal
		}
	}
	for _, re := range signalRegexes {
		if re.MatchString(line) {
			return lineSignal
		}
	}

	// Noise check
	for _, prefix := range noisePrefixes {
		if strings.HasPrefix(line, prefix) {
			return lineNoise
		}
	}
	for _, sub := range noiseContains {
		if strings.Contains(line, sub) {
			return lineNoise
		}
	}
	for _, re := range noiseRegexes {
		if re.MatchString(line) {
			return lineNoise
		}
	}

	// Default: signal
	return lineSignal
}
