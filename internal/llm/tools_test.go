package llm

import (
	"context"
	"testing"
)

func TestDoneDiagnosis(t *testing.T) {
	h := &ToolHandler{}
	_, isDone, err := h.Execute(context.Background(), FunctionCall{
		Name: "done",
		Args: map[string]any{
			"category":                        "diagnosis",
			"confidence":                      float64(85),
			"missing_information_sensitivity": "low",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !isDone {
		t.Fatal("expected done=true")
	}
	if h.DiagnosisCategory() != CategoryDiagnosis {
		t.Errorf("category = %q, want %q", h.DiagnosisCategory(), CategoryDiagnosis)
	}
	if h.DiagnosisConfidence() != 85 {
		t.Errorf("confidence = %d, want 85", h.DiagnosisConfidence())
	}
	if h.DiagnosisSensitivity() != "low" {
		t.Errorf("sensitivity = %q, want %q", h.DiagnosisSensitivity(), "low")
	}
}

func TestDoneNoFailures(t *testing.T) {
	h := &ToolHandler{}
	_, isDone, err := h.Execute(context.Background(), FunctionCall{
		Name: "done",
		Args: map[string]any{
			"category": "no_failures",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !isDone {
		t.Fatal("expected done=true")
	}
	if h.DiagnosisCategory() != CategoryNoFailures {
		t.Errorf("category = %q, want %q", h.DiagnosisCategory(), CategoryNoFailures)
	}
}

func TestDoneNotSupported(t *testing.T) {
	h := &ToolHandler{}
	_, isDone, err := h.Execute(context.Background(), FunctionCall{
		Name: "done",
		Args: map[string]any{
			"category": "not_supported",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !isDone {
		t.Fatal("expected done=true")
	}
	if h.DiagnosisCategory() != CategoryNotSupported {
		t.Errorf("category = %q, want %q", h.DiagnosisCategory(), CategoryNotSupported)
	}
}

func TestDoneWithNoArgs(t *testing.T) {
	h := &ToolHandler{}
	_, isDone, err := h.Execute(context.Background(), FunctionCall{
		Name: "done",
		Args: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !isDone {
		t.Fatal("expected done=true")
	}
	if h.DiagnosisCategory() != CategoryDiagnosis {
		t.Errorf("category = %q, want %q", h.DiagnosisCategory(), CategoryDiagnosis)
	}
	if h.DiagnosisConfidence() != 50 {
		t.Errorf("confidence = %d, want 50", h.DiagnosisConfidence())
	}
	if h.DiagnosisSensitivity() != "medium" {
		t.Errorf("sensitivity = %q, want %q", h.DiagnosisSensitivity(), "medium")
	}
}

func TestDoneWithFloat64Confidence(t *testing.T) {
	h := &ToolHandler{}
	_, _, err := h.Execute(context.Background(), FunctionCall{
		Name: "done",
		Args: map[string]any{
			"category":                        "diagnosis",
			"confidence":                      float64(72.8),
			"missing_information_sensitivity": "high",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.DiagnosisConfidence() != 72 {
		t.Errorf("confidence = %d, want 72", h.DiagnosisConfidence())
	}
}

func TestDoneWithInvalidSensitivity(t *testing.T) {
	h := &ToolHandler{}
	_, _, err := h.Execute(context.Background(), FunctionCall{
		Name: "done",
		Args: map[string]any{
			"category":                        "diagnosis",
			"confidence":                      float64(60),
			"missing_information_sensitivity": "invalid",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.DiagnosisSensitivity() != "medium" {
		t.Errorf("sensitivity = %q, want %q", h.DiagnosisSensitivity(), "medium")
	}
}

func TestDoneWithInvalidCategory(t *testing.T) {
	h := &ToolHandler{}
	_, _, err := h.Execute(context.Background(), FunctionCall{
		Name: "done",
		Args: map[string]any{
			"category": "unknown_value",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.DiagnosisCategory() != CategoryDiagnosis {
		t.Errorf("category = %q, want %q (fallback)", h.DiagnosisCategory(), CategoryDiagnosis)
	}
}

func TestDoneSkippedCategory(t *testing.T) {
	h := &ToolHandler{}
	// Simulate model skipping done entirely — category stays ""
	if h.DiagnosisCategory() != "" {
		t.Errorf("category = %q, want empty string", h.DiagnosisCategory())
	}
}
