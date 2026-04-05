# syntax=docker/dockerfile:1

# ─── Build stage ──────────────────────────────────────────────────────────────
# Always runs on the host platform so cross-compilation is fast (pure Go).
FROM --platform=$BUILDPLATFORM golang:alpine AS builder

ARG TARGETARCH
ARG TARGETOS=linux

# Install Node.js, npm, and Task runner.
RUN apk add --no-cache nodejs npm bash && \
    go install github.com/go-task/task/v3/cmd/task@latest

WORKDIR /app

# Cache Go modules — layer is reused as long as go files don't change.
COPY go.mod go.sum ./
COPY backend/go.mod backend/go.sum ./backend/
RUN go work init . ./backend

# Cache npm dependencies — layer is reused as long as package-lock.json doesn't change.
COPY frontend/package.json frontend/package-lock.json ./frontend/
RUN npm ci --prefix frontend

# Copy the rest of the source.
COPY . .

# Build: Task runs `npm run build` then `go build` with GOOS/GOARCH set.
RUN task build OS=${TARGETOS} ARCH=${TARGETARCH}

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
EXPOSE 8080

ENTRYPOINT ["./portfolio-analysis"]
