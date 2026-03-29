package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func alwaysOK(_ context.Context) checkResult {
	return checkResult{status: statusOK, message: "everything is fine"}
}

func alwaysFail(_ context.Context) checkResult {
	return checkResult{status: statusFail, message: "something broke", detail: "fix it"}
}

func alwaysInfo(_ context.Context) checkResult {
	return checkResult{status: statusInfo, message: "heisenberg v0.0.0-test"}
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
	check := checkGitHubTokenWith("")
	result := check(context.Background())
	assert.Equal(t, statusFail, result.status)
	assert.Contains(t, result.message, "not set")
}

func TestCheckGitHubToken_Valid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-gh-token", r.Header.Get("Authorization"))
		fmt.Fprintln(w, `{"login":"testuser"}`)
	}))
	defer srv.Close()

	check := checkGitHubTokenWithURL("test-gh-token", srv.URL+"/user")
	result := check(context.Background())
	assert.Equal(t, statusOK, result.status)
	assert.Contains(t, result.message, "@testuser")
}

func TestCheckGitHubToken_Invalid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	check := checkGitHubTokenWithURL("bad-token", srv.URL+"/user")
	result := check(context.Background())
	assert.Equal(t, statusFail, result.status)
	assert.Contains(t, result.message, "invalid")
}

func TestCheckGoogleAPIKey_Missing(t *testing.T) {
	check := checkGoogleAPIKeyWith("")
	result := check(context.Background())
	assert.Equal(t, statusFail, result.status)
	assert.Contains(t, result.message, "not set")
}

func TestCheckGoogleAPIKey_UsesHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify key is in header, not in URL
		assert.Equal(t, "test-google-key", r.Header.Get("x-goog-api-key"))
		assert.Empty(t, r.URL.Query().Get("key"), "API key should not be in URL query params")
		fmt.Fprintln(w, `{"models":[]}`)
	}))
	defer srv.Close()

	check := checkGoogleAPIKeyWithURL("test-google-key", srv.URL+"/v1beta/models")
	result := check(context.Background())
	assert.Equal(t, statusOK, result.status)
	assert.Contains(t, result.message, "validated")
}

func TestCheckGoogleAPIKey_Invalid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	check := checkGoogleAPIKeyWithURL("bad-key", srv.URL+"/v1beta/models")
	result := check(context.Background())
	assert.Equal(t, statusFail, result.status)
	assert.Contains(t, result.message, "invalid")
}

func TestCheckVersion(t *testing.T) {
	result := checkVersion(context.Background())
	assert.Equal(t, statusInfo, result.status)
	assert.Contains(t, result.message, "heisenberg")
}

func TestCheckConfigFile_NoFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	result := checkConfigFile(context.Background())
	assert.Equal(t, statusInfo, result.status)
	assert.Contains(t, result.message, "not found")
}

func TestDefaultChecks_ConfigFallback(t *testing.T) {
	// Verify defaultChecks returns the expected number of checks
	checks := defaultChecks()
	require.Len(t, checks, 7)
	assert.Equal(t, "GITHUB_TOKEN", checks[0].name)
	assert.Equal(t, "GOOGLE_API_KEY", checks[1].name)
}
