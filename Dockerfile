# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src
COPY pkg/go.mod pkg/go.sum ./
RUN go mod download

COPY pkg/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/symagios ./cmd/symagios \
 && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/mcp-gateway ./cmd/mcp-gateway \
 && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/search-broker ./cmd/search-broker

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata \
 && mkdir -p /etc/symagios

WORKDIR /app

COPY --from=builder /out/symagios /usr/local/bin/symagios
COPY --from=builder /out/mcp-gateway /usr/local/bin/mcp-gateway
COPY --from=builder /out/search-broker /usr/local/bin/search-broker
COPY deploy/llm/config.yaml /etc/symagios/config.yaml

ENV SYMAGIOS_DB_PATH=/data/symagios.db \
    SYMAGIOS_WORKERS=4 \
    SYMAGIOS_LOG_LEVEL=info \
    SYMAGIOS_LOG_FORMAT=json \
    SYMAGIOS_CONFIG=/etc/symagios/config.yaml

VOLUME ["/data"]

ENTRYPOINT ["symagios"]
CMD ["engine"]
