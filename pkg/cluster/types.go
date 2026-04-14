// Package cluster provides failure grouping for CI workflow runs.
// It extracts error signatures from job logs and clusters similar failures
// using exact match and Jaccard similarity — pure Go, no LLM dependency.
package cluster

// ErrorSignature is a normalized fingerprint of a failure.
type ErrorSignature struct {
	Category   string   // "exit_code", "stack_trace", "error_message", "fallback"
	Normalized string   // normalized error string for comparison
	RawExcerpt string   // original text excerpt (for LLM context)
	Tokens     []string // tokenized form for Jaccard similarity
	TopFrames  []string // up to 3 distinct stack frame locations (stack_trace only)
}

// FailureInfo holds extracted failure data for one job.
type FailureInfo struct {
	JobID      int64
	JobName    string
	Conclusion string
	Signature  ErrorSignature
	LogTail    string // last N bytes of log, for LLM context
}

// Cluster groups jobs that share a common failure pattern.
type Cluster struct {
	ID             int
	Signature      ErrorSignature
	Failures       []FailureInfo
	Representative FailureInfo // most detailed failure (longest log)
}

// Result is the output of the clustering phase.
type Result struct {
	Clusters    []Cluster
	Unclustered []FailureInfo // failures that couldn't be fingerprinted
	TotalFailed int
	Method      string // "exact", "jaccard", "single" (fallback)
}
