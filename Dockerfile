# syntax=docker/dockerfile:1

# ─── Frontend build stage ─────────────────────────────────────────────────────
# Runs on host platform; output is architecture-independent, so this stage
# is shared across all platform targets and only runs once.
FROM --platform=$BUILDPLATFORM node:22-alpine AS frontend-builder
WORKDIR /app
COPY frontend/package.json frontend/package-lock.json ./frontend/
RUN npm ci --prefix frontend
COPY frontend/ ./frontend/
RUN npm run build --prefix frontend

# ─── Go build stage ───────────────────────────────────────────────────────────
# Always runs on the host platform so cross-compilation is fast (pure Go).
FROM --platform=$BUILDPLATFORM golang:alpine AS builder

ARG TARGETARCH
ARG TARGETOS=linux

# Install Task runner only (Node.js not needed here).
RUN apk add --no-cache bash && \
    go install github.com/go-task/task/v3/cmd/task@latest

WORKDIR /app

# Cache Go modules — layer is reused as long as go.mod/go.sum don't change.
COPY go.mod go.sum ./
COPY backend/go.mod backend/go.sum ./backend/
RUN go work init . ./backend && go mod download all

# Copy source; frontend/dist is excluded by .dockerignore so the pre-built
# artifact copied below won't be overwritten.
COPY . .
COPY --from=frontend-builder /app/frontend/dist ./frontend/dist/

# Compile Go binary only (frontend already built above).
RUN task go-build OS=${TARGETOS} ARCH=${TARGETARCH}

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
