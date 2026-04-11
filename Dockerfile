# syntax=docker/dockerfile:1

# ─── Frontend build stage ─────────────────────────────────────────────────────
# Runs on host platform; output is architecture-independent.
# Skipped in CI when SKIP_FRONTEND=true (pre-built dist passed via context).
FROM --platform=$BUILDPLATFORM node:22-alpine AS frontend-builder
ARG SKIP_FRONTEND=false
WORKDIR /app
COPY frontend/package.json frontend/package-lock.json ./frontend/
RUN if [ "$SKIP_FRONTEND" = "true" ]; then echo "Skipping npm ci"; else npm ci --prefix frontend; fi
COPY frontend/ ./frontend/
RUN if [ "$SKIP_FRONTEND" = "true" ]; then mkdir -p /app/frontend/dist; else npm run build --prefix frontend; fi

# ─── Go build stage ───────────────────────────────────────────────────────────
# Always runs on the host platform so cross-compilation is fast (pure Go).
FROM --platform=$BUILDPLATFORM golang:alpine AS builder

ARG SKIP_FRONTEND=false
ARG TARGETARCH
ARG TARGETOS=linux

# Install Task runner only (Node.js not needed here).
RUN apk add --no-cache bash && \
    go install github.com/go-task/task/v3/cmd/task@latest

WORKDIR /app

# Cache Go modules — layer is reused as long as go.mod/go.sum don't change.
COPY go.mod go.sum ./
COPY backend/go.mod backend/go.sum ./backend/
RUN --mount=type=cache,target=/go/pkg/mod \
    go work init . ./backend && go mod download all

# Copy source.
COPY . .
# In standalone mode, frontend/dist is excluded by .dockerignore, so copy
# from the frontend-builder stage. In CI (SKIP_FRONTEND=true), frontend/dist
# is already in the build context — copy the (empty) builder output to a
# temp location and discard it so the context version is preserved.
COPY --from=frontend-builder /app/frontend/dist /tmp/frontend-dist
RUN if [ "$SKIP_FRONTEND" != "true" ]; then \
      rm -rf frontend/dist && cp -a /tmp/frontend-dist frontend/dist; \
    fi && rm -rf /tmp/frontend-dist

# Compile Go binary only (frontend already built above).
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    task go-build OS=${TARGETOS} ARCH=${TARGETARCH}

# ─── Final image ──────────────────────────────────────────────────────────────
FROM alpine:3.21

# ca-certificates: needed for HTTPS calls (Yahoo Finance, Gemini, CNB).
# tzdata: needed for correct timezone handling.
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

ARG TARGETARCH
ARG TARGETOS=linux
COPY --from=builder /app/dist/portfolio-analysis-${TARGETOS}-${TARGETARCH} ./portfolio-analysis

ENV PORT=8080
ENV METRICS_PORT=9090
EXPOSE 8080
EXPOSE 9090

ENTRYPOINT ["./portfolio-analysis"]
