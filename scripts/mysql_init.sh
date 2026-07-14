#!/bin/bash
# Creates the restricted orchestrator database account and bootstrap flags.

# This file is sourced by the official MySQL entrypoint. Enabling nounset here
# would leak into the parent script and break its optional positional arguments.
set -eo pipefail

readonly REQUIRED_VARIABLES=(
  MYSQL_ORCHESTRATOR_USER
  MYSQL_ORCHESTRATOR_PASSWORD
  MYSQL_PLATFORM_DATABASE
  MYSQL_PLATFORM_USER
  SEED_TEST_USERS
)

validate_identifier() {
  local name="$1"
  local value="$2"
  if [[ ! "${value}" =~ ^[A-Za-z][A-Za-z0-9_]{0,63}$ ]]; then
    echo "Invalid MySQL identifier in ${name}" >&2
    return 1
  fi
}

escape_sql_string() {
  local value="$1"
  value="${value//\\/\\\\}"
  value="${value//\'/\'\'}"
  printf '%s' "${value}"
}

main() {
  local variable
  for variable in "${REQUIRED_VARIABLES[@]}"; do
    if [[ -z "${!variable:-}" ]]; then
      echo "Required environment variable ${variable} is empty" >&2
      return 1
    fi
  done

  validate_identifier "MYSQL_PLATFORM_DATABASE" "${MYSQL_PLATFORM_DATABASE}"
  validate_identifier "MYSQL_PLATFORM_USER" "${MYSQL_PLATFORM_USER}"
  validate_identifier "MYSQL_ORCHESTRATOR_USER" "${MYSQL_ORCHESTRATOR_USER}"

  if [[ "${MYSQL_PLATFORM_DATABASE}" != "platform" ]]; then
    echo "MYSQL_PLATFORM_DATABASE must be platform" >&2
    return 1
  fi

  local escaped_password
  escaped_password="$(escape_sql_string "${MYSQL_ORCHESTRATOR_PASSWORD}")"

  docker_process_sql --database=mysql <<-EOSQL
	REVOKE ALL PRIVILEGES, GRANT OPTION
	  FROM '${MYSQL_PLATFORM_USER}'@'%';
	GRANT SELECT, INSERT, UPDATE, DELETE
	  ON \`${MYSQL_PLATFORM_DATABASE}\`.*
	  TO '${MYSQL_PLATFORM_USER}'@'%';
	CREATE USER IF NOT EXISTS '${MYSQL_ORCHESTRATOR_USER}'@'%'
	  IDENTIFIED BY '${escaped_password}';
	ALTER USER '${MYSQL_ORCHESTRATOR_USER}'@'%'
	  IDENTIFIED BY '${escaped_password}';
	GRANT EXECUTE ON \`${MYSQL_PLATFORM_DATABASE}\`.*
	  TO '${MYSQL_ORCHESTRATOR_USER}'@'%';
	CREATE TABLE IF NOT EXISTS \`${MYSQL_PLATFORM_DATABASE}\`.bootstrap_settings (
	  setting_key VARCHAR(64) PRIMARY KEY,
	  setting_value VARCHAR(255) NOT NULL
	) ENGINE=InnoDB;
	INSERT INTO \`${MYSQL_PLATFORM_DATABASE}\`.bootstrap_settings (
	  setting_key,
	  setting_value
	) VALUES (
	  'seed_test_users',
	  '${SEED_TEST_USERS}'
	) ON DUPLICATE KEY UPDATE
	  setting_value = VALUES(setting_value);
	FLUSH PRIVILEGES;
EOSQL
}

main "$@"
