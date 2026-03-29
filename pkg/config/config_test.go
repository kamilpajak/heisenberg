package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadFrom_NoFile(t *testing.T) {
	cfg, err := LoadFrom(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	require.NoError(t, err)
	assert.Equal(t, &Config{}, cfg)
}

func TestLoadFrom_ValidYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "model: gemini-2.5-pro\ngithub_token: gh_abc\ngoogle_api_key: gk_xyz\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	cfg, err := LoadFrom(path)
	require.NoError(t, err)
	assert.Equal(t, "gemini-2.5-pro", cfg.Model)
	assert.Equal(t, "gh_abc", cfg.GitHubToken)
	assert.Equal(t, "gk_xyz", cfg.GoogleAPIKey)
}

func TestLoadFrom_InvalidYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("model: [invalid"), 0o644))

	_, err := LoadFrom(path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parsing")
}

func TestLoadFrom_PartialConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("model: gemini-2.5-flash\n"), 0o644))

	cfg, err := LoadFrom(path)
	require.NoError(t, err)
	assert.Equal(t, "gemini-2.5-flash", cfg.Model)
	assert.Empty(t, cfg.GitHubToken)
	assert.Empty(t, cfg.GoogleAPIKey)
}

func TestPath(t *testing.T) {
	path := Path()
	assert.Contains(t, path, "heisenberg")
	assert.True(t, filepath.IsAbs(path))
	assert.Equal(t, "config.yaml", filepath.Base(path))
}
