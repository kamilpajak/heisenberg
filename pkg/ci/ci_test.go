package ci

import (
	"archive/zip"
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckArtifacts(t *testing.T) {
	tests := []struct {
		name       string
		artifacts  []Artifact
		wantUsable bool
		wantExpAll bool
	}{
		{
			"all available",
			[]Artifact{{ID: 1, Expired: false}, {ID: 2, Expired: false}},
			true, false,
		},
		{
			"all expired",
			[]Artifact{{ID: 1, Expired: true}, {ID: 2, Expired: true}},
			false, true,
		},
		{
			"mixed",
			[]Artifact{{ID: 1, Expired: true}, {ID: 2, Expired: false}},
			true, false,
		},
		{
			"empty",
			[]Artifact{},
			false, false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := CheckArtifacts(tt.artifacts)
			assert.Equal(t, tt.wantUsable, status.HasUsable)
			assert.Equal(t, tt.wantExpAll, status.AllExpired)
			assert.Equal(t, len(tt.artifacts), status.Total)
		})
	}
}

func TestExtractFirstFile(t *testing.T) {
	data := buildZip(t, "data.json", []byte(`{"key":"value"}`))
	content, err := ExtractFirstFile(data)
	require.NoError(t, err)
	assert.Equal(t, `{"key":"value"}`, string(content))
}

func TestExtractFirstFile_Empty(t *testing.T) {
	data := buildZip(t, "", nil)
	_, err := ExtractFirstFile(data)
	assert.Error(t, err)
}

func buildZip(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	if name != "" && content != nil {
		fw, err := w.Create(name)
		require.NoError(t, err)
		_, err = fw.Write(content)
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	return buf.Bytes()
}
