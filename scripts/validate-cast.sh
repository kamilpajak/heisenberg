#!/bin/bash
# Validate an asciinema recording against the UX mockup.
# Usage: ./scripts/validate-cast.sh <file.cast>
set -euo pipefail

CAST_FILE="${1:?Usage: $0 <file.cast>}"

if [ ! -f "$CAST_FILE" ]; then
  echo "Error: $CAST_FILE not found" >&2
  exit 1
fi

CAST_FILE="$(cd "$(dirname "$CAST_FILE")" && pwd)/$(basename "$CAST_FILE")" \
  exec go test -v -run TestValidateDemoCast ./cmd/cli/...
