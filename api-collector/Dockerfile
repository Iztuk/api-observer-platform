FROM golang:1.26-bookworm AS builder

WORKDIR /src

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        build-essential \
        ca-certificates && \
    rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=1 \
    GOOS=linux \
    go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/api-observer-collector \
    .

FROM debian:bookworm-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        ca-certificates \
        tzdata && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder \
    /out/api-observer-collector \
    /usr/local/bin/api-observer-collector

RUN mkdir -p \
    /var/lib/api-observer-collector \
    /logs

VOLUME ["/var/lib/api-observer-collector"]

ENTRYPOINT ["/usr/local/bin/api-observer-collector"]
