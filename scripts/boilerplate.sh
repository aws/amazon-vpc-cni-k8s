#!/bin/bash
# Prepend Amazon copyright header to .go files missing it.
set -euo pipefail

for f in $(find ./cmd ./pkg ./test ./scripts -name "*.go" 2>/dev/null); do
  if ! grep -q "Copyright Amazon.com Inc. or its affiliates" "$f"; then
    cat scripts/boilerplate.go.txt "$f" > "$f.new" && mv "$f.new" "$f"
  fi
done
