# syntax=docker/dockerfile:1.4
# Multi-stage build for AI Fabric Go binaries.
FROM golang:1.26.1 AS builder

# Install git for modules that resolve via VCS.
RUN apt-get update && apt-get install -y --no-install-recommends \
  git && \
  rm -rf /var/lib/apt/lists/*

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
  go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
  --mount=type=cache,target=/root/.cache/go-build \
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -p $(nproc) -o /out/issue-handler ./cmd/issue-handler && \
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -p $(nproc) -o /out/bot ./cmd/bot && \
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -p $(nproc) -o /out/semantic-version ./cmd/semantic-version && \
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -p $(nproc) -o /out/fetch-doc ./cmd/fetch-doc

FROM debian:bookworm-slim

RUN apt-get update >/dev/null && apt-get install -y --no-install-recommends \
  ca-certificates bash git docker.io && \
  rm -rf /var/lib/apt/lists/*

WORKDIR /workspace

COPY --from=builder /out/issue-handler /usr/local/bin/issue-handler
COPY --from=builder /out/bot /usr/local/bin/tg-bot
COPY --from=builder /out/semantic-version /usr/local/bin/semantic-version
COPY --from=builder /out/fetch-doc /usr/local/bin/fetch-doc

ENTRYPOINT ["/bin/bash", "-lc"]
CMD ["tg-bot"]
