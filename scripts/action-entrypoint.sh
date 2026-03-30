#!/bin/bash
set -uo pipefail

REPO="${INPUT_REPOSITORY:-$GITHUB_REPOSITORY}"
RUN_ID="${INPUT_RUN_ID:-}"

CMD=("heisenberg" "$REPO" "--format" "json")
[[ -n "$RUN_ID" ]] && CMD+=("--run-id" "$RUN_ID")

# Run and capture JSON (allow non-zero exit — structured errors are still valid JSON)
OUTPUT=$("${CMD[@]}" 2>/dev/stderr) || true

if [[ -z "$OUTPUT" ]]; then
  echo "::error::Heisenberg produced no output. Check stderr above for details."
  exit 1
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
