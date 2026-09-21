#!/bin/bash
set -e
set -o pipefail

MIGRATIONS_DIR="${MIGRATIONS_DIR:-/migrations}"
export MYSQL_PWD="${DB_PASSWORD}"

run_sql() {
  mariadb --host="${DB_HOST}" --port="${DB_PORT:-3306}" --user="${DB_USER}" --batch --skip-column-names "${DB_NAME}" "$@"
}

ensure_bookkeeping() {
  run_sql --execute="CREATE TABLE IF NOT EXISTS schema_migrations (version VARCHAR(255) PRIMARY KEY, applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)"
}

migrate_up() {
  local file version applied
  for file in "${MIGRATIONS_DIR}"/*.up.sql; do
    [ -e "${file}" ] || continue
    version="$(basename "${file}" .up.sql)"
    applied="$(run_sql --execute="SELECT COUNT(*) FROM schema_migrations WHERE version='${version}'")"
    if [ "${applied}" != "0" ]; then
      continue
    fi
    echo "applying ${version}"
    run_sql <"${file}"
    run_sql --execute="INSERT INTO schema_migrations (version) VALUES ('${version}')"
  done
  echo "migrations up to date"
}

migrate_down() {
  local version
  version="$(run_sql --execute="SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1")"
  if [ -z "${version}" ]; then
    echo "nothing to roll back"
    return
  fi
  echo "rolling back ${version}"
  run_sql <"${MIGRATIONS_DIR}/${version}.down.sql"
  run_sql --execute="DELETE FROM schema_migrations WHERE version='${version}'"
}

ensure_bookkeeping
case "${1:-up}" in
up) migrate_up ;;
down) migrate_down ;;
*)
  echo "usage: migrate.sh [up|down]" >&2
  exit 2
  ;;
esac
