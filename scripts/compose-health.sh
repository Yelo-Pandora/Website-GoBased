#!/bin/bash
# Verifies the public scaffold endpoints after Compose startup.

set -euo pipefail

readonly HTTP_PORT="${HTTP_PORT:-8080}"
readonly BASE_URL="http://127.0.0.1:${HTTP_PORT}"

main() {
  curl --fail --silent --show-error "${BASE_URL}/healthz" >/dev/null
  curl --fail --silent --show-error \
    "${BASE_URL}/api/v1/system/info" >/dev/null
  docker compose ps
}

main "$@"
