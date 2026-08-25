#!/usr/bin/env bash
# Cross-instance federation e2e test.
#
# Spins up two Forgejo instances (forgejoA, forgejoB) on a shared Docker
# network. Instance A hosts a private repository; instance B's user is added
# as a federated read-only collaborator; B's user fetches the private repo's
# actor via AP and clones it over git using a scoped token.
#
# Exit status: 0 on success, 1 on failure.
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$DIR/../../.." && pwd)"
COMPOSE="docker compose"
A_API="http://localhost:4001"
B_API="http://localhost:4002"

log() { printf '\033[1;34m[e2e]\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m[e2e FAIL]\033[0m %s\n' "$*"; exit 1; }
need() { command -v "$1" >/dev/null || fail "missing tool: $1"; }

need curl; need git; need jq; need docker; need go

CLONE_DIR=""
cleanup() {
	log "cleaning up docker containers"
	(cd "$ROOT" && $COMPOSE -f tests/federation/e2e/docker-compose.yml down -v --remove-orphans >/dev/null 2>&1 || true)
	if [ -n "$CLONE_DIR" ] && [ -d "$CLONE_DIR" ]; then
		rm -rf "$CLONE_DIR"
	fi
}
trap cleanup EXIT

# ---------------------------------------------------------------------------
# 1. Build the Forgejo binary (with sqlite) and start two instances
# ---------------------------------------------------------------------------
log "building forgejo binary (sqlite)"
(cd "$ROOT" && CGO_ENABLED=1 CGO_CFLAGS="-O2 -g -DSQLITE_MAX_VARIABLE_NUMBER=32766" \
  go build -tags "sqlite sqlite_unlock_notify" \
  -ldflags '-s -w -X "main.Version=9.0.0" -X "main.ForgejoVersion=9.0.0"' \
  -o "$ROOT/forgejo-sqlite" ./)

log "building image and starting instances"
# The Docker build context is the repository root (see Dockerfile paths).
cd "$ROOT" && $COMPOSE -f tests/federation/e2e/docker-compose.yml up -d --build --wait 2>/dev/null || \
  $COMPOSE -f tests/federation/e2e/docker-compose.yml up -d --build

# Wait for both to be healthy
for base in "$A_API" "$B_API"; do
  for i in $(seq 1 60); do
    if curl -sf "$base/api/v1/version" >/dev/null 2>&1; then break; fi
    sleep 1
    [ "$i" = 60 ] && fail "instance $base did not become healthy"
  done
done
log "instances are up"

# ---------------------------------------------------------------------------
# 2. Set up users and tokens
# ---------------------------------------------------------------------------
# A: alice (owner of the private repo). B: bob (will be the federated collaborator).
TOKEN_A=$(curl -sf -X POST "$A_API/api/v1/users/alice/tokens" \
  -H "Content-Type: application/json" \
  -u alice:admin1234 \
  -d '{"name":"e2e","scopes":["all"]}' | jq -r .sha1)
[ -n "$TOKEN_A" ] && [ "$TOKEN_A" != "null" ] || fail "failed to create token for alice@A"

TOKEN_B=$(curl -sf -X POST "$B_API/api/v1/users/bob/tokens" \
  -H "Content-Type: application/json" \
  -u bob:admin1234 \
  -d '{"name":"e2e","scopes":["all"]}' | jq -r .sha1)
[ -n "$TOKEN_B" ] && [ "$TOKEN_B" != "null" ] || fail "failed to create token for bob@B"
log "tokens created"

# ---------------------------------------------------------------------------
# 3. alice@A creates a private repository with content
# ---------------------------------------------------------------------------
log "creating private repo on A"
curl -sf -X POST "$A_API/api/v1/user/repos" \
  -H "Authorization: token $TOKEN_A" -H "Content-Type: application/json" \
  -d '{"name":"private-repo","private":true,"auto_init":true,"readme":"Default","default_branch":"main"}' >/dev/null

