package patterns

import (
	"testing"

	"github.com/kamilpajak/heisenberg/pkg/llm"
	"github.com/stretchr/testify/assert"
)

func TestComputeFingerprint_Full(t *testing.T) {
	rca := &llm.RootCauseAnalysis{
		FailureType: "timeout",
		Location:    &llm.CodeLocation{FilePath: "tests/checkout.spec.ts", LineNumber: 28},
		RootCause:   "CSS selector changed, waitForSelector timed out in beforeEach hook",
	}

	fp := ComputeFingerprint(rca)

	assert.Equal(t, "timeout", fp.FailureType)
	assert.Equal(t, "*.ts", fp.FilePattern)
	assert.Contains(t, fp.ErrorTokens, "selector")
	assert.Contains(t, fp.ErrorTokens, "waitforselector")
	assert.Contains(t, fp.ErrorTokens, "timed")
	assert.Contains(t, fp.ErrorTokens, "beforeeach")
}

func TestComputeFingerprint_NoLocation(t *testing.T) {
	rca := &llm.RootCauseAnalysis{
		FailureType: "assertion",
		RootCause:   "Expected text 'Login' but got 'Sign In'",
	}

	fp := ComputeFingerprint(rca)

	assert.Equal(t, "assertion", fp.FailureType)
	assert.Empty(t, fp.FilePattern)
	assert.Contains(t, fp.ErrorTokens, "expected")
	assert.Contains(t, fp.ErrorTokens, "login")
}

func TestComputeFingerprint_NormalizesCase(t *testing.T) {
	rca := &llm.RootCauseAnalysis{
		FailureType: "TIMEOUT",
		RootCause:   "Connection REFUSED to localhost:5432",
	}

	fp := ComputeFingerprint(rca)

	assert.Equal(t, "timeout", fp.FailureType)
	assert.Contains(t, fp.ErrorTokens, "connection")
	assert.Contains(t, fp.ErrorTokens, "refused")
}

func TestComputeFingerprint_FiltersShortTokens(t *testing.T) {
	rca := &llm.RootCauseAnalysis{
		FailureType: "assertion",
		RootCause:   "a is not b in the test",
	}

	fp := ComputeFingerprint(rca)

	assert.NotContains(t, fp.ErrorTokens, "a")
	assert.NotContains(t, fp.ErrorTokens, "is")
	assert.Contains(t, fp.ErrorTokens, "not")
	assert.Contains(t, fp.ErrorTokens, "the")
	assert.Contains(t, fp.ErrorTokens, "test")
}
