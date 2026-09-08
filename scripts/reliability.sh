#!/usr/bin/env bash
set -euo pipefail

if [[ -z "${TEST_DATABASE_URL:-}" ]]; then
  echo "TEST_DATABASE_URL must point to a disposable PostgreSQL server" >&2
  exit 2
fi

exec make reliability
