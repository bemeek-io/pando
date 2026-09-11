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
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 pando \
    && mkdir -p /var/lib/pando \
    && chown pando:pando /var/lib/pando

# Runs unprivileged. The runtime adapter reaches Docker through the mounted
# socket, which is a group membership question at deploy time, not a reason to
# run this process as root.
USER pando
WORKDIR /var/lib/pando

COPY --from=build /out/pando /usr/local/bin/pando

EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/pando"]
