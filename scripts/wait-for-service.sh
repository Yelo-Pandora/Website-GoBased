#!/bin/bash
# Waits for an HTTP endpoint to return success.

set -euo pipefail

readonly URL="${1:-}"
readonly MAX_ATTEMPTS="${2:-30}"

main() {
  if [[ -z "${URL}" ]]; then
    echo "usage: wait-for-service.sh URL [MAX_ATTEMPTS]" >&2
    return 2
  fi

  local attempt
  for ((attempt = 1; attempt <= MAX_ATTEMPTS; attempt++)); do
    if curl --fail --silent --show-error "${URL}" >/dev/null; then
      return 0
    fi
    sleep 1
  done

  echo "service did not become ready: ${URL}" >&2
  return 1
}

main "$@"
