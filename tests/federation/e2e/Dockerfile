# Cross-instance federation e2e: two Forgejo containers on a shared network.
# Instance A hosts a private repository; instance B's user is added as a
# federated read-only collaborator; B's user clones A's private repo over git
# with a scoped token.
#
# Build context: the repository root (see run-e2e.sh).

FROM debian:12-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
	ca-certificates git git-lfs curl sqlite3 && rm -rf /var/lib/apt/lists/*

RUN useradd -r -m -d /home/git git && mkdir -p /data && chown -R git:git /data

# The binary is built without embedded assets (no bindata tag); ship the
# options directory so locales are readable from disk.
COPY options /app/options

COPY forgejo-sqlite /usr/local/bin/forgejo
COPY tests/federation/e2e/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/forgejo /usr/local/bin/entrypoint.sh

USER git
EXPOSE 3000
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]