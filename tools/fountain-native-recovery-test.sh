#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
set -euo pipefail

# Disposable CLI wiring proof; never accepts retained appliance paths or a DSN.
[[ $# == 2 ]] || { echo "Usage: $0 IMAGE /home/PATH/NEW-OUTPUT" >&2; exit 2; }
engine=${CONTAINER_ENGINE:-docker}
[[ $1 =~ ^sha256:[0-9a-f]{64}$ ]]
image=$("$engine" image inspect --format '{{.Id}}' "$1")
out=$(realpath -m -- "$2")
[[ $image == "$1" && $out == /home/* && ! -e $out && ! -L $out ]]
mkdir -m 700 "$out"
out=$(cd "$out" && pwd -P)
pg_image=$("$engine" image inspect --format '{{.Id}}' "${FOUNTAIN_TEST_POSTGRES_IMAGE:-docker.io/library/postgres:17}")
[[ $pg_image =~ ^sha256:[0-9a-f]{64}$ ]]
fixture="fountain-recovery-$$"
pg_created=0
network_created=0
cleanup() {
  local result=$?
  if [[ $pg_created == 1 ]]; then
    "$engine" logs "$fixture-pg" > "$out/postgres.log" 2>&1 || true
    "$engine" rm -fv "$fixture-pg" >/dev/null || true
  fi
  if [[ $network_created == 1 ]]; then
    "$engine" network rm "$fixture-net" >/dev/null || true
  fi
  # Only this tool's newly created fixture subtree is touched. In rootless
  # Podman this restores host-owner readability from the subordinate git UID.
  "$engine" run --rm --pull=never --network=none --user=0:0 \
    --volume="$out:/fixture:Z" --entrypoint=/bin/sh "$image" \
    -ec 'chown -R 0:0 /fixture' >/dev/null 2>&1 || true
  exit "$result"
}
trap cleanup EXIT

printf 'image: %s\npostgres image: %s\n' "$image" "$pg_image" > "$out/identities.txt"
"$engine" image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$image" >> "$out/identities.txt"
mkdir -p "$out/data/gitea/conf" "$out/custom"
printf '%s\n' 'disposable-fixture-password' > "$out/forgejo.passwd"
chmod 600 "$out/forgejo.passwd"
cat > "$out/data/gitea/conf/app.ini" <<'CONFIG'
RUN_USER = git
RUN_MODE = prod
WORK_PATH = /data/gitea
[server]
APP_DATA_PATH = /data/gitea
DISABLE_SSH = true
[security]
INSTALL_LOCK = true
[database]
DB_TYPE = postgres
HOST = soda-postgres:5432
NAME = forgejo
USER = forgejo
PASSWD_URI = file:/etc/forgejo/db_passwd
SSL_MODE = disable
[repository]
ROOT = /data/git/repositories
[log]
MODE = console
LEVEL = Warn
CONFIG
"$engine" run --rm --pull=never --network=none --user=0:0 \
  --volume="$out:/fixture:Z" --entrypoint=/bin/sh "$image" \
  -ec 'chown -R 1000:1000 /fixture/data /fixture/custom /fixture/forgejo.passwd'

"$engine" network create "$fixture-net" >/dev/null
network_created=1
"$engine" run -d --name "$fixture-pg" --pull=never \
  --network="$fixture-net" --network-alias=soda-postgres \
  --tmpfs=/var/lib/postgresql/data:rw \
  --env=POSTGRES_USER=forgejo --env=POSTGRES_DB=forgejo \
  --env=POSTGRES_PASSWORD=disposable-fixture-password \
  "$pg_image" >/dev/null
pg_created=1
for ((attempt=0; attempt<60; attempt++)); do
  if "$engine" exec "$fixture-pg" pg_isready -U forgejo -d forgejo >/dev/null 2>&1; then break; fi
  sleep 1
done
"$engine" exec "$fixture-pg" pg_isready -U forgejo -d forgejo >/dev/null

cli() {
  "$engine" run --rm --pull=never --network="$fixture-net" \
    --user=1000:1000 --workdir=/data/gitea \
    --cap-drop=all --security-opt=no-new-privileges \
    --volume="$out/data:/data:Z" \
    --volume="$out/custom:/usr/share/soda/forgejo:ro,Z" \
    --volume="$out/forgejo.passwd:/etc/forgejo/db_passwd:ro,Z" \
    --entrypoint=/app/gitea/gitea "$image" \
    --work-path=/data/gitea --custom-path=/usr/share/soda/forgejo \
    --config=/data/gitea/conf/app.ini "$@"
}
expect_exit() {
  local expected=$1 log=$2 actual=0
  shift 2
  cli admin native-operation "$@" > "$out/$log" 2>&1 || actual=$?
  [[ $actual == "$expected" ]] || {
    echo "Expected exit $expected, got $actual; inspect $out/$log" >&2
    return 1
  }
}
sql() {
  "$engine" exec -i "$fixture-pg" psql -X -v ON_ERROR_STOP=1 -U forgejo -d forgejo "$@"
}

# This initializes only the brand-new disposable database. No web, SSH,
# extension manager, service controller or background writer is ever started.
cli migrate > "$out/migrate.log" 2>&1
cli --version > "$out/version.txt"
expect_exit 0 status-before.txt status
grep -Fx 'state: idle' "$out/status-before.txt"
grep -Fx 'inhibited: false' "$out/status-before.txt"
expect_exit 1 refusal-uninhibited.txt recover --owner ord:fixture/none --generation 1
grep -F 'domain is not inhibited' "$out/refusal-uninhibited.txt"
"$engine" run --rm --pull=never --network=none --user=1000:1000 \
  --volume="$out/data:/data:Z" --entrypoint=/bin/sh "$image" \
  -ec 'umask 077; : > /data/gitea/nativeop-offline'
expect_exit 0 recovery-idle.txt recover --owner ord:fixture/none --generation 1
grep -Fx 'verdict: idle' "$out/recovery-idle.txt"

# SQL here constructs explicit crash-prefix fixtures, never an operator recipe.
# The native CLI must preserve identity refusals, release a known absent-token
# authority effect and retain an unrecognized writer family for intervention.
sql > "$out/seed-known.log" <<'SQL'
UPDATE reservation SET owner='ord:authority/token/9999/fixture', generation=41,
  owner_kind='ordinary', verifier='fixture-only', scope='{"kind":"ordinary","repository_id":0,"family":"authority","authority_op":"token","authority_id":9999}'
  WHERE id=1 AND owner='';
SQL
expect_exit 3 refusal-owner.txt recover --owner ord:fixture/wrong --generation 41
grep -Fx 'reason: wrong_owner' "$out/refusal-owner.txt"
expect_exit 3 refusal-generation.txt recover --owner ord:authority/token/9999/fixture --generation 42
grep -Fx 'reason: wrong_generation' "$out/refusal-generation.txt"
expect_exit 0 recovery-known.txt recover --owner ord:authority/token/9999/fixture --generation 41
grep -Fx 'verdict: released' "$out/recovery-known.txt"
grep -Fx 'effect: consistent' "$out/recovery-known.txt"
expect_exit 0 status-released.txt status
grep -Fx 'state: idle' "$out/status-released.txt"
grep -Fx 'inhibited: true' "$out/status-released.txt"
sql > "$out/seed-unknown.log" <<'SQL'
UPDATE reservation SET owner='ord:fixture/unknown', generation=43,
  owner_kind='ordinary', verifier='fixture-only', scope='{"kind":"ordinary","repository_id":0,"family":"fixture-unknown"}'
  WHERE id=1 AND owner='';
SQL
expect_exit 3 recovery-fenced.txt recover --owner ord:fixture/unknown --generation 43
grep -Fx 'verdict: fenced' "$out/recovery-fenced.txt"
grep -Fx 'reason: unknown_family' "$out/recovery-fenced.txt"
expect_exit 0 status-fenced.txt status
grep -Fx 'state: held' "$out/status-fenced.txt"
grep -Fx 'owner: ord:fixture/unknown' "$out/status-fenced.txt"
grep -Fx 'generation: 43' "$out/status-fenced.txt"
grep -Fx 'inhibited: true' "$out/status-fenced.txt"
echo 'Disposable image CLI, PostgreSQL mapping, identity refusals and recovery verdicts passed.'
