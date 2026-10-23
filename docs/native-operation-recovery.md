# Offline native-operation recovery

`admin native-operation status` reads the native reservation. `recover` reconciles
the exact held owner and generation against repository and database evidence; it
does not replay the interrupted write. Run both commands through the installed
Fountain executable, with the same configuration, database and repository storage
as the stopped server.

Stop every writer and inhibit restart before recovery: web/SSH ingress, queues,
scheduled/admin commands and surviving descendants. Keep the database available.
The file `$AppDataPath/nativeop-offline` inhibits new claims; it neither proves
that writers stopped nor releases an existing owner. Keep inhibition in place
through reconciliation. There is no force unlock or timeout release.

## Soda appliance invocation

This recipe applies to Soda's rootful Podman appliance service, whose container
is `soda-forgejo` and whose systemd unit is `forgejo.service`. Use the installed
Soda image, including its presentation/setup layer. Capture its immutable local
image ID **before** stopping the unit: Quadlet removes the service container on
stop, so a later `podman exec` cannot supply the offline command.

Run these commands in one operator Bash session on the appliance:

```bash
fountain_image=$(sudo podman inspect --format '{{.Image}}' soda-forgejo)
if [[ $fountain_image =~ ^[0-9a-f]{64}$ ]]; then fountain_image="sha256:$fountain_image"; fi
[[ $fountain_image =~ ^sha256:[0-9a-f]{64}$ ]] || exit 1
sudo podman image exists "$fountain_image" || exit 1

sudo /usr/bin/soda-forgejo-domain stop || exit 1
sudo /usr/bin/soda-forgejo-domain inhibit || exit 1
sudo /usr/bin/soda-forgejo-domain status || exit 1
```

Confirm `forgejo.service` is inactive and runtime masked, the service container
is absent and the marker is present. Retire any separately launched native
commands or descendants before continuing; the service check does not discover
processes launched outside its unit/container. The runtime mask lasts until
reboot. Do not reboot during this procedure.

The retained deployment mappings are:

