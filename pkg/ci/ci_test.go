package ci

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