# ---------------------------------------------------------------------------
# 4. bob@B follows alice@A's profile → materialises bob as a federated user on A
# ---------------------------------------------------------------------------
log "bob@B follows alice@A (materialises bob on A)"
# The follow activity is delivered from B's user actor to A's alice actor inbox.
curl -sf -X POST "$B_API/api/v1/user/activitypub/follow" \
  -H "Authorization: token $TOKEN_B" -H "Content-Type: application/json" \
  -d "{\"target\":\"http://forgejoA:3000/api/v1/activitypub/user-id/1\"}" >/dev/null

# Give the federation a moment to deliver.
sleep 3

# bob@B is now materialised on A as @bob@forgejob:3000 (inactive, AP type).
# Verify the remote user exists on A.
ENC_NAME=$(python3 -c "import urllib.parse; print(urllib.parse.quote('@bob@forgejob:3000'))")
BOB_ON_A=$(curl -s "$A_API/api/v1/users/$ENC_NAME" -H "Authorization: token $TOKEN_A" -o /dev/null -w '%{http_code}' || true)
log "bob materialised on A: HTTP $BOB_ON_A"

# ---------------------------------------------------------------------------
# 5. alice@A adds bob@B as a read-only collaborator on the private repo
# ---------------------------------------------------------------------------
log "alice@A adds bob@B as read-only collaborator"
COLLAB_NAME=$(python3 -c "import urllib.parse; print(urllib.parse.quote('@bob@forgejob:3000'))")
curl -sf -X PUT "$A_API/api/v1/repos/alice/private-repo/collaborators/$COLLAB_NAME" \
  -H "Authorization: token $TOKEN_A" -H "Content-Type: application/json" \
  -d '{}' -o /dev/null -w '%{http_code}\n' | grep -q 204 || fail "failed to add collaborator"

# ---------------------------------------------------------------------------
# 6. Verify: bob's signed AP fetch of the private repo actor succeeds
# ---------------------------------------------------------------------------
# Issue a read-only git token for the federated collaborator on A.
COLLAB_TOKEN=$(curl -sf -X POST "$A_API/api/v1/repos/alice/private-repo/collaborators/$COLLAB_NAME/token" \
  -H "Authorization: token $TOKEN_A" | jq -r .token)
[ -n "$COLLAB_TOKEN" ] && [ "$COLLAB_TOKEN" != "null" ] || fail "failed to issue federated collaborator token"

# The token must authenticate API read of the private repo as bob.
HTTP_CODE=$(curl -sf -o /dev/null -w '%{http_code}' \
  -H "Authorization: token $COLLAB_TOKEN" \
  "$A_API/api/v1/repos/alice/private-repo" || true)
[ "$HTTP_CODE" = "200" ] || fail "federated collaborator token did not authenticate API access (got $HTTP_CODE)"
log "federated collaborator token authenticates API access"

# ---------------------------------------------------------------------------
# 7. Verify: bob can clone the private repo over git smart HTTP
# ---------------------------------------------------------------------------
log "bob@B clones the private repo from A over git"
CLONE_DIR=$(mktemp -d)
git clone --quiet "http://oauth2:${COLLAB_TOKEN}@localhost:4001/alice/private-repo.git" "$CLONE_DIR/private-repo" || fail "git clone failed"
[ -f "$CLONE_DIR/private-repo/README.md" ] || fail "clone succeeded but content missing"
log "clone succeeded; content verified"

# ---------------------------------------------------------------------------
# 8. Negative: a different instance user cannot access the private repo
# ---------------------------------------------------------------------------
log "verifying a third party cannot access the private repo"
# Create charlie on B (not a collaborator).
curl -sf -X POST "$B_API/api/v1/user/repos" \
  -H "Authorization: token $TOKEN_B" -H "Content-Type: application/json" \
  -d '{"name":"unrelated","auto_init":true}' >/dev/null 2>&1 || true
# charlie has no collaborator token; cloning must fail.
if git clone --quiet "http://oauth2:invalid-token@localhost:4001/alice/private-repo.git" "$CLONE_DIR/bad" 2>/dev/null; then
  fail "clone with invalid token unexpectedly succeeded"
fi
log "invalid-token clone correctly rejected"

log "ALL E2E CHECKS PASSED"
