#!/usr/bin/env bash
# Retired with docker/entrypoint.sh; the copy-only image uses the Go multicall
# entrypoint (server/cmd/server/multicall.go) instead of a shell wrapper.
echo "entrypoint.test.sh: skipped (shell container entrypoint retired)"
exit 0
