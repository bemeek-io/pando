#!/bin/sh
# Grant the unprivileged pando user access to the container runtime socket, then
# drop to it.
#
# The socket's group ID differs per host — it is not the same on Docker Desktop,
# a Linux server, or a rootless install — so it cannot be baked into the image.
# The entrypoint reads it at startup and joins that group.
#
# This starts as root and drops immediately, which is the standard pattern and
# the reason the image does not simply run as root: the Go process that serves
# traffic never has more than it needs. If the socket is absent the drop still
# happens — Pando starts, and the Docker adapter reports itself unhealthy, which
# the planner turns into a readable refusal rather than a crash.
set -e

SOCKET=/var/run/docker.sock

if [ -S "$SOCKET" ]; then
    SOCKET_GID=$(stat -c '%g' "$SOCKET")

    if [ "$SOCKET_GID" = "0" ]; then
        # Root-owned socket, as on Docker Desktop. Adding a user to the root
        # group is the narrowest way in without running the process as root.
        addgroup pando root 2>/dev/null || true
    else
        EXISTING=$(getent group "$SOCKET_GID" | cut -d: -f1)
        if [ -z "$EXISTING" ]; then
            addgroup -g "$SOCKET_GID" dockerhost 2>/dev/null || true
            EXISTING=dockerhost
        fi
        addgroup pando "$EXISTING" 2>/dev/null || true
    fi
fi

exec su-exec pando "$@"
