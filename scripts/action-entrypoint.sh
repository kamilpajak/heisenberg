#!/bin/bash
set -euo pipefail

REPO="${INPUT_REPOSITORY:-$GITHUB_REPOSITORY}"
RUN_ID="${INPUT_RUN_ID:-}"

CMD=("heisenberg" "$REPO" "--json")
[[ -n "$RUN_ID" ]] && CMD+=("--run-id" "$RUN_ID")

# Run and capture JSON
OUTPUT=$("${CMD[@]}" 2>/dev/stderr)

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
