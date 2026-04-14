package semcluster

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAntonymGuardrail_BlocksOppositeOutcomes(t *testing.T) {
	cases := []struct {
		name string
		a, b string
	}{
		{"pass vs fail", "test passed", "test failed"},
		{"succeed vs fail", "build succeeded", "build failed"},
		{"complete vs fail", "job completed", "job failed"},
		{"ok vs error", "status: ok", "status: error"},
		{"connect vs refuse", "connection accepted", "connection refused"},
		{"accept vs reject", "request accepted", "request rejected"},
		{"allow vs deny", "access allowed", "access denied"},
		{"found vs missing", "file found", "file missing"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.True(t, antonymGuardrail(tc.a, tc.b), "must block merge: %q vs %q", tc.a, tc.b)
			assert.True(t, antonymGuardrail(tc.b, tc.a), "must block merge (reverse): %q vs %q", tc.b, tc.a)
		})
	}
}

func TestAntonymGuardrail_AllowsSamePolarity(t *testing.T) {
	cases := []struct {
		name string
		a, b string
	}{
		{"both failed different detail", "connection refused on 5432", "connection refused on 5433"},
		{"both passed", "all tests passed", "integration tests passed"},
		{"both errors", "error: file not found", "error: permission denied"},
		{"unrelated", "compiler optimization level 2", "compiler optimization level 3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.False(t, antonymGuardrail(tc.a, tc.b), "must allow merge: %q vs %q", tc.a, tc.b)
		})
	}
}

func TestAntonymGuardrail_CaseInsensitive(t *testing.T) {
	assert.True(t, antonymGuardrail("Test PASSED", "test failed"))
}

func TestAntonymGuardrail_RequiresWordBoundary(t *testing.T) {
	// "passed" in "compassed" should NOT trigger a false antonym match.
	assert.False(t, antonymGuardrail("compassed value", "test failed"))
}
