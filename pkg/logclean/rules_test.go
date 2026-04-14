package logclean

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	noiseAssertMsg  = "expected noise: %q"
	signalAssertMsg = "expected signal: %q"
)

func TestClassifyLine_RunnerMetadata(t *testing.T) {
	noiseLines := []string{
		"Current runner version: '2.333.1'",
		"Runner Image Provisioner",
		"Hosted Compute Agent",
		"GITHUB_TOKEN Permissions",
		"Secret source: Actions",
		"Prepare workflow directory",
		"Prepare all required actions",
		"Getting action download info",
		"Runner name : 'GitHub Actions 42'",
		"Runner group name : 'Default'",
		"Machine name : 'fv-az1234-567'",
		"Operating System",
	}
	for _, line := range noiseLines {
		assert.Equal(t, lineNoise, classifyLine(line), noiseAssertMsg, line)
	}
}

func TestClassifyLine_GitCommands(t *testing.T) {
	noiseLines := []string{
		"[command]/usr/bin/git version",
		"[command]/usr/bin/git config --global --add safe.directory /home/runner/work/repo/repo",
		"[command]/usr/bin/git submodule foreach --recursive sh -c \"git config --local\"",
		"* [new branch]      main -> origin/main",
		"* [new tag]          v1.0.0 -> v1.0.0",
		"Fetching the repository",
		"Initializing the repository",
		"Setting up auth",
		"Adding repository directory to the temporary git global config as a safe directory",
		"Temporarily overriding HOME='/home/runner/work/_temp/abc123'",
		"git version 2.53.0",
		"http.https://github.com/.extraheader",
	}
	for _, line := range noiseLines {
		assert.Equal(t, lineNoise, classifyLine(line), noiseAssertMsg, line)
	}
}

func TestClassifyLine_PostJobCleanup(t *testing.T) {
	noiseLines := []string{
		"Post job cleanup.",
		"Cleaning up orphan processes",
		"Terminate orphan process: pid (20684) (java)",
		"Complete job name: Build",
	}
	for _, line := range noiseLines {
		assert.Equal(t, lineNoise, classifyLine(line), noiseAssertMsg, line)
	}
}

func TestClassifyLine_ActionSetup(t *testing.T) {
	noiseLines := []string{
		"Download action repository 'actions/checkout@v6' (SHA:de0fac2e4500dabe)",
		"Download action repository 'actions/setup-node@v4' (SHA:abc123)",
	}
	for _, line := range noiseLines {
		assert.Equal(t, lineNoise, classifyLine(line), noiseAssertMsg, line)
	}
}

func TestClassifyLine_DeprecationWarnings(t *testing.T) {
	noiseLines := []string{
		"Node.js 20 actions are deprecated. The following actions are running on Node.js 20",
		"(node:12345) [DEP0040] DeprecationWarning: The punycode module is deprecated",
	}
	for _, line := range noiseLines {
		assert.Equal(t, lineNoise, classifyLine(line), noiseAssertMsg, line)
	}
}

func TestClassifyLine_GHACommands(t *testing.T) {
	noiseLines := []string{
		"##[group]Run actions/checkout@v6",
		"##[endgroup]",
		"##[warning]Node.js 20 actions are deprecated",
	}
	for _, line := range noiseLines {
		assert.Equal(t, lineNoise, classifyLine(line), noiseAssertMsg, line)
	}
}

func TestClassifyLine_AzureDevOpsNoise(t *testing.T) {
	noiseLines := []string{
		"##vso[task.prependpath]/opt/hostedtoolcache/Python/3.10.0/x64",
		"##vso[task.complete result=Succeeded;]",
		"Finishing: Checkout repo@main",
		"Finishing: Initialize job",
	}
	for _, line := range noiseLines {
		assert.Equal(t, lineNoise, classifyLine(line), noiseAssertMsg, line)
	}
}

func TestClassifyLine_ErrorLines(t *testing.T) {
	signalLines := []string{
		"Error: connection refused",
		"FAIL: TestLogin (0.5s)",
		"FAILED tests/test_auth.py::test_login",
		"panic: runtime error: index out of range",
		"error: script \"test\" exited with code 1",
		"ERROR: build failed",
	}
	for _, line := range signalLines {
		assert.Equal(t, lineSignal, classifyLine(line), signalAssertMsg, line)
	}
}

func TestClassifyLine_StackTraces(t *testing.T) {
	signalLines := []string{
		"\t/app/pkg/server/handler.go:42",
		"    at processTicksAndRejections (node:internal/process/task_queues:95:5)",
		"    at Object.<anonymous> (src/config.test.ts:8:31)",
		`File "/app/tests/test_auth.py", line 42`,
		"at com.example.AppTest.testLogin(AppTest.java:42)",
		"thread 'main' panicked at src/main.rs:42:5",
	}
	for _, line := range signalLines {
		assert.Equal(t, lineSignal, classifyLine(line), signalAssertMsg, line)
	}
}

func TestClassifyLine_BuildFailures(t *testing.T) {
	signalLines := []string{
		"BUILD FAILED in 42s",
		"FAILURE: Build failed with an exception.",
	}
	for _, line := range signalLines {
		assert.Equal(t, lineSignal, classifyLine(line), signalAssertMsg, line)
	}
}

func TestClassifyLine_ExitCodes(t *testing.T) {
	signalLines := []string{
		"Process completed with exit code 1.",
		"exited with code 2",
		"exit status 1",
	}
	for _, line := range signalLines {
		assert.Equal(t, lineSignal, classifyLine(line), signalAssertMsg, line)
	}
}

func TestClassifyLine_ErrorAnnotation(t *testing.T) {
	assert.Equal(t, lineSignal, classifyLine("##[error]Process completed with exit code 1."))
	assert.Equal(t, lineSignal, classifyLine("##[error]Received unexpected error"))
}

func TestClassifyLine_NormalBuildOutput(t *testing.T) {
	signalLines := []string{
		"Building project...",
		"Compiling src/main.ts",
		"Running tests...",
		"(pass) Network Configuration > resolveApiUrl > should return default API URL",
		"(fail) loadConfig > returns null privateKey when MONACO_PRIVATE_KEY is missing [2.00ms]",
		"npm warn deprecated some-package@1.0.0",
		"bun install v1.3.11 (af24e281)",
		"250 pass",
		"5 fail",
	}
	for _, line := range signalLines {
		assert.Equal(t, lineSignal, classifyLine(line), signalAssertMsg, line)
	}
}

func TestSignalOverridesNoise(t *testing.T) {
	// ##[error] starts with ##[ which is a noise prefix, but ##[error] is signal
	assert.Equal(t, lineSignal, classifyLine("##[error]Process completed with exit code 1."))

	// A git command that contains an error keyword
	assert.Equal(t, lineSignal, classifyLine("[command]/usr/bin/git clone failed: error: RPC failed"))
}
