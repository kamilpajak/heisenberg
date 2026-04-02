package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/kamilpajak/heisenberg/pkg/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	msgNotSet         = "not set"
	msgEverythingFine = "everything is fine"
)

func alwaysOK(_ context.Context) checkResult {
	return checkResult{status: statusOK, message: msgEverythingFine}
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
	assert.Contains(t, out, msgEverythingFine)
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
	assert.Contains(t, out, msgEverythingFine)
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
	assert.Contains(t, result.message, msgNotSet)
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
	assert.Contains(t, result.message, msgNotSet)
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

func TestCheckVersion_WithCommit(t *testing.T) {
	old := commit
	commit = "abc1234"
	defer func() { commit = old }()

	result := checkVersion(context.Background())
	assert.Equal(t, statusInfo, result.status)
	assert.Contains(t, result.message, "abc1234")
}

func TestCheckNetworkFunc_Reachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	check := checkNetworkFunc(ln.Addr().String())
	result := check(context.Background())
	assert.Equal(t, statusOK, result.status)
	assert.Contains(t, result.message, "reachable")
}

func TestCheckNetworkFunc_Unreachable(t *testing.T) {
	// Port 1 is almost certainly not listening
	check := checkNetworkFunc("127.0.0.1:1")
	result := check(context.Background())
	assert.Equal(t, statusFail, result.status)
	assert.Contains(t, result.message, "unreachable")
	assert.NotEmpty(t, result.detail)
}

func TestCheckConfigFile_NoFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	result := checkConfigFile(context.Background())
	assert.Equal(t, statusInfo, result.status)
	assert.Contains(t, result.message, "not found")
}

func TestCheckConfigFile_ValidWithModel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfgPath := config.Path()
	require.NoError(t, os.MkdirAll(filepath.Dir(cfgPath), 0o755))
	require.NoError(t, os.WriteFile(cfgPath, []byte("model: gemini-2.5-flash\n"), 0o644))

	result := checkConfigFile(context.Background())
	assert.Equal(t, statusOK, result.status)
	assert.Contains(t, result.message, "gemini-2.5-flash")
}

func TestCheckConfigFile_Empty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfgPath := config.Path()
	require.NoError(t, os.MkdirAll(filepath.Dir(cfgPath), 0o755))
	require.NoError(t, os.WriteFile(cfgPath, []byte(""), 0o644))

	result := checkConfigFile(context.Background())
	assert.Equal(t, statusOK, result.status)
	assert.Contains(t, result.message, "empty")
}

func TestCheckConfigFile_Invalid(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfgPath := config.Path()
	require.NoError(t, os.MkdirAll(filepath.Dir(cfgPath), 0o755))
	require.NoError(t, os.WriteFile(cfgPath, []byte("model: [broken"), 0o644))

	result := checkConfigFile(context.Background())
	assert.Equal(t, statusFail, result.status)
	assert.Contains(t, result.message, "invalid")
}

func TestDefaultChecks_ConfigFallback(t *testing.T) {
	t.Setenv("AZURE_DEVOPS_PAT", "")
	// Verify defaultChecks returns the expected number of checks (no Azure PAT → 7)
	checks := defaultChecks()
	require.Len(t, checks, 7)
	assert.Equal(t, "GITHUB_TOKEN", checks[0].name)
	assert.Equal(t, "GOOGLE_API_KEY", checks[1].name)
}

func TestDefaultChecks_WithAzurePAT(t *testing.T) {
	t.Setenv("AZURE_DEVOPS_PAT", "test-pat-12345678")
	checks := defaultChecks()
	require.Len(t, checks, 9)
	assert.Equal(t, "AZURE_DEVOPS_PAT", checks[7].name)
	assert.Equal(t, "Network: dev.azure.com", checks[8].name)
}

func TestCheckAzurePAT_Missing(t *testing.T) {
	check := checkAzurePATWith("")
	result := check(context.Background())
	assert.Equal(t, statusFail, result.status)
	assert.Contains(t, result.message, msgNotSet)
}

func TestCheckAzurePAT_Valid(t *testing.T) {
	check := checkAzurePATWith("abcdefghij1234567890")
	result := check(context.Background())
	assert.Equal(t, statusOK, result.status)
	assert.Contains(t, result.message, "abcd")
	assert.Contains(t, result.message, "7890")
}

func TestCheckAzurePAT_Short(t *testing.T) {
	check := checkAzurePATWith("short")
	result := check(context.Background())
	assert.Equal(t, statusOK, result.status)
	assert.Contains(t, result.message, "***")
}
