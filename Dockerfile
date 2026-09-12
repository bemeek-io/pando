# Pando ships as one binary (R-253). This image is how it reaches a host —
# the artifact is unchanged, the container is just the delivery.
#
FROM golang:1.27-alpine AS build
WORKDIR /src

# Dependencies first, so a source change does not re-download the module cache.
COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/pando ./cmd/pando

# 3.21 rather than 3.20 because that is where postgresql17-client appears, and
# the client major version has to match the server: pg_dump refuses a server
# newer than itself, and discovering that during a restore is discovering it at
# the worst possible moment. Bump this with the postgres service in
# docker-compose.yml, never separately.
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata su-exec postgresql17-client \
    && adduser -D -u 10001 pando \
    && mkdir -p /var/lib/pando \
    && chown pando:pando /var/lib/pando

WORKDIR /var/lib/pando

COPY --from=build /out/pando /usr/local/bin/pando
COPY entrypoint.sh /usr/local/bin/entrypoint.sh

# The entrypoint starts as root only long enough to join the runtime socket's
# group — whose ID differs per host and so cannot be baked in — then drops to
# the unprivileged pando user. The server itself never runs as root.
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/entrypoint.sh", "/usr/local/bin/pando"]
