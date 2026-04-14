package cluster

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractSignature_TopFrames_Go(t *testing.T) {
	log := `2026-03-29T10:00:00.1234567Z goroutine 1 [running]:
2026-03-29T10:00:00.1234567Z main.init.func1()
2026-03-29T10:00:00.1234567Z 	/home/runner/work/proj/main.go:42 +0x1a4
2026-03-29T10:00:00.1234567Z main.main()
2026-03-29T10:00:00.1234567Z 	/home/runner/work/proj/main.go:10 +0x20
2026-03-29T10:00:00.1234567Z 	/home/runner/work/proj/config.go:88 +0x10`

	sig := ExtractSignature(log)
	require.Equal(t, "stack_trace", sig.Category)
	require.NotEmpty(t, sig.TopFrames)
	assert.LessOrEqual(t, len(sig.TopFrames), 3, "cap at 3 frames")
	// First frame (crash site) must always be present.
	assert.Contains(t, sig.TopFrames[0], "main.go:42")
}

func TestExtractSignature_TopFrames_Python(t *testing.T) {
	log := `2026-03-29T10:00:00.1234567Z Traceback (most recent call last):
2026-03-29T10:00:00.1234567Z   File "/app/tests/helpers.py", line 10, in setup
2026-03-29T10:00:00.1234567Z   File "/app/tests/test_login.py", line 42, in test_login
2026-03-29T10:00:00.1234567Z   File "/app/tests/shared.py", line 88, in assert_status
2026-03-29T10:00:00.1234567Z AssertionError: assert 500 == 200`

	sig := ExtractSignature(log)
	require.Equal(t, "stack_trace", sig.Category)
	require.NotEmpty(t, sig.TopFrames)
	// TopFrames should contain distinct filenames (up to 3).
	seen := map[string]bool{}
	for _, f := range sig.TopFrames {
		seen[f] = true
	}
	assert.Equal(t, len(sig.TopFrames), len(seen), "frames must be distinct")
}

func TestExtractSignature_TopFrames_EmptyForNonStackTrace(t *testing.T) {
	log := `Process completed with exit code 2.`
	sig := ExtractSignature(log)
	assert.Empty(t, sig.TopFrames, "non-stack-trace signatures have no frames")
}

func TestExtractSignature_ExitCode(t *testing.T) {
	log := `2026-03-29T10:00:00.1234567Z Running tests...
2026-03-29T10:00:05.1234567Z FAIL
2026-03-29T10:00:05.1234567Z Process completed with exit code 2.`

	sig := ExtractSignature(log)
	assert.Equal(t, "exit_code", sig.Category)
	assert.Contains(t, sig.Normalized, "exit code")
	assert.Contains(t, sig.RawExcerpt, "exit code 2")
}

func TestExtractSignature_GoStackTrace(t *testing.T) {
	log := `2026-03-29T10:00:00.1234567Z goroutine 1 [running]:
2026-03-29T10:00:00.1234567Z main.init.func1()
2026-03-29T10:00:00.1234567Z 	/home/runner/work/proj/main.go:42 +0x1a4
2026-03-29T10:00:00.1234567Z main.main()
2026-03-29T10:00:00.1234567Z 	/home/runner/work/proj/main.go:10 +0x20`

	sig := ExtractSignature(log)
	assert.Equal(t, "stack_trace", sig.Category)
	assert.Contains(t, sig.Normalized, "main.go")
	assert.Contains(t, sig.RawExcerpt, "main.go:42")
}

func TestExtractSignature_JSStackTrace(t *testing.T) {
	log := `2026-03-29T10:00:00.1234567Z Error: Timeout waiting for selector
2026-03-29T10:00:00.1234567Z     at Object.waitForSelector (/app/node_modules/playwright/lib/helper.js:123:15)
2026-03-29T10:00:00.1234567Z     at Context.<anonymous> (tests/checkout.spec.ts:45:22)`

	sig := ExtractSignature(log)
	assert.Equal(t, "stack_trace", sig.Category)
	assert.Contains(t, sig.RawExcerpt, "checkout.spec.ts:45")
}

func TestExtractSignature_PythonStackTrace(t *testing.T) {
	log := `2026-03-29T10:00:00.1234567Z Traceback (most recent call last):
2026-03-29T10:00:00.1234567Z   File "/app/tests/test_login.py", line 42, in test_login
2026-03-29T10:00:00.1234567Z     assert response.status_code == 200
2026-03-29T10:00:00.1234567Z AssertionError: assert 500 == 200`

	sig := ExtractSignature(log)
	assert.Equal(t, "stack_trace", sig.Category)
	assert.Contains(t, sig.RawExcerpt, "test_login.py")
}

func TestExtractSignature_JavaStackTrace(t *testing.T) {
	log := `2026-03-29T10:00:00.1234567Z java.lang.NullPointerException
2026-03-29T10:00:00.1234567Z 	at com.example.Service.process(Service.java:42)
2026-03-29T10:00:00.1234567Z 	at com.example.Controller.handle(Controller.java:15)`

	sig := ExtractSignature(log)
	assert.Equal(t, "stack_trace", sig.Category)
	assert.Contains(t, sig.Normalized, "service.java")
	assert.Contains(t, sig.RawExcerpt, "Service.java:42")
}

