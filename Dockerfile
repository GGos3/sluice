FROM golang:1.24-alpine AS builder

WORKDIR /src

ARG VERSION=dev
ARG BUILD_TIME=unknown
ARG TARGETOS
ARG TARGETARCH

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -ldflags "-s -w -X main.version=${VERSION} -X main.buildTime=${BUILD_TIME}" \
    -o /out/sluice ./cmd/sluice

# =============================================================================
# Target: server
# =============================================================================
FROM alpine:3.21 AS server

ARG VERSION=dev
LABEL org.opencontainers.image.title="sluice-server" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.source="https://github.com/ggos3/sluice" \
      org.opencontainers.image.description="Sluice proxy server with SSH tunnel support"

# Install proxy tools + SSH client for tunnel orchestration
RUN apk add --no-cache bind-tools ca-certificates openssh-client

COPY --from=builder /out/sluice /usr/local/bin/sluice
RUN mkdir -p /etc/sluice

EXPOSE 18080

ENTRYPOINT ["sluice", "server"]
CMD ["--help"]

# =============================================================================
# Target: agent
# =============================================================================
FROM alpine:3.21 AS agent

ARG VERSION=dev
LABEL org.opencontainers.image.title="sluice-agent" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.source="https://github.com/ggos3/sluice" \
      org.opencontainers.image.description="Sluice transparent proxy agent (Linux only)"

# Agent needs minimal tools - just the binary
RUN apk add --no-cache ca-certificates

COPY --from=builder /out/sluice /usr/local/bin/sluice

ENTRYPOINT ["sluice", "agent"]
CMD ["--help"]
