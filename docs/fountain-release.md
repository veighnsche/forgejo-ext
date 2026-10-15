# Fountain releases and downstream contract

Fountain maintains the Forgejo fork, executable, native frontend, extension host,
SDK, tests and container runtime. Soda owns its extension, presentation,
configuration and appliance packaging. A Fountain release needs only this
repository; no Soda checkout, ISO build or appliance qualification is involved.

## Entry point

Use a clean, committed checkout on native Linux x86_64. Release versions have the
form `15.0.10-fountain.1`: the upstream base followed by an independent Fountain
release number. A new source change needs a new version; reuse an existing image
digest across Soda releases. Never replace a published version or use `latest`.

Local validation publishes nothing:

```sh
bash tools/fountain-release.sh 15.0.10-fountain.1 "$(git rev-parse HEAD)" \
  "$HOME/fountain-release-15.0.10-fountain.1"
```

Prerequisites: Go 1.26.9 (selected by the script), Node as pinned in
`.node-version`, npm 11.11.0 as selected by `package.json`, Git, GCC for native SQLite tests, GNU make/tar, gzip, jq and
a working native Docker engine with BuildKit. `CONTAINER_ENGINE` may name a
Podman wrapper with equivalent build/run/cp/inspect commands. Configure that
wrapper's storage, run root, networking and SELinux mounts for the workstation.
Output, temporary files and caches must resolve under `/home`; configure the
container engine's storage there as well. The output directory must not exist.
Use a short `TMPDIR` under `/home` for Unix socket fixtures; by default the
script allocates a unique short directory directly below the home directory.
The script leaves its isolated source, logs, disposable data and artifacts there
for inspection; it removes only containers it created, never shared caches.

The script archives the exact HEAD commit before generation. It runs
`make fountain-check-release`, sharing the existing upstream source checks,
frontend lint/tests, strict browser SDK checks, extension/setting/CLI/example
tests and standalone SDK tests before the expensive container build. Formatting
and tidy gates compare both worktree and index against an isolated Git baseline.
The complete hosted push gate additionally includes broad Go units, PostgreSQL,
SQLite integration and a native browser journey; see
[Fountain testing](fountain-testing.md). Local release checks do not replace that
qualification. The single container
build uses the existing Dockerfile and Makefile's frontend, bindata and static
executable recipes. Its default upstream platforms and rootless recipe remain
available; this release track publishes only native `linux/amd64`.

The image test uses its immutable configuration ID, normal s6 startup and
disposable SQLite state. It checks labels, executable/API versions, packaged JS,
CSS and license notices, the stopped-host extension CLI, a real handshake with
`contrib/extensions/example`, required-package readiness, and restart with the
same data. The example is compiled independently against the checked-in SDK.
This is a packaging/host smoke test; Soda's product and appliance tests remain
Soda's responsibility.

The workflow uses npm/Go caches, BuildKit dependency and runtime layers, and Go
compiler/module cache mounts. It pushes the already tested image by configuration
ID using `docker push`, without a second build. gzip level 1 favors overall
release time for downloadable files; normal container gzip layers stay compatible
with Docker/GHCR and unchanged layers are reused. No xz level-9 recompression or
parallel multi-platform release is added.

