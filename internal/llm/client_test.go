package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPreviewExcerpt_Short(t *testing.T) {
	assert.Equal(t, "short text", previewExcerpt("short text", 200))
}

func TestPreviewExcerpt_FindsErrorKeyword(t *testing.T) {
	long := "lots of setup noise " + repeat("x", 200) + " Error: something broke badly " + repeat("y", 100)
	result := previewExcerpt(long, 80)
	assert.Contains(t, result, "Error: something broke badly")
	assert.True(t, len(result) <= 90, "excerpt should be bounded") // 80 + "..." prefix/suffix
}

func TestPreviewExcerpt_FindsFAIL(t *testing.T) {
	long := repeat("a", 300) + "FAIL tests/login.spec.ts" + repeat("b", 100)
	result := previewExcerpt(long, 80)
	assert.Contains(t, result, "FAIL")
}

func TestPreviewExcerpt_FallsBackToTail(t *testing.T) {
	long := repeat("a", 100) + "useful tail content"
	result := previewExcerpt(long, 30)
	assert.True(t, len(result) <= 40)
	assert.Contains(t, result, "useful tail content")
}

func TestPreviewExcerpt_StripsANSI(t *testing.T) {
	ansi := "before \x1b[31mred text\x1b[0m after"
	result := previewExcerpt(ansi, 200)
	assert.NotContains(t, result, "\x1b")
	assert.Contains(t, result, "red text")
}

func TestPreviewExcerpt_KeywordPriority(t *testing.T) {
	// "FAIL" should be found before generic "error"
	long := repeat("x", 100) + "##[error]Process completed" + repeat("y", 100) + "FAIL test.spec.ts" + repeat("z", 100)
	result := previewExcerpt(long, 80)
	assert.Contains(t, result, "FAIL test.spec.ts")
}

func TestIsEmptyResponse_Nil(t *testing.T) {
	assert.True(t, isEmptyResponse(nil))
}

func TestIsEmptyResponse_EmptyParts(t *testing.T) {
	c := &Candidate{Content: Content{Parts: []Part{}}}
	assert.True(t, isEmptyResponse(c))
}

func TestIsEmptyResponse_WithText(t *testing.T) {
	c := &Candidate{Content: Content{Parts: []Part{{Text: "hello"}}}}
	assert.False(t, isEmptyResponse(c))
}

func TestIsEmptyResponse_WithFunctionCall(t *testing.T) {
	c := &Candidate{Content: Content{Parts: []Part{{FunctionCall: &FunctionCall{Name: "done"}}}}}
	assert.False(t, isEmptyResponse(c))
}

func TestIsEmptyResponse_OnlyEmptyParts(t *testing.T) {
	c := &Candidate{Content: Content{Parts: []Part{{}, {}}}}
	assert.True(t, isEmptyResponse(c))
}

func TestDescribeEmptyResponse_Nil(t *testing.T) {
	assert.Equal(t, "no candidate", describeEmptyResponse(nil))
}

func TestDescribeEmptyResponse_WithFinishReason(t *testing.T) {
	c := &Candidate{FinishReason: "STOP"}
	assert.Equal(t, "finishReason=STOP", describeEmptyResponse(c))
}

func TestDescribeEmptyResponse_WithBlockedSafety(t *testing.T) {
	c := &Candidate{
		FinishReason: "SAFETY",
		SafetyRatings: []SafetyRating{
			{Category: "HARM_CATEGORY_DANGEROUS", Probability: "HIGH", Blocked: true},
			{Category: "HARM_CATEGORY_HARASSMENT", Probability: "NEGLIGIBLE"},
		},
	}
	result := describeEmptyResponse(c)
	assert.Contains(t, result, "finishReason=SAFETY")
	assert.Contains(t, result, "HARM_CATEGORY_DANGEROUS=HIGH (blocked)")
	assert.NotContains(t, result, "HARASSMENT")
}

// repeat creates a string of n copies of s.
func repeat(s string, n int) string {
	result := ""
	for range n {
		result += s
	}
	return result
}
