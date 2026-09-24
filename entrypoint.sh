#!/bin/sh
# Grant the unprivileged nonroot user access to the container runtime socket,
# then drop to it.
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
#
# The user is the hardened runtime base's own, nonroot (65532). The base is
# busybox and nothing else, so the group is looked up in /etc/group directly:
# there is no getent.
set -e

USER_NAME=nonroot
SOCKET=/var/run/docker.sock

if [ -S "$SOCKET" ]; then
    SOCKET_GID=$(stat -c '%g' "$SOCKET")

    # Join whichever group owns the socket, creating it under that ID if the
    # image has none. That includes 0 — the socket is root-owned on Docker
    # Desktop — because the hardened base has no root group to join by name.
    # Joining the group is the narrowest way in without running as root.
    EXISTING=$(grep -E "^[^:]*:[^:]*:${SOCKET_GID}:" /etc/group | head -n 1 | cut -d: -f1)
    if [ -z "$EXISTING" ]; then
        addgroup -g "$SOCKET_GID" dockerhost 2>/dev/null || true
        EXISTING=dockerhost
    fi
    addgroup "$USER_NAME" "$EXISTING" 2>/dev/null || true
fi

exec su-exec "$USER_NAME" "$@"