| Item | Offline container value |
| --- | --- |
| Executable | `/app/gitea/gitea` |
| Data owner | UID/GID `1000:1000` (`git` in the installed image) |
| Working directory and work path | `/data/gitea` |
| Configuration | `/data/gitea/conf/app.ini` |
| Custom path | `/usr/share/soda/forgejo` |
| AppDataPath / offline marker | `/data/gitea` / `/data/gitea/nativeop-offline` |
| Retained data and repositories | `/var/lib/soda/forgejo:/data` |
| Database secret | `/etc/soda/postgres/forgejo.passwd:/etc/forgejo/db_passwd:ro` |
| Database network | `systemd-soda` (Quadlet's `soda.network`) |

The installed `app.ini` contains the settings produced by normal startup,
including `APP_DATA_PATH=/data/gitea` and PostgreSQL at `soda-postgres:5432` with
database/user `forgejo` and `PASSWD_URI=file:/etc/forgejo/db_passwd`. Reuse that
file and secret without printing them. This command bypasses the web entrypoint
and s6 setup, so it does not rerender configuration or start writers. Confirm
the retained configuration describes this deployment before continuing; use its
actual additional mounts/network if the operator changed repository or database
storage. Do not substitute a fresh empty data directory.

```bash
fountain_offline() {
  sudo podman run --rm --pull=never \
    --network=systemd-soda \
    --user=1000:1000 \
    --workdir=/data/gitea \
    --cap-drop=all \
    --security-opt=no-new-privileges \
    --volume=/var/lib/soda/forgejo:/data:Z \
    --volume=/etc/soda/postgres/forgejo.passwd:/etc/forgejo/db_passwd:ro,Z \
    --entrypoint=/app/gitea/gitea \
    "$fountain_image" \
    --work-path=/data/gitea \
    --custom-path=/usr/share/soda/forgejo \
    --config=/data/gitea/conf/app.ini \
    admin native-operation "$@"
}

fountain_offline status
```

No HTTP/SSH ports, host socket, extension callback socket, catalog or maintenance
directory is mounted: the CLI reads configuration and native evidence and does
not start extensions. The image's `git` account and retained data ownership must
agree; do not fix a mismatch by recursively changing the retained volume.

`status` prints `state`, `revision` and `inhibited`; when held it also prints the
exact `owner`, `generation`, `kind` and `family`. It excludes verifier secrets and
scope payloads. Require `inhibited: true`. If held, copy the reported identity:

```bash
read -r -p 'Exact held owner: ' fountain_owner
read -r -p 'Exact held generation: ' fountain_generation
fountain_offline recover --owner "$fountain_owner" --generation "$fountain_generation"
fountain_recovery_exit=$?
printf 'recovery exit: %s\n' "$fountain_recovery_exit"
fountain_offline status
```

Interpret the printed verdict together with its effect, reason and checks:

| Result | Meaning and next action |
| --- | --- |
| `released`, exit 0 | Exact owner released after a known effect was established. Preserve any incomplete/intervention result reported for the original operation. |
| `idle`, exit 0 | No reservation owner remains; no interrupted write was replayed. |
| `fenced`, exit 3 | Owner remains held: identity mismatch, unaccounted effects or uncertain attribution needs intervention. Keep inhibition; do not guess another owner or generation. |
| Other error / nonzero exit | Configuration, database, filesystem or invocation failed. Keep inhibition and resolve that specific failure. |

Only after successful reconciliation and an idle readback, lift inhibition and
restart through the deployment controls:

```bash
sudo /usr/bin/soda-forgejo-domain lift || exit 1
sudo /usr/bin/soda-forgejo-domain start || exit 1
sudo /usr/bin/soda-forgejo-domain status
```

Check the original operation through its supported receipt/readback path after
restart. Recovery of the reservation is distinct from completion of every
deferred native effect.

## Other deployments

Use the same image and native data user as that deployment. Supply explicit
`--work-path`, `--custom-path` and `--config`, preserve its AppDataPath marker,
database secret/network, repositories and any external retained mounts, and
invoke `/app/gitea/gitea` directly. The standalone Fountain image defaults to
custom path `/data/gitea`; Soda's custom path above belongs to its derived image.
Deployment service controls own the stop, inhibition and restart sequence.

## Qualification boundary

The optional disposable image fixture runs without a retained appliance or
external database. Supply an exact local image configuration ID and a fresh
output directory under `/home`; the selected local PostgreSQL 17 image ID is
recorded beside it:

```bash
CONTAINER_ENGINE=podman bash tools/fountain-native-recovery-test.sh \
  sha256:EXACT_LOCAL_IMAGE_CONFIGURATION_ID /home/OPERATOR/NEW-RECOVERY-PROOF
```

It starts only its own isolated PostgreSQL container/network, initializes fresh
fixture storage, and checks the actual image CLI's uninhibited refusal, idle
readback/recovery, exact-owner/generation refusals, release of a known authority
effect and unknown-family fencing. SQL seeds explicitly synthetic crash-prefix
rows in that disposable database; this is not an operator recovery method or
proof of an actual interrupted native writer. Cleanup removes only its fixture
container/network and preserves logs and image identities in the output path.

Service/model recovery tests cover known effects and fenced uncertainty in
disposable databases and repository fixtures. The focused
`TestIssueGraphProcessDeathRecovery` test submits an issue graph in a separate
process against persistent SQLite storage, observes its before/after-primary
barrier, kills and reaps that owned child with `SIGKILL`, then reopens the database
without reloading fixtures. It checks exact recovery and replay without duplicate
issues/dependencies; notification completion remains `needs_intervention` after
an interrupted committed primary. This Linux-only proof covers the issue-graph
process boundary with SQLite. Actual merge/publication process death and other
database engines remain separate evidence. These checks do not qualify a retained
appliance's service controls, SELinux labels, database network or installed
image. The Soda invocation above is derived from its packaged Quadlet,
Containerfile, setup and domain-control sources. Installed acceptance still
requires a disposable appliance observation of this exact command after whole
writer-domain stop; a source test or idle CLI smoke is narrower evidence.
