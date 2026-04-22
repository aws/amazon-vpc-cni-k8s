#!/bin/bash
# Verify all .go files have the Amazon copyright header.
set -euo pipefail

MISSING=()
for f in $(find ./cmd ./pkg ./test ./scripts -name "*.go" 2>/dev/null); do
  if ! grep -q "Copyright Amazon.com Inc. or its affiliates" "$f"; then
    MISSING+=("$f")
  fi
done

if [ ${#MISSING[@]} -ne 0 ]; then
  echo "ERROR: The following files are missing the Amazon copyright header:"
  printf '  %s\n' "${MISSING[@]}"
  exit 1
fi

echo "All .go files have the copyright header."
