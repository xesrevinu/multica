#!/bin/sh
# Create (if needed) and migrate the orchestrator integration-test database.
#   eval "$(scripts/orch-testdb.sh | tail -1)"; go test ./internal/orchestrator -run Integration
set -e
BASE=${PG_BASE:-postgres://multica:multica@localhost:5432}
DB=${ORCH_TEST_DB:-multica_orch_test}
cd "$(dirname "$0")/.."
go run ./scripts/orchtestdb "$BASE" "$DB"
DATABASE_URL="$BASE/$DB?sslmode=disable" go run ./cmd/migrate up | tail -1
echo "export ORCH_TEST_DATABASE_URL=$BASE/$DB?sslmode=disable"
