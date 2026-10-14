#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
set -euo pipefail
[[ $# == 4 ]]
image=$1 version=$2 commit=$3 out=$4
engine=${CONTAINER_ENGINE:-docker}
[[ $image =~ ^sha256:[0-9a-f]{64}$ && $out == /home/* ]]
[[ $("$engine" image inspect --format '{{.Os}}/{{.Architecture}}' "$image") == linux/amd64 ]]
[[ $("$engine" image inspect --format '{{index .Config.Labels "org.opencontainers.image.version"}}' "$image") == "$version" ]]
[[ $("$engine" image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$image") == "$commit" ]]
"$engine" run --rm "$image" forgejo --version | tee "$out/version.txt"
grep -F "$version+gitea-1.22.0" "$out/version.txt"
grep -F 'bindata' "$out/version.txt"
mkdir "$out/data"
# All fixture state is disposable. Use the image's s6 setup and generated app.ini.
container=$("$engine" run -d --mount "type=bind,src=$out/data,dst=/data" \
  --mount "type=bind,src=$out/example,dst=/example,ro=true" \
  -e FORGEJO__security__INSTALL_LOCK=true -e FORGEJO__server__DISABLE_SSH=true \
  -e FORGEJO__extensions__ENABLED=true -e FORGEJO__extensions__REQUIRED_IDS=example \
  "$image")
cleanup() { "$engine" logs "$container" > "$out/container.log" 2>&1; "$engine" rm -fv "$container" >/dev/null; }
trap cleanup EXIT
# The first boot writes configuration but cannot become ready without its required package.
for ((i=0; i<60; i++)); do
  if "$engine" exec "$container" test -s /data/gitea/conf/app.ini; then break; fi
  sleep 1
done
"$engine" stop "$container" >/dev/null
"$engine" run --rm --mount "type=bind,src=$out/data,dst=/data" \
  --mount "type=bind,src=$out/example,dst=/example,ro=true" \
  "$image" su-exec git forgejo extensions install /example
"$engine" run --rm --mount "type=bind,src=$out/data,dst=/data" \
  "$image" su-exec git forgejo extensions status example | tee "$out/extension-status.txt"
grep -F $'example\tenabled\tmanifest-valid' "$out/extension-status.txt"
"$engine" start "$container" >/dev/null
ready() {
  for ((i=0; i<90; i++)); do
    if "$engine" exec "$container" curl -fsS http://localhost:3000/api/healthz > "$out/health.json"; then return; fi
    sleep 1
  done
  echo 'Container failed readiness' >&2
  return 1
}
verify() {
  ready
  "$engine" exec "$container" curl -fsS http://localhost:3000/api/v1/version | jq -e --arg v "$version+gitea-1.22.0" '.version == $v'
  for asset in js/index.js css/index.css licenses.txt; do
    "$engine" exec "$container" curl -fsS "http://localhost:3000/assets/$asset" > "$out/$(basename "$asset")"
    test -s "$out/$(basename "$asset")"
  done
  grep -F 'MIT' "$out/licenses.txt" >/dev/null
  # RequiredIDs makes readiness contingent on a real SDK handshake. A native
  # route (401 to an unauthenticated client) also proves contribution registration.
  [[ $("$engine" exec "$container" curl -s -o /dev/null -w '%{http_code}' http://localhost:3000/workspace) == 401 ]]
  "$engine" exec "$container" test -s /data/gitea/gitea.db
}
verify
"$engine" exec "$container" sh -c 'echo retained > /data/gitea/release-restart-marker'
"$engine" restart "$container" >/dev/null
verify
"$engine" exec "$container" grep -Fx retained /data/gitea/release-restart-marker
echo 'Image startup, assets, extension handshake and persistent-state restart passed.'
