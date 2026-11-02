# Single Multica image (API + embedded web + SQLite).
#
# Same idea as the old Dockerfile.web: compile on the host/CI, this file only
# packages. Build inputs with:
#   ./scripts/stage-image.sh          # → .docker/stage/
#   docker build -t multica-backend:local .
#
# Or: make image
#
# Stage layout (created by scripts/stage-image.sh):
#   server, entrypoint→server, migrate→server, maintenance,
#   schema/, migrations/, dist/server/multica-web (+ static assets),
#   LICENSE, NOTICE
FROM gcr.io/distroless/base-debian12

WORKDIR /app

ENV NODE_ENV=production
ENV PORT=8080
ENV FRONTEND_PORT=3000
ENV FRONTEND_HOST=0.0.0.0
ENV REMOTE_API_URL=http://127.0.0.1:8080
ENV DATABASE_URL=file:/app/data/db/multica.db

COPY .docker/stage/ /app/

EXPOSE 8080 3000

ENTRYPOINT ["/app/entrypoint"]
