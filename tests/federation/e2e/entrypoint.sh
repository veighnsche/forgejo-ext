#!/bin/bash
# Per-instance entrypoint: writes app.ini from env, migrates, creates the
# initial user, starts Forgejo.
set -e

INSTANCE=${INSTANCE:-A}
HTTP_PORT=${HTTP_PORT:-3000}
ROOT_URL=${ROOT_URL:-http://localhost:3000/}
ADMIN_USER=${ADMIN_USER:-admin}
ADMIN_PASSWORD=${ADMIN_PASSWORD:-admin1234}
ADMIN_EMAIL=${ADMIN_EMAIL:-admin@example.com}
PEER_HOSTS=${PEER_HOSTS:-}

mkdir -p /data/app/conf /data/git/repositories /data/log
cat > /data/app/conf/app.ini <<EOF
APP_NAME = Forgejo E2E ${INSTANCE}
RUN_MODE = prod

[server]
APP_DATA_PATH = /data
STATIC_ROOT_PATH = /app
HTTP_PORT = ${HTTP_PORT}
ROOT_URL = ${ROOT_URL}
DOMAIN = ${ROOT_URL#*://}
DOMAIN = \${DOMAIN%%:*}
SSH_DOMAIN = \${DOMAIN%%:*}
DISABLE_SSH = true
LFS_START_SERVER = true

[database]
DB_TYPE = sqlite3
PATH = /data/forgejo-${INSTANCE}.db

[repository]
ROOT = /data/git/repositories

[security]
INSTALL_LOCK = true
INTERNAL_TOKEN = e2e-internal-token-${INSTANCE}-0000000000000000000000000000000000000000000000000000000000

[service]
REGISTER_EMAIL_CONFIRM = false
ENABLE_NOTIFY_MAIL = false

[migrations]
ALLOW_LOCALNETWORKS = true
ALLOW_UNENCRYPTED = true

[federation]
ENABLED = true
INSECURE_ALLOW_INVALID_HOSTS = true
ALLOWED_HOSTS = ${PEER_HOSTS}

[log]
ROOT_PATH = /data/log
MODE = console
LEVEL = info
EOF

forgejo --config /data/app/conf/app.ini migrate

# Create the admin user (idempotent: ignores "already exists").
forgejo --config /data/app/conf/app.ini admin user create \
	--username "${ADMIN_USER}" --password "${ADMIN_PASSWORD}" --email "${ADMIN_EMAIL}" \
	--must-change-password=false 2>/dev/null || true

# Create a second regular user for cross-instance interactions.
if [ -n "${REMOTE_USER:-}" ]; then
	forgejo --config /data/app/conf/app.ini admin user create \
		--username "${REMOTE_USER}" --password "${REMOTE_PASSWORD:-remote1234}" --email "${REMOTE_EMAIL:-remote@example.com}" \
		--must-change-password=false 2>/dev/null || true
fi

exec forgejo --config /data/app/conf/app.ini web
