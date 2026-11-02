#!/usr/bin/env bash
# Host-build Multica into .docker/stage/ for the copy-only Dockerfile.
# Default target: linux/arm64 (OrbStack / Apple Silicon home lab).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
STAGE="${ROOT}/.docker/stage"
GOOS="${GOOS:-linux}"
GOARCH="${GOARCH:-arm64}"
VERSION="${VERSION:-$(git -C "$ROOT" describe --tags --match 'v[0-9]*' --always --dirty 2>/dev/null || echo dev)}"
COMMIT="${COMMIT:-$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)}"
DATE="${DATE:-$(date -u '+%Y-%m-%dT%H:%M:%SZ')}"

WEB_TARGET="${MULTICA_WEB_COMPILE_TARGET:-}"
if [[ -z $WEB_TARGET && $GOOS == linux ]]; then
  case "$GOARCH" in
  arm64 | aarch64) WEB_TARGET=bun-linux-arm64 ;;
  amd64 | x86_64) WEB_TARGET=bun-linux-x64 ;;
  esac
fi

log() { printf '\033[1;32m==>\033[0m %s\n' "$*"; }

cd "$ROOT"

log "go build server+maintenance (${GOOS}/${GOARCH}) ${VERSION}"
mkdir -p "${ROOT}/server/bin"
(
  cd "${ROOT}/server"
  export CGO_ENABLED=0 GOOS GOARCH
  go build -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o bin/server ./cmd/server
  go build -ldflags "-s -w" -o bin/maintenance ./cmd/maintenance
)

log "web build${WEB_TARGET:+ (target=${WEB_TARGET})}"
if [[ -n $WEB_TARGET ]]; then
  MULTICA_WEB_COMPILE_TARGET="$WEB_TARGET" pnpm --filter @multica/web build
else
  pnpm --filter @multica/web build
fi

web_bin="${ROOT}/apps/web/dist/server/multica-web"
if [[ ! -x $web_bin ]]; then
  echo "missing web binary at ${web_bin}" >&2
  exit 1
fi
case "$(file -b "$web_bin" 2>/dev/null || true)" in
*Mach-O*)
  echo "web binary is still Mach-O; set MULTICA_WEB_COMPILE_TARGET=bun-linux-arm64 (or x64)" >&2
  exit 1
  ;;
esac

log "stage → ${STAGE}"
rm -rf "$STAGE"
mkdir -p "${STAGE}/schema" "${STAGE}/migrations" "${STAGE}/dist/server" "${STAGE}/data/db" "${STAGE}/data/uploads"

cp "${ROOT}/server/bin/server" "${STAGE}/server"
cp "${ROOT}/server/bin/maintenance" "${STAGE}/maintenance"
ln -sf server "${STAGE}/entrypoint"
ln -sf server "${STAGE}/migrate"
cp "${ROOT}/server/schema/sqlite.sql" "${STAGE}/schema/sqlite.sql"
cp -R "${ROOT}/server/migrations/." "${STAGE}/migrations/"
# Keep the linux web executable + static assets the multicall launcher expects.
cp -R "${ROOT}/apps/web/dist/." "${STAGE}/dist/"
cp "${ROOT}/LICENSE" "${ROOT}/NOTICE" "${STAGE}/"

log "staged $(du -sh "$STAGE" | awk '{print $1}')"
log "next: docker build --platform ${GOOS}/${GOARCH} -t multica-backend:local ${ROOT}"