func TestExtractSignature_ErrorMessage(t *testing.T) {
	log := `2026-03-29T10:00:00.1234567Z npm run test
2026-03-29T10:00:05.1234567Z Error: expect(received).toContain(expected)
2026-03-29T10:00:05.1234567Z Expected substring: "utilsBundle"
2026-03-29T10:00:05.1234567Z Received string: "..."`

	sig := ExtractSignature(log)
	assert.Equal(t, "error_message", sig.Category)
	assert.Contains(t, sig.Normalized, "expect(received).tocontain(expected)")
}

func TestExtractSignature_ConnectionRefused(t *testing.T) {
	log := `2026-03-29T10:00:00.1234567Z page.goto: net::ERR_CONNECTION_REFUSED at http://localhost:7745/home`

	sig := ExtractSignature(log)
	assert.Equal(t, "error_message", sig.Category)
	assert.Contains(t, sig.Normalized, "err_connection_refused")
}

func TestExtractSignature_Fallback(t *testing.T) {
	log := `2026-03-29T10:00:00.1234567Z Step 1: checkout
2026-03-29T10:00:01.1234567Z Step 2: build
2026-03-29T10:00:02.1234567Z something failed here
2026-03-29T10:00:03.1234567Z Step 3: done`

	sig := ExtractSignature(log)
	assert.Equal(t, "fallback", sig.Category)
	assert.Contains(t, sig.Normalized, "failed")
}

func TestExtractSignature_Empty(t *testing.T) {
	sig := ExtractSignature("")
	assert.Equal(t, "", sig.Category)
	assert.Empty(t, sig.Normalized)
}

func TestExtractSignature_TimestampStripping(t *testing.T) {
	// Ensure GitHub Actions timestamps don't interfere with extraction
	log := "2026-03-29T10:00:00.1234567Z Error: something broke"
	sig := ExtractSignature(log)
	require.NotEmpty(t, sig.Category)
	assert.NotContains(t, sig.Normalized, "2026")
}

func TestNormalize(t *testing.T) {
	tests := []struct {
		input string
		notIn string // should NOT contain after normalization
	}{
		{"\x1b[31mError\x1b[0m", "\x1b"},                        // ANSI
		{"at 0x7fff5fbff8c0", "0x7fff"},                         // hex
		{"id=550e8400-e29b-41d4-a716-446655440000", "550e8400"}, // UUID
		{"expected 42 but got 43", "42"},                        // numbers
		{"  too   many   spaces  ", "   "},                      // whitespace
	}

	for _, tt := range tests {
		name := tt.input
		if len(name) > 20 {
			name = name[:20]
		}
		t.Run(name, func(t *testing.T) {
			result := normalize(tt.input)
			assert.NotContains(t, result, tt.notIn)
		})
	}
}

func TestTokenize(t *testing.T) {
	tokens := tokenize("error: connection refused at localhost:3000")
	assert.Contains(t, tokens, "error")
	assert.Contains(t, tokens, "connection")
	assert.Contains(t, tokens, "refused")
	// Short tokens filtered
	assert.NotContains(t, tokens, "at")
}

func TestExtractSignature_RustPanic(t *testing.T) {
	log := `2026-03-29T10:00:00.1234567Z thread 'main' panicked at src/parser.rs:42:5
2026-03-29T10:00:00.1234567Z note: run with RUST_BACKTRACE=1`

	sig := ExtractSignature(log)
	assert.Equal(t, "stack_trace", sig.Category)
	assert.Contains(t, sig.RawExcerpt, "parser.rs:42")
}

func TestExtractSignature_StackTracePrioritizedOverExitCode(t *testing.T) {
	// GitHub Actions appends "exit code 1" to almost every failing job.
	// Stack trace is more informative and should win.
	log := `2026-03-29T10:00:00.1234567Z goroutine 1 [running]:
2026-03-29T10:00:00.1234567Z main.handler()
2026-03-29T10:00:00.1234567Z 	/app/server.go:42 +0x1a4
2026-03-29T10:00:01.1234567Z Process completed with exit code 1.`

	sig := ExtractSignature(log)
	assert.Equal(t, "stack_trace", sig.Category, "stack trace should be prioritized over generic exit code")
	assert.Contains(t, sig.RawExcerpt, "server.go:42")
}

func TestExtractSignature_ExitCodeOnlyWhenNoStackTrace(t *testing.T) {
	// When there's no stack trace or error message, exit code is fine
	log := `2026-03-29T10:00:00.1234567Z Running build...
2026-03-29T10:00:01.1234567Z Process completed with exit code 2.`

	sig := ExtractSignature(log)
	assert.Equal(t, "exit_code", sig.Category)
}

func TestExtractSignature_LongLog_UsesLastPortion(t *testing.T) {
	// Signature extraction should focus on the end of the log
	// (where errors typically appear)
	filler := strings.Repeat("2026-03-29T10:00:00.1234567Z normal log line\n", 10000)
	log := filler + "2026-03-29T10:05:00.1234567Z Error: the actual failure message\n"

	sig := ExtractSignature(log)
	assert.NotEmpty(t, sig.Category)
	assert.Contains(t, sig.Normalized, "actual failure message")
}
