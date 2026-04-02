package api

import (
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestParseRepoID(t *testing.T) {
	t.Run("valid UUID", func(t *testing.T) {
		expected := uuid.New()
		req := httptest.NewRequest("GET", "/repos/"+expected.String(), nil)
		req.SetPathValue("repoID", expected.String())

		got, err := parseRepoID(req)

		assert.NoError(t, err)
		assert.Equal(t, expected, got)
	})

	t.Run("invalid UUID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/repos/not-a-uuid", nil)
		req.SetPathValue("repoID", "not-a-uuid")

		_, err := parseRepoID(req)

		assert.Error(t, err)
	})

	t.Run("empty value", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/repos/", nil)
		req.SetPathValue("repoID", "")

		_, err := parseRepoID(req)

		assert.Error(t, err)
	})
}

func TestParseAnalysisID(t *testing.T) {
	t.Run("valid UUID", func(t *testing.T) {
		expected := uuid.New()
		req := httptest.NewRequest("GET", "/analyses/"+expected.String(), nil)
		req.SetPathValue("analysisID", expected.String())

		got, err := parseAnalysisID(req)

		assert.NoError(t, err)
		assert.Equal(t, expected, got)
	})

	t.Run("invalid UUID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/analyses/invalid", nil)
		req.SetPathValue("analysisID", "invalid")

		_, err := parseAnalysisID(req)

		assert.Error(t, err)
	})
}

func TestOrgContext_Fields(t *testing.T) {
	// Test that orgContext struct works correctly
	oc := orgContext{
		User:   nil,
		OrgID:  uuid.New(),
		Member: nil,
	}

	assert.NotEqual(t, uuid.Nil, oc.OrgID)
	assert.Nil(t, oc.User)
	assert.Nil(t, oc.Member)
}
