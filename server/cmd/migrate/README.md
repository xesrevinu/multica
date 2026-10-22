# migrate

`cmd/migrate` applies the SQLite schema in one shot. It does not run the old Postgres migration history, advisory locks, or `down`.

From the `server/` module directory:

```sh
# default: file:./data/multica.db and schema/sqlite.sql
go run ./cmd/migrate up

DATABASE_URL=file:./data/multica.db \
MULTICA_SCHEMA_PATH=schema/sqlite.sql \
  go run ./cmd/migrate up
```

`DATABASE_URL` may be a `file:` URI or a bare path. `postgres://` is rejected. The parent directory of the database file is created if needed. The schema file is not embedded; a missing `schema/sqlite.sql` fails until that file exists or `MULTICA_SCHEMA_PATH` points at it.
