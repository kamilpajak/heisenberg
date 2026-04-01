#!/bin/bash
# Run heisenberg against known test repos and save results as snapshots.
# Usage:
#   ./scripts/e2e-snapshot.sh                        # uses default model
#   HEISENBERG_MODEL=gemini-2.5-pro ./scripts/e2e-snapshot.sh
#
# Artifacts expire after 90 days. When runs below stop working,
# find replacements:
#   gh run list --repo OWNER/REPO --status failure --limit 3
set -euo pipefail

MODEL=${HEISENBERG_MODEL:-gemini-3-pro-preview}
DATE=$(date +%Y-%m-%d)
DIR="testdata/e2e/snapshots/${DATE}_${MODEL}"
mkdir -p "$DIR"

# Test repos: "owner/repo:run_id"
REPOS=(
  "microsoft/playwright:23642131867"
  "sysadminsmedia/homebox:23672419835"
)

BINARY="./heisenberg"
if [[ ! -f "$BINARY" ]]; then
  echo "Building heisenberg..."
  go build -o "$BINARY" ./cmd/cli/
fi

PASS=0
FAIL=0

for entry in "${REPOS[@]}"; do
  REPO="${entry%%:*}"
  RUN_ID="${entry##*:}"
  SLUG="${REPO//\//_}_${RUN_ID}"
  echo -n "  $REPO #$RUN_ID ... "

  if "$BINARY" analyze --format json --model "$MODEL" "$REPO" --run-id "$RUN_ID" \
    2>"$DIR/${SLUG}.stderr" > "$DIR/${SLUG}.json"; then
    CATEGORY=$(jq -r '.category' "$DIR/${SLUG}.json")
    COUNT=$(jq '.analyses | length' "$DIR/${SLUG}.json")
    echo "OK  category=$CATEGORY analyses=$COUNT"
    PASS=$((PASS + 1))
  else
    echo "FAIL  (exit $?)"
    FAIL=$((FAIL + 1))
  fi
done

echo ""
echo "Snapshots saved to: $DIR"
echo "Results: $PASS passed, $FAIL failed"
