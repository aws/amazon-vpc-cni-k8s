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

cni_write_workload_state() {
  : "${CNI_WORKLOAD_STATE_FILE:?CNI_WORKLOAD_STATE_FILE must be set}"

  local state_dir
  state_dir=$(dirname -- "$CNI_WORKLOAD_STATE_FILE")
  mkdir -p "$state_dir"

  local temporary="${CNI_WORKLOAD_STATE_FILE}.tmp.$$"
  {
    printf 'WORKLOAD_PROFILE_ID=%q\n' "${WORKLOAD_PROFILE_ID:-}"
    printf 'WORKLOAD_GIT_REVISION=%q\n' "${WORKLOAD_GIT_REVISION:-}"
    printf 'TEST_SCENARIO_ID=%q\n' "${TEST_SCENARIO_ID:-}"
    printf 'WORKLOAD_REPORT_PATH=%q\n' "${WORKLOAD_REPORT_PATH:-}"
    printf 'WORKLOAD_STARTED_AT=%q\n' "${WORKLOAD_STARTED_AT:-}"
    printf 'WORKLOAD_COMPLETED_AT=%q\n' "${WORKLOAD_COMPLETED_AT:-}"
    printf 'WORKLOAD_REQUESTED_PODS=%q\n' "${WORKLOAD_REQUESTED_PODS:-0}"
    printf 'WORKLOAD_READY_PODS=%q\n' "${WORKLOAD_READY_PODS:-0}"
    printf 'WORKLOAD_CREATED_PODS=%q\n' "${WORKLOAD_CREATED_PODS:-0}"
    printf 'WORKLOAD_DELETED_PODS=%q\n' "${WORKLOAD_DELETED_PODS:-0}"
    printf 'WORKLOAD_POD_THROUGHPUT=%q\n' "${WORKLOAD_POD_THROUGHPUT:-0}"
    printf 'WORKLOAD_CHURN_PODS_PER_ROUND=%q\n' "${WORKLOAD_CHURN_PODS_PER_ROUND:-0}"
    printf 'WORKLOAD_CHURN_INTERVAL_SECONDS=%q\n' "${WORKLOAD_CHURN_INTERVAL_SECONDS:-0}"
    printf 'WORKLOAD_DURATION_SECONDS=%q\n' "${WORKLOAD_DURATION_SECONDS:-0}"
    printf 'WORKLOAD_ROUNDS_REQUESTED=%q\n' "${WORKLOAD_ROUNDS_REQUESTED:-0}"
    printf 'WORKLOAD_ROUNDS_COMPLETED=%q\n' "${WORKLOAD_ROUNDS_COMPLETED:-0}"
    printf 'WORKLOAD_CONNECTIVITY_STATUS=%q\n' "${WORKLOAD_CONNECTIVITY_STATUS:-unknown}"
    printf 'WORKLOAD_UNIQUE_IP_STATUS=%q\n' "${WORKLOAD_UNIQUE_IP_STATUS:-unknown}"
    printf 'WORKLOAD_STATUS=%q\n' "${WORKLOAD_STATUS:-unknown}"
    printf 'WORKLOAD_CLEANUP_STATUS=%q\n' "${WORKLOAD_CLEANUP_STATUS:-pending}"
    printf 'SCALE_TEST_NAMESPACE_PREFIX=%q\n' "${SCALE_TEST_NAMESPACE_PREFIX:-cni-scale}"
  } >"$temporary"
  mv -f -- "$temporary" "$CNI_WORKLOAD_STATE_FILE"
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
  "workloadRevision": "$(cni_json_escape "${WORKLOAD_GIT_REVISION:-}")",
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
