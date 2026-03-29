package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func alwaysOK(_ context.Context) checkResult {
	return checkResult{status: statusOK, name: "test", message: "everything is fine"}
}

func alwaysFail(_ context.Context) checkResult {
	return checkResult{status: statusFail, name: "test", message: "something broke", detail: "fix it"}
}

func alwaysInfo(_ context.Context) checkResult {
	return checkResult{status: statusInfo, name: "test", message: "heisenberg v0.0.0-test"}
}

func TestDoctor_AllPass(t *testing.T) {
	var buf bytes.Buffer
	checks := []doctorCheck{
		{"check1", alwaysOK},
		{"check2", alwaysOK},
		{"info", alwaysInfo},
	}

	err := runDoctorWith(&buf, checks)
	assert.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "everything is fine")
	assert.Contains(t, out, "heisenberg v0.0.0-test")
	assert.Contains(t, out, "2 passed")
	assert.NotContains(t, out, "failed")
}

func TestDoctor_SomeFail(t *testing.T) {
	var buf bytes.Buffer
	checks := []doctorCheck{
		{"ok", alwaysOK},
		{"fail", alwaysFail},
	}

	err := runDoctorWith(&buf, checks)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "1 check(s) failed")

	out := buf.String()
	assert.Contains(t, out, "everything is fine")
	assert.Contains(t, out, "something broke")
	assert.Contains(t, out, "fix it")
	assert.Contains(t, out, "1 passed, 1 failed")
}

func TestDoctor_AllFail(t *testing.T) {
	var buf bytes.Buffer
	checks := []doctorCheck{
		{"fail1", alwaysFail},
		{"fail2", alwaysFail},
	}

	err := runDoctorWith(&buf, checks)
	assert.Error(t, err)

	out := buf.String()
	assert.Contains(t, out, "0 passed, 2 failed")
}

func TestCheckGitHubToken_Missing(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	result := checkGitHubToken(context.Background())
	assert.Equal(t, statusFail, result.status)
	assert.Contains(t, result.message, "not set")
}

func TestCheckGoogleAPIKey_Missing(t *testing.T) {
	t.Setenv("GOOGLE_API_KEY", "")
	result := checkGoogleAPIKey(context.Background())
	assert.Equal(t, statusFail, result.status)
	assert.Contains(t, result.message, "not set")
}

func TestCheckVersion(t *testing.T) {
	result := checkVersion(context.Background())
	assert.Equal(t, statusInfo, result.status)
	assert.Contains(t, result.message, "heisenberg")
}

func TestCheckConfigFile_NoFile(t *testing.T) {
	result := checkConfigFile(context.Background())
	// Will be either OK (file exists) or INFO (not found) depending on environment
	assert.NotEqual(t, statusFail, result.status)
	assert.Contains(t, result.message, "Config file")
}
