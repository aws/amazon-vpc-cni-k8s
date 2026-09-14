#!/usr/bin/env bash

set -euo pipefail

cni_json_escape() {
  local value=$1
  value=${value//\\/\\\\}
  value=${value//\"/\\\"}
  value=${value//$'\n'/\\n}
  value=${value//$'\r'/\\r}
  value=${value//$'\t'/\\t}
  printf '%s' "$value"
}

cni_write_workload_report() {
  : "${WORKLOAD_REPORT_PATH:?WORKLOAD_REPORT_PATH must be set}"
  : "${WORKLOAD_PROFILE_ID:?WORKLOAD_PROFILE_ID must be set}"

  local report_dir
  report_dir=$(dirname -- "$WORKLOAD_REPORT_PATH")
  mkdir -p "$report_dir"

  local temporary="${WORKLOAD_REPORT_PATH}.tmp.$$"
  cat >"$temporary" <<EOF
{
  "schemaVersion": 1,
  "profileID": "$(cni_json_escape "$WORKLOAD_PROFILE_ID")",
  "scenarioID": "$(cni_json_escape "${TEST_SCENARIO_ID:-}")",
  "startedAt": "$(cni_json_escape "${WORKLOAD_STARTED_AT:-}")",
  "completedAt": "$(cni_json_escape "${WORKLOAD_COMPLETED_AT:-}")",
  "requestedPods": ${WORKLOAD_REQUESTED_PODS:-0},
  "readyPods": ${WORKLOAD_READY_PODS:-0},
  "createdPods": ${WORKLOAD_CREATED_PODS:-0},
  "deletedPods": ${WORKLOAD_DELETED_PODS:-0},
  "podThroughput": ${WORKLOAD_POD_THROUGHPUT:-0},
  "churnPodsPerRound": ${WORKLOAD_CHURN_PODS_PER_ROUND:-0},
  "churnIntervalSeconds": ${WORKLOAD_CHURN_INTERVAL_SECONDS:-0},
  "durationSeconds": ${WORKLOAD_DURATION_SECONDS:-0},
  "roundsRequested": ${WORKLOAD_ROUNDS_REQUESTED:-0},
  "roundsCompleted": ${WORKLOAD_ROUNDS_COMPLETED:-0},
  "functionalChecks": {
    "connectivity": "$(cni_json_escape "${WORKLOAD_CONNECTIVITY_STATUS:-unknown}")",
    "uniquePodIPs": "$(cni_json_escape "${WORKLOAD_UNIQUE_IP_STATUS:-unknown}")"
  },
  "workloadStatus": "$(cni_json_escape "${WORKLOAD_STATUS:-unknown}")",
  "cleanupStatus": "$(cni_json_escape "${WORKLOAD_CLEANUP_STATUS:-unknown}")"
}
EOF
  mv -f -- "$temporary" "$WORKLOAD_REPORT_PATH"
}
