FROM golang:1.24-alpine AS builder

WORKDIR /src

ARG VERSION=dev
ARG BUILD_TIME=unknown

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags "-s -w -X main.version=${VERSION} -X main.buildTime=${BUILD_TIME}" \
    -o /out/sluice ./cmd/proxy

FROM alpine:3.21

ARG VERSION=dev
LABEL org.opencontainers.image.title="sluice" \
      org.opencontainers.image.version="${VERSION}"

RUN apk add --no-cache ca-certificates curl git wget

COPY --from=builder /out/sluice /usr/local/bin/sluice
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
COPY configs/config.yaml /etc/sluice/config.yaml

ENV SLUICE_MODE=client

EXPOSE 8080
ENTRYPOINT ["docker-entrypoint.sh"]
CMD ["sh"]
