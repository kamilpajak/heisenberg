#!/bin/bash
# Compare two e2e snapshot directories on structural fields.
# Usage: ./scripts/e2e-compare.sh DIR_A DIR_B
#
# Compares: category, confidence, analyses count, failure types, bug locations.
# Skips: text field (non-deterministic prose).
# Normalizes analyses array order by file_path before comparison.
set -euo pipefail

if [ $# -ne 2 ]; then
  echo "Usage: $0 <snapshot_dir_a> <snapshot_dir_b>"
  echo "Example: $0 testdata/e2e/snapshots/2026-03-29_gemini-2.5-pro testdata/e2e/snapshots/2026-03-29_gemini-3-pro-preview"
  exit 1
fi

DIR_A="$1"
DIR_B="$2"

if [ ! -d "$DIR_A" ]; then echo "Error: $DIR_A not found"; exit 1; fi
if [ ! -d "$DIR_B" ]; then echo "Error: $DIR_B not found"; exit 1; fi

LABEL_A=$(basename "$DIR_A")
LABEL_B=$(basename "$DIR_B")

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
DIM='\033[2m'
NC='\033[0m'

# Extract structural fields (sorted analyses by file_path)
structural() {
  jq '{
    category,
    confidence,
    sensitivity,
    analyses_count: (.analyses | length),
    analyses: [.analyses | sort_by(.location.file_path // "") | .[] | {
      title,
      failure_type,
      bug_location,
      bug_location_confidence,
      file_path: (.location.file_path // null),
      line_number: (.location.line_number // null)
    }]
  }' "$1"
}

echo ""
printf "  %-45s  %s\n" "$LABEL_A" "$LABEL_B"
printf "  %s\n" "$(printf '%.0s─' {1..90})"

MATCH=0
DIFF=0

for FILE_A in "$DIR_A"/*.json; do
  BASENAME=$(basename "$FILE_A")
  FILE_B="$DIR_B/$BASENAME"

  if [ ! -f "$FILE_B" ]; then
    printf "  ${YELLOW}%-45s  MISSING in B${NC}\n" "$BASENAME"
    DIFF=$((DIFF + 1))
    continue
  fi

  STRUCT_A=$(structural "$FILE_A")
  STRUCT_B=$(structural "$FILE_B")

  # Compare top-level fields
  CAT_A=$(echo "$STRUCT_A" | jq -r '.category')
  CAT_B=$(echo "$STRUCT_B" | jq -r '.category')
  CONF_A=$(echo "$STRUCT_A" | jq -r '.confidence')
  CONF_B=$(echo "$STRUCT_B" | jq -r '.confidence')
  COUNT_A=$(echo "$STRUCT_A" | jq -r '.analyses_count')
  COUNT_B=$(echo "$STRUCT_B" | jq -r '.analyses_count')

  # Build comparison line
  SLUG="${BASENAME%.json}"
  if [ "$STRUCT_A" = "$STRUCT_B" ]; then
    printf "  ${GREEN}%-45s  MATCH${NC}  category=%s confidence=%s analyses=%s\n" \
      "$SLUG" "$CAT_A" "$CONF_A" "$COUNT_A"
    MATCH=$((MATCH + 1))
  else
    printf "  ${RED}%-45s  DIFF${NC}\n" "$SLUG"

    # Show per-field diff
    if [ "$CAT_A" != "$CAT_B" ]; then
      printf "    ${DIM}category:${NC}    ${RED}%s → %s${NC}\n" "$CAT_A" "$CAT_B"
    else
      printf "    ${DIM}category:${NC}    %s\n" "$CAT_A"
    fi

    if [ "$CONF_A" != "$CONF_B" ]; then
      DELTA=$((CONF_B - CONF_A))
      printf "    ${DIM}confidence:${NC}  ${YELLOW}%s → %s (Δ%+d)${NC}\n" "$CONF_A" "$CONF_B" "$DELTA"
    else
      printf "    ${DIM}confidence:${NC}  %s\n" "$CONF_A"
    fi

    if [ "$COUNT_A" != "$COUNT_B" ]; then
      printf "    ${DIM}analyses:${NC}    ${RED}%s → %s${NC}\n" "$COUNT_A" "$COUNT_B"
    else
      printf "    ${DIM}analyses:${NC}    %s\n" "$COUNT_A"
    fi

    # Per-RCA diff
    MAX=$((COUNT_A > COUNT_B ? COUNT_A : COUNT_B))
    for ((i=0; i<MAX; i++)); do
      TYPE_A=$(echo "$STRUCT_A" | jq -r ".analyses[$i].failure_type // \"—\"")
      TYPE_B=$(echo "$STRUCT_B" | jq -r ".analyses[$i].failure_type // \"—\"")
      BUG_A=$(echo "$STRUCT_A" | jq -r ".analyses[$i].bug_location // \"—\"")
      BUG_B=$(echo "$STRUCT_B" | jq -r ".analyses[$i].bug_location // \"—\"")
      TITLE_A=$(echo "$STRUCT_A" | jq -r ".analyses[$i].title // \"—\"" | head -c 40)
      TITLE_B=$(echo "$STRUCT_B" | jq -r ".analyses[$i].title // \"—\"" | head -c 40)

      if [ "$TYPE_A" = "$TYPE_B" ] && [ "$BUG_A" = "$BUG_B" ]; then
        printf "    ${DIM}[%d] %s (%s, %s)${NC}\n" "$((i+1))" "$TITLE_A" "$TYPE_A" "$BUG_A"
      else
        printf "    ${RED}[%d] A: %s (%s, %s)${NC}\n" "$((i+1))" "$TITLE_A" "$TYPE_A" "$BUG_A"
        printf "    ${RED}    B: %s (%s, %s)${NC}\n" "$TITLE_B" "$TYPE_B" "$BUG_B"
      fi
    done

    DIFF=$((DIFF + 1))
  fi
done

echo ""
echo "  Summary: $MATCH matched, $DIFF differed"
