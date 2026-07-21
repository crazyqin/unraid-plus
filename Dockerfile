# **************************************************************************
# unraid++ multi-arch production image
#   Stage 1: build React frontend (node)
#   Stage 2: build Go backend (golang) — embeds the frontend dist
#   Stage 3: minimal runtime (alpine)
# **************************************************************************

# -----------------------------------------------------------------------------
# Stage 1: frontend build
# -----------------------------------------------------------------------------
FROM node:20-alpine AS web-builder
WORKDIR /web

# Use pnpm for fast, deterministic installs
RUN corepack enable && corepack prepare pnpm@9.12.0 --activate

# Install deps first (cached layer)
COPY web/package.json web/pnpm-lock.yaml* ./
RUN pnpm install --frozen-lockfile || pnpm install

# Build
COPY web/ ./
RUN pnpm build

# -----------------------------------------------------------------------------
# Stage 2: backend build
#   The Go binary embeds /web/dist as static assets via go:embed.
# -----------------------------------------------------------------------------
FROM golang:1.23-alpine AS server-builder
WORKDIR /src

RUN apk add --no-cache git ca-certificates

# Download deps first (cached layer)
COPY server/go.mod server/go.sum* ./
RUN go mod download

# Copy source and the built frontend (for go:embed)
COPY server/ ./
COPY --from=web-builder /web/dist ./internal/web/dist

# Sync dependencies (writes go.sum if missing / stale). Safe no-op when already tidy.
RUN go mod tidy

# Build static binary, linux/amd64 by default (CI will override GOARCH for arm64)
RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags "-s -w -X main.Version=${VERSION:-dev} -X main.Commit=${COMMIT:-none} -X main.BuildTime=${BUILD_TIME:-unknown}" \
    -o /out/unraidpp ./cmd/server

# -----------------------------------------------------------------------------
# Stage 3: runtime
# -----------------------------------------------------------------------------
FROM alpine:3.20 AS runtime
WORKDIR /app

# tini for proper signal forwarding, ca-certs for HTTPS to Unraid
RUN apk add --no-cache tini ca-certificates tzdata && \
    addgroup -S app && adduser -S -G app app

COPY --from=server-builder /out/unraidpp /app/unraidpp

USER app
EXPOSE 8080

ENV TZ=Asia/Shanghai \
    UNRAIDPP_LISTEN=:8080 \
    UNRAIDPP_DATA_DIR=/data

ENTRYPOINT ["/sbin/tini", "--"]
CMD ["/app/unraidpp"]
