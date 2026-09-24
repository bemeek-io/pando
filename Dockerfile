# Pando ships as one binary (R-253). This image is how it reaches a host —
# the artifact is unchanged, the container is just the delivery.
#
# Base images are pinned by digest, with the tag kept as the readable half. A
# tag moves — `alpine:3.24` is a different filesystem this month than last — so
# a build is only reproducible against a digest. Dependabot updates these, which
# is what keeps the pin from meaning "old".
#
# The console is embedded in the binary rather than served beside it (R-253),
# so it has to exist before the Go build, not after it. Building it here rather
# than relying on whatever the host happens to have in internal/console/dist:
# that directory holds only a README in a fresh clone, so an image built without
# this stage starts, serves the API, and 404s the UI.
#
# Go as well as Node, because `npm run build` regenerates the API types from the
# Go types first (R-261: the API is the product, so the console's view of it is
# generated rather than written). A Node-only stage cannot run it, and splitting
# the steps here would put a second definition of how the console builds next to
# the one in package.json.
#
# Neither toolchain reaches the final image.
#
# Both build stages run on the builder's own platform and cross-compile: the
# console is JavaScript and the binary is built with cgo off, so neither needs
# to run on the platform it is for. The release builds amd64 and arm64 in one
# go (issue #52), and running npm and the Go toolchain under emulation for the
# other one took the better part of an hour.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine@sha256:4cb7ac979db5fcc41cae44b2227ba5ab8a51e8807f40d9ba4dee20a0ad960b5b AS console
WORKDIR /src
RUN apk add --no-cache nodejs npm

# Manifests first, so a change to console source does not re-run npm ci.
COPY console/package.json console/package-lock.json ./console/
RUN cd console && npm ci

# The whole tree: the build reads cmd/gen-api-types for the types and
# .claude/skills/pando-design for the design system.
COPY . .
# Vite is configured to write to ../internal/console/dist, which is the path
# go:embed reads.
RUN cd console && npm run build

FROM --platform=$BUILDPLATFORM golang:1.27-alpine@sha256:4cb7ac979db5fcc41cae44b2227ba5ab8a51e8807f40d9ba4dee20a0ad960b5b AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src

# Dependencies first, so a source change does not re-download the module cache.
COPY go.mod go.sum* ./
RUN go mod download

COPY . .
COPY --from=console /src/internal/console/dist/ ./internal/console/dist/

# go:embed is satisfied by the directory's README alone, so a console that
# failed to arrive would produce a binary that builds, starts, and has no UI.
# Fail here instead, where the cause is still visible.
RUN test -f internal/console/dist/index.html \
    || { echo "the console did not reach the build stage" >&2; exit 1; }

# Stamped only when the release workflow says what it is building
# (.github/workflows/image.yml), with the same three values GoReleaser stamps
# into the released CLI. A local build passes none and `pando version` says
# "development build", which is accurate: it is not a release and must not
# claim to be one.
ARG VERSION=""
ARG COMMIT=""
ARG BUILD_DATE=""
RUN stamp=""; \
    if [ -n "$VERSION" ]; then \
      stamp="-X main.buildVersion=${VERSION} -X main.buildCommit=${COMMIT} -X main.buildDate=${BUILD_DATE}"; \
    fi; \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
      go build -trimpath -ldflags="-s -w ${stamp}" -o /out/pando ./cmd/pando

# 3.21 rather than 3.20 because that is where postgresql17-client appears, and
# the client major version has to match the server: pg_dump refuses a server
# newer than itself, and discovering that during a restore is discovering it at
# the worst possible moment. Bump this with the postgres service in
# docker-compose.yml, never separately.
# nixpacks turns a repository with no deployment instructions into a
# Dockerfile, which BuildKit then builds (R-095: wrap an existing
# implementation rather than reimplementing convention-matching).
#
# Fetched here rather than at runtime so an install with no internet still
# builds, and pinned so the same repository produces the same plan a year from
# now. It only ever *generates* — `nixpacks build --out` writes a Dockerfile and
# does not build, so no container runtime socket is involved anywhere (R-112).
FROM --platform=$BUILDPLATFORM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b AS nixpacks
ARG NIXPACKS_VERSION=1.41.0
ARG TARGETARCH
RUN apk add --no-cache curl tar \
    && case "$TARGETARCH" in \
         arm64) arch=aarch64 ;; \
         amd64) arch=x86_64  ;; \
         *) echo "unsupported architecture: $TARGETARCH" >&2; exit 1 ;; \
       esac \
    && curl -fsSL -o /tmp/nixpacks.tgz \
       "https://github.com/railwayapp/nixpacks/releases/download/v${NIXPACKS_VERSION}/nixpacks-v${NIXPACKS_VERSION}-${arch}-unknown-linux-musl.tar.gz" \
    && tar xzf /tmp/nixpacks.tgz -C /usr/local/bin nixpacks \
    && chmod +x /usr/local/bin/nixpacks

FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b
RUN apk add --no-cache ca-certificates tzdata su-exec postgresql17-client \
    && adduser -D -u 10001 pando \
    && mkdir -p /var/lib/pando /etc/traefik/dynamic \
    && chown pando:pando /var/lib/pando /etc/traefik/dynamic

WORKDIR /var/lib/pando

COPY --from=build /out/pando /usr/local/bin/pando
COPY --from=nixpacks /usr/local/bin/nixpacks /usr/local/bin/nixpacks
COPY entrypoint.sh /usr/local/bin/entrypoint.sh

# The entrypoint starts as root only long enough to join the runtime socket's
# group — whose ID differs per host and so cannot be baked in — then drops to
# the unprivileged pando user. The server itself never runs as root.
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/entrypoint.sh", "/usr/local/bin/pando"]
