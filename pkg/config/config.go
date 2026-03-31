package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds optional user configuration loaded from a YAML file.
type Config struct {
	Model          string `yaml:"model"`
	GitHubToken    string `yaml:"github_token"`
	GoogleAPIKey   string `yaml:"google_api_key"`
	AzureDevOpsPAT string `yaml:"azure_devops_pat"`
}

// Load reads the config file from the default path and returns the parsed config.
// Returns an empty Config if the file does not exist.
// Returns an error only if the file exists but is malformed.
func Load() (*Config, error) {
	return LoadFrom(Path())
}

// LoadFrom reads a config file from the given path.
// Returns an empty Config if the file does not exist.
func LoadFrom(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &cfg, nil
}

// Path returns the config file path using the OS-appropriate config directory.
func Path() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, "heisenberg", "config.yaml")
}
