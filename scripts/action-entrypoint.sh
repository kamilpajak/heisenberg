#!/bin/bash
set -uo pipefail

REPO="${INPUT_REPOSITORY:-$GITHUB_REPOSITORY}"
# Docker actions set INPUT_RUN-ID (with hyphen), bash can't reference that directly
RUN_ID=$(printenv 'INPUT_RUN-ID' 2>/dev/null || echo "")

CMD=("heisenberg" "$REPO" "--format" "json")
[[ -n "$RUN_ID" ]] && CMD+=("--run-id" "$RUN_ID")

# Run and capture JSON (allow non-zero exit — structured errors are still valid JSON)
OUTPUT=$("${CMD[@]}" 2>/dev/stderr) || true

if [[ -z "$OUTPUT" ]]; then
  echo "::error::Heisenberg produced no output. Check stderr above for details."
  exit 1
fi

# Check for error response (e.g., quota exceeded, config error)
ERROR_MSG=$(echo "$OUTPUT" | jq -r '.error // empty')
if [[ -n "$ERROR_MSG" ]]; then
  EXIT_CODE=$(echo "$OUTPUT" | jq -r '.exit_code // 1')
  echo "::error::$ERROR_MSG"
  {
    echo "## 🔬 Heisenberg Error"
    echo ""
    echo "$ERROR_MSG"
  } >> "$GITHUB_STEP_SUMMARY"
  exit "$EXIT_CODE"
fi

# Parse JSON
DIAGNOSIS=$(echo "$OUTPUT" | jq -r '.text')
CONFIDENCE=$(echo "$OUTPUT" | jq -r '.confidence')
CATEGORY=$(echo "$OUTPUT" | jq -r '.category')

# Set outputs
{
  echo "diagnosis<<EOF"
  echo "$DIAGNOSIS"
  echo "EOF"
  echo "confidence=$CONFIDENCE"
  echo "category=$CATEGORY"
} >> "$GITHUB_OUTPUT"

# Job Summary
{
  echo "## 🔬 Heisenberg Analysis"
  echo ""
  if [[ "$CATEGORY" == "diagnosis" ]]; then
    echo "**Confidence:** ${CONFIDENCE}%"
    echo ""
  fi
  echo "$DIAGNOSIS"
} >> "$GITHUB_STEP_SUMMARY"