Dispatch on the same maintained branch so subsequent releases can restore the previous
release's caches. Cache mounts are saved separately with the
[action recommended by Docker](https://docs.docker.com/build/ci/github-actions/cache/#cache-mounts);
the layer cache alone does not save compiler/module mount contents. Cache keys
include the source commit, with a compatible restore prefix, so newly compiled
objects are saved after each source change.

## Publication

The inherited `.forgejo` release workflows are role-gated Codeberg infrastructure
jobs, including cross-platform/rootless builds and cascading downstream tests.
They are preserved. `.github/workflows/fountain-release.yml` is Fountain's GitHub
publication entry point and reuses the same build recipes, rather than adopting
the Codeberg publishing infrastructure or creating another compiler pipeline.

After separately authorizing publication, push a reviewed tag at the exact
maintained-branch HEAD and dispatch from that branch:

```sh
gh workflow run fountain-release.yml --ref codex/fountain-extensions \
  -f tag=v15.0.10-fountain.1
```

The workflow refuses a tag dispatch or a mismatch between the
dispatch commit and the existing tag,
an existing GitHub release, an existing image version tag, registry errors, or a
private source repository. It creates a draft release with all assets before
publishing it. A failed publication can leave an image or draft; inspect those
explicitly, do not overwrite them or assume the release completed atomically.
Create a new version for a subsequent attempt. GitHub publication and permission
behavior require a real authorized run; local tests cannot establish them.

First-release prerequisites:

- A public GitHub source repository, Actions enabled, workflow registered on its
  default branch and present on the maintained branch, the reviewed tag at that
  maintained branch's dispatch commit, and `GITHUB_TOKEN` permissions for
  contents/packages. The example uses this checkout's maintained branch; choose
  the branch carrying the intended Fountain source when that changes.
- Configure the `fountain-release` GitHub environment with required reviewer
  approval if desired. Configure package access for this repository if an
  existing GHCR package is reused.
- Explicit owner authorization to push the commit/tag, publish the release/image
  and set package visibility. GHCR initially creates packages as private: the
  owner must make `ghcr.io/veighnsche/fountain` public after the first push and
  verify anonymous pull by digest. The workflow never changes visibility.

No personal access token is needed for the release workflow. Checkout does not
persist credentials; registry login uses the workflow token, and publication
passes the GitHub token through the environment. Never put registry credentials
or app secrets in source, image build arguments, release assets or CLI arguments.

## Public artifacts and provenance

For version `V`, publish exactly:

- `ghcr.io/veighnsche/fountain:V`, a native Linux amd64 image. The authoritative
  consumer reference is `ghcr.io/veighnsche/fountain@sha256:…` in `release.json`.
- GitHub release tag `vV`, with these attached files:
  `fountain-V-linux-amd64.gz`, `fountain-V-source.tar.gz`,
  `forgejo-src-V+gitea-1.22.0.tar.gz`, `LICENSE`, `SHA256SUMS`, `release.json`.

The executable is copied from the validated image. The exact Git archive includes
the complete maintained tree, SDK, lockfiles and third-party modifications; its
tar commit ID binds it to `source_commit`. The prepared archive reuses upstream
`sources-tarbal`: vendored Go dependencies, generated bindata, SDK and the frontend
recovered from the shipped executable, plus the stored full version. It can be
rebuilt with the documented upstream Makefile toolchain. It does not package
workstation credentials, Git history, npm caches or node_modules. Frontend
dependency sources are identified by the frozen npm lock and available from npm;
their notices are generated by the upstream webpack recipe.

Preserve the GPL-3.0-or-later license, per-file notices, third-party sources and
embedded `/assets/licenses.txt`. The image also carries
`/usr/share/licenses/fountain/LICENSE`. Downstreams redistributing Fountain must
provide the matching source and notices alongside their binaries/images, including
their own modifications. Keep both source archives available for each release.

`release.json` schema 1 binds the version, full source commit, source filenames and
SHA-256 checksums, image manifest digest, validated image configuration digest,
platform, extension protocol and browser contract. `SHA256SUMS` covers every
attached payload including metadata (excluding the checksum file itself). The
publisher verifies that the pushed manifest references the configuration tested
locally. OCI labels carry the same version, source URL and source revision. This
is concise release provenance, not a claim of bit-for-bit reproducible builds or
a signed attestation.

## Soda consumption contract

Pin `release.json.image` in Soda's release inputs; pull and use that exact digest
as `BASE_IMAGE`. Verify `SHA256SUMS` and the source commit/SDK contracts before
adopting a release. Replace Soda's Fountain compilation and binary replacement
with this base image; retain Soda's small presentation/setup layer and extension
build. Keep Fountain source/notices in Soda's redistributed source materials.
No change to Soda is part of this track.

| Interface | Release image contract |
| --- | --- |
| Executable | `/app/gitea/gitea`; `/app/gitea/forgejo-cli` aliases it. |
| CLI wrappers | `/usr/local/bin/gitea` and `/usr/local/bin/forgejo` set work/custom defaults and execute the real binary. Preserve the wrapper when adding branding. |
| Startup | `/usr/bin/entrypoint`, then `/usr/bin/s6-svscan /etc/s6`; `/etc/s6/gitea/setup` generates config and runs `environment-to-ini`, `/etc/s6/gitea/run` starts the host as `git`. |
| Identity | UID/GID 1000 by default; upstream `USER_UID`/`USER_GID` controls remain. Administrative CLI operations run as the data owner. |
| Persistent state | `/data`; default work/custom/app-data `/data/gitea`, config `/data/gitea/conf/app.ini`, SQLite `/data/gitea/gitea.db`, repositories `/data/git/repositories`. |
| Configuration | `FORGEJO__section__KEY` / `GITEA__section__KEY`, including supported `__FILE` secret inputs, through `/usr/local/bin/environment-to-ini`. Soda retains explicit `APP_DATA_PATH` when changing `GITEA_CUSTOM`/`FORGEJO_CUSTOM`. |
| Networking | HTTP 3000 and SSH 22; `/api/healthz` checks the initialized service. Disable SSH when the appliance supplies a different SSH service. |
| Frontend | Built assets embedded with bindata, served at `/assets/`; Soda-owned custom templates/assets use the normal custom path. |
| Extensions | Disabled by default. Set `[extensions] ENABLED=true`, absolute `PATH` (default `/data/gitea/extensions`) and `REQUIRED_IDS` for packages that must load. Stop the host before `forgejo extensions install/list/status/enable/disable/…`. |

Protocol and browser mount contract are currently both 1, defined in
`sdk/manifest.go` and `sdk/contract.go`; publish their values with the host release.
Pin the SDK source at the same Fountain commit, including
`sdk/browser/extension-contract.d.ts`. Matching integers alone do not guarantee
that a particular extension's capabilities or APIs are supported: verify its
manifest and required behavior against the selected host. Packages must contain
an executable compatible with Linux amd64/musl (or a static binary), readable
assets and a valid manifest. Fountain runs trusted packages as the host user;
installation, activation, private data and socket lifecycle remain host-owned.
No Soda branding, configuration or extension is preinstalled in Fountain.

## Extension lifecycle contract (RCX-1 I2)

Generic browser-managed download, enable and disable. The implementation is
generic: no product-specific logic lives in Fountain; Soda supplies its own
declaration, dependencies and restricted host effects.

| Interface | Definition |
| --- | --- |
| Lifecycle operations | `sdk/lifecycle.go`: `observe`, `activate`, `deactivate` requests identify package, pinned version and operation; results report operation, observed state (`disabled`, `activating`, `enabled`, `deactivating`, `failed`), progress and a bounded error. Failed results with `unconfirmed` must be re-observed before their effects are retried. |
| Trusted catalog | `sdk/catalog.go`: versioned package set; each entry carries ID, version, HTTPS archive URL, exact bytes, SHA-256, compatible Fountain protocol/browser contract, required flag and capabilities. `modules/extensions/catalog.go` verifies size and digest, extracts the gzip archive inside staging, and validates manifest and package files before activation. |
| Live manager | `services/extensions/lifecycle.go`: `Observe` (no effects), `Activate` (persists requested state, starts the backend, opens admission only after effects and observation), `Deactivate` (closes new admission, invokes the registered external teardown hook, verifies it, then stops the backend). Required packages are always refused. `Snapshot` reports running IDs/versions/installations for maintenance. |
| Persistent intent | Fountain alone persists requested state through the `.disabled` marker; `modules/extensions/intent.go` records the last operation observation (never authority). Restart re-observes interrupted operations without retrying their effects. Installation UUIDs, private data, admission controls and package locking are reused unchanged. |
| Native admin settings | `GET /admin/extensions` plus `POST /admin/extensions/enable/{id}` and `POST /admin/extensions/disable/{id}`, behind the live site-admin guard with native session and cross-origin protection. Enable installs from the declared catalog when needed; disable keeps required packages isolated. No whole-Fountain restart and no hidden CLI step. |
| Wire fixture | JSON round-trips in `sdk/lifecycle_catalog_test.go`; acquisition fixtures in `modules/extensions/catalog_intent_test.go`; live lock/teardown/isolation/interruption proof in `services/extensions/lifecycle_test.go`; admin/CSRF/denied-actor fixtures in `routers/web/extensions/admin_test.go`. Downstreams import these SDK definitions instead of creating parallel DTOs. |
