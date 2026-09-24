#!/bin/sh
# Writes the compose file a release ships: docker-compose.yml with the pando
# service's `build: .` replaced by the published image, pinned to the version
# being released.
#
# Generated from the one compose file rather than kept as a second copy, so the
# file an operator downloads cannot drift from the one CI and contributors run:
# every volume, variable and the Docker socket mount stay described in one place
# (issue #52).
#
# usage: scripts/release-compose.sh 0.3.0 [image] > docker-compose.yml
set -eu

version=${1:?usage: release-compose.sh VERSION [IMAGE]}
image=${2:-trypando/pando}
version=${version#v}
src=$(dirname "$0")/../docker-compose.yml

# Exactly one `build: .`, the pando service's. Anything else means the file
# changed shape and this substitution would ship something nobody reviewed.
count=$(grep -c '^    build: \.$' "$src" || true)
if [ "$count" != 1 ]; then
  echo "release-compose.sh: expected one '    build: .' line in docker-compose.yml, found $count" >&2
  exit 1
fi

cat <<EOF
# Pando ${version}, from https://github.com/bemeek-io/pando/releases/tag/v${version}
#
# Runs the published image ${image}:${version}. To upgrade, download the
# compose file of the newer release over this one and run \`docker compose up -d\`
# again; the data lives in the named volumes below and is kept.
#
EOF
sed "s|^    build: \.\$|    image: ${image}:${version}|" "$src"
