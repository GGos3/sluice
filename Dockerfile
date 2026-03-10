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
    -o /out/sluice ./cmd/proxy

FROM alpine:3.21

ARG VERSION=dev
LABEL org.opencontainers.image.title="sluice" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.source="https://github.com/ggos3/sluice" \
      org.opencontainers.image.description="Forward proxy for firewalled environments"

RUN apk add --no-cache bind-tools ca-certificates curl git ipset iptables redsocks wget

COPY --from=builder /out/sluice /usr/local/bin/sluice
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN mkdir -p /etc/sluice

ENV SLUICE_MODE=run

EXPOSE 18080
ENTRYPOINT ["docker-entrypoint.sh"]
CMD ["sh"]
