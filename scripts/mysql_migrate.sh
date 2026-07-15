#!/bin/bash
# Applies ordered platform database migrations exactly once.

set -euo pipefail

readonly MIGRATION_DIR="/opt/platform/migrations"
readonly EXPECTED_DATABASE="platform"

mysql_command() {
  MYSQL_PWD="${MYSQL_ROOT_PASSWORD}" mysql \
    --batch \
    --default-character-set=utf8mb4 \
    --host="${MYSQL_HOST}" \
    --port="${MYSQL_PORT}" \
    --raw \
    --skip-column-names \
    --user=root \
    "$@"
}

validate_environment() {
  local variable
  for variable in \
    MYSQL_HOST \
    MYSQL_PORT \
    MYSQL_PLATFORM_DATABASE \
    MYSQL_ROOT_PASSWORD; do
    if [[ -z "${!variable:-}" ]]; then
      echo "Required environment variable ${variable} is empty" >&2
      return 1
    fi
  done

  if [[ "${MYSQL_PLATFORM_DATABASE}" != "${EXPECTED_DATABASE}" ]]; then
    echo "MYSQL_PLATFORM_DATABASE must be ${EXPECTED_DATABASE}" >&2
    return 1
  fi
}

prepare_migration_table() {
  mysql_command <<'SQL'
CREATE TABLE IF NOT EXISTS platform.schema_migrations (
  version VARCHAR(128) NOT NULL,
  applied_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (version)
) ENGINE=InnoDB;
SQL
}

apply_migration() {
  local migration_file="$1"
  local migration_name
  local applied

  migration_name="$(basename "${migration_file}" .sql)"
  if [[ ! "${migration_name}" =~ ^[0-9]{3}_[a-z0-9_]+$ ]]; then
    echo "Invalid migration filename: ${migration_file}" >&2
    return 1
  fi

  applied="$(
    mysql_command \
      --execute="SELECT COUNT(*) FROM platform.schema_migrations
        WHERE version = '${migration_name}'"
  )"
  if [[ "${applied}" == "1" ]]; then
    return 0
  fi

  echo "Applying migration ${migration_name}"
  mysql_command < "${migration_file}"
  mysql_command \
    --execute="INSERT INTO platform.schema_migrations (version)
      VALUES ('${migration_name}')"
}

main() {
  local migration_file

  validate_environment
  prepare_migration_table
  shopt -s nullglob
  for migration_file in "${MIGRATION_DIR}"/*.sql; do
    apply_migration "${migration_file}"
  done
}

main "$@"
