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

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata su-exec \
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
