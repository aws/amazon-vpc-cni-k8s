#!/usr/bin/env bash

set -euo pipefail

if [[ "$#" -ne 4 ]]; then
  echo "usage: $0 IMAGE VERSION GIT_COMMIT BUILD_DATE" >&2
  exit 2
fi

image="$1"
expected_version="$2"
expected_commit="$3"
expected_build_date="$4"

work_dir="$(mktemp -d)"
container_id=""
inspect_container_id=""
cleanup() {
  if [[ -n "${container_id}" ]]; then
    docker rm --force "${container_id}" > /dev/null 2>&1 || true
  fi
  if [[ -n "${inspect_container_id}" ]]; then
    docker rm --force "${inspect_container_id}" > /dev/null 2>&1 || true
  fi
  rm -rf "${work_dir}"
}
trap cleanup EXIT

inspect_container_id="$(docker create "${image}")"
docker cp "${inspect_container_id}:/app/aws-k8s-agent" "${work_dir}/aws-k8s-agent"
docker rm "${inspect_container_id}" > /dev/null
inspect_container_id=""

expected_go_version="$(go version "${work_dir}/aws-k8s-agent" | awk '{print $2}')"
case "$(uname -m)" in
  x86_64)
    expected_arch="amd64"
    ;;
  aarch64 | arm64)
    expected_arch="arm64"
    ;;
  *)
    echo "unsupported build host architecture: $(uname -m)" >&2
    exit 1
    ;;
esac
expected_platform="linux/${expected_arch}"

mkdir -p "${work_dir}/aws-routed-eni"
container_id="$(docker run --detach \
  --network none \
  --volume "${work_dir}/aws-routed-eni:/host/var/log/aws-routed-eni" \
  --entrypoint /app/aws-k8s-agent \
  "${image}")"

metadata_file="${work_dir}/aws-routed-eni/aws-vpc-cni-metadata.json"
for _ in $(seq 1 100); do
  if [[ -s "${metadata_file}" ]]; then
    break
  fi
  sleep 0.1
done

if [[ ! -s "${metadata_file}" ]]; then
  docker logs "${container_id}" >&2 || true
  echo "AWS VPC CNI did not publish metadata" >&2
  exit 1
fi

python3 - "${metadata_file}" \
  "${expected_version}" \
  "${expected_commit}" \
  "${expected_build_date}" \
  "${expected_go_version}" \
  "${expected_platform}" <<'PY'
import datetime
import json
import sys

path, version, commit, build_date, go_version, platform = sys.argv[1:]
with open(path, "rb") as metadata_file:
    raw_metadata = metadata_file.read()
if not raw_metadata.endswith(b"\n"):
    raise SystemExit("metadata file is missing its trailing newline")
metadata = json.loads(raw_metadata)

expected = {
    "schemaVersion": 1,
    "component": "aws-vpc-cni",
    "version": version,
    "gitCommit": commit,
    "buildDate": build_date,
    "goVersion": go_version,
    "platform": platform,
}
for field, value in expected.items():
    if metadata.get(field) != value:
        raise SystemExit(
            f"{field} mismatch: got {metadata.get(field)!r}, expected {value!r}"
        )
generated_at = metadata.get("generatedAt")
if not isinstance(generated_at, str) or not generated_at.endswith("Z"):
    raise SystemExit(f"generatedAt is not UTC RFC3339: {generated_at!r}")
datetime.datetime.fromisoformat(generated_at.removesuffix("Z") + "+00:00")
PY
