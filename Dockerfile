# --- Build stage ---
FROM golang:1.26.9-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src

# Cache dependencies
COPY server/go.mod server/go.sum ./server/
RUN cd server && go mod download

# Copy server source
COPY server/ ./server/

# Build the multi-call server binary (serves as server, entrypoint, and migrate)
# and the loopback maintenance CLI.
ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown
RUN cd server && CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" -o bin/server ./cmd/server
RUN cd server && CGO_ENABLED=0 go build -ldflags "-s -w" -o bin/maintenance ./cmd/maintenance

# Stage the runtime filesystem for the distroless image.
RUN mkdir -p /out/data/db /out/data/uploads /out/schema /out/migrations /out/dist \
  && cp /src/server/bin/server /out/server \
  && ln -s server /out/entrypoint \
  && ln -s server /out/migrate \
  && cp /src/server/bin/maintenance /out/maintenance \
  && cp /src/server/schema/sqlite.sql /out/schema/sqlite.sql \
  && cp -R /src/server/migrations/. /out/migrations/

COPY LICENSE NOTICE /out/
COPY apps/web/dist /out/dist
RUN rm -rf /out/dist/server/_next /out/dist/server/ssr /out/dist/server/assets \
  && rm -f /out/dist/server/*.js /out/dist/server/*.mjs

# --- Runtime stage ---
# Single-container runtime: gcr.io/distroless/base-debian12 provides glibc and
# CA certificates for both the Go API server (:8080) and the Bun single-file
# web executable (:3000) with no shell or package manager overhead.
FROM gcr.io/distroless/base-debian12

WORKDIR /app

ENV NODE_ENV=production
ENV PORT=8080
ENV FRONTEND_PORT=3000
ENV FRONTEND_HOST=0.0.0.0
ENV REMOTE_API_URL=http://127.0.0.1:8080
ENV DATABASE_URL=file:/app/data/db/multica.db

COPY --from=builder /out/ /app/

EXPOSE 8080 3000

ENTRYPOINT ["/app/entrypoint"]
