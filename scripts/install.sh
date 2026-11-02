#!/usr/bin/env bash
# The docker-compose.selfhost stack was removed from this fork in favor of a
# single copy-only SQLite image (`make image` + Helm/K8s). See Dockerfile and
# scripts/stage-image.sh.
echo "scripts/install.sh: the Compose self-host installer is retired in this fork." >&2
echo "Build the image with: make image" >&2
echo "Deploy with your Helm values (postgres.enabled=false, frontend.embedded=true)." >&2
exit 1

