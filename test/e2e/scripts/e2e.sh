#!/bin/bash

set -ux
IFS=$'\n\t'

TEST="${1:-cni}"
NO_EXIT="${2:-true}"

OUT_PATH="/tmp/cni-e2e"
LOG_PATH="$OUT_PATH/plugins/e2e/results"

mkdir -p "$LOG_PATH"

ginkgo --noColor -v "$TEST" | tee "$LOG_PATH/e2e.log"

tar -C "$OUT_PATH" -cvzf "$OUT_PATH/results.tar.gz" plugins/

if [ "$NO_EXIT" == true ]; then
    echo 'no-exit was specified, cni-e2e is now blocking'
    trap : TERM INT; sleep infinity & wait
fi


