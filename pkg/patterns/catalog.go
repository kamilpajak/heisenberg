package patterns

import (
	_ "embed"
	"encoding/json"
)

//go:embed catalog.json
var catalogJSON []byte

// CatalogEntry represents a known failure pattern in the curated catalog.
type CatalogEntry struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	FailureType  string   `json:"failure_type"`
	FilePatterns []string `json:"file_patterns"`
	ErrorTokens  []string `json:"error_tokens"`
	Frequency    string   `json:"frequency"`
}

// LoadCatalog parses the embedded pattern catalog.
func LoadCatalog() ([]CatalogEntry, error) {
	var entries []CatalogEntry
	if err := json.Unmarshal(catalogJSON, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}
