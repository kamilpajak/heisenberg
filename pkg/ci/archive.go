package ci

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"strings"
)

// ExtractFirstFile extracts the first file from a ZIP archive.
// Prefers .html and .json files; falls back to any non-directory entry.
func ExtractFirstFile(zipData []byte) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, err
	}

	var fallback []byte
	for _, f := range reader.File {
		if f.FileInfo().IsDir() {
			continue
		}
		content := readZipEntry(f)
		if content == nil {
			continue
		}
		name := strings.ToLower(f.Name)
		if strings.HasSuffix(name, ".html") || strings.HasSuffix(name, ".json") {
			return content, nil
		}
		if fallback == nil {
			fallback = content
		}
	}

	if fallback != nil {
		return fallback, nil
	}
	return nil, fmt.Errorf("no files found in artifact")
}

func readZipEntry(f *zip.File) []byte {
	rc, err := f.Open()
	if err != nil {
		return nil
	}
	content, err := io.ReadAll(rc)
	rc.Close()
	if err != nil || len(content) == 0 {
		return nil
	}
	return content
}
