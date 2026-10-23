# Fountain testing and protected pushes

The maintained branch is `codex/fountain-extensions`. Its required GitHub Actions
check is `Fountain CI`, supplied by `.github/workflows/fountain-ci.yml`.
Qualification runs on every maintained-branch push, pull request targeting that
branch, and `ci/fountain-FULL_COMMIT_SHA` tag push. There are no path-based
exemptions. The inherited upstream `.forgejo` workflows serve a different CI
installation.

## Qualify an exact candidate

Review and commit the complete intended changes, then run:

```sh
bash tools/fountain-push.sh
```

This command creates the immutable `ci/fountain-FULL_COMMIT_SHA` tag, checks the
exact workflow/SHA and GitHub Actions app, and ordinarily fast-forwards
`codex/fountain-extensions` only after success. The entire command has a hard
300-second timeout, including network waits. It reserves time for publication,
reports failure promptly, and never bypasses protection. If a timeout interrupts
publication, inspect the origin branch before retrying. A shorter budget is
available through `FOUNTAIN_PUSH_TIMEOUT_SECONDS`; values above 300 are rejected.

For manual operation, push the exact candidate tag and inspect `Fountain push CI`
before pushing that same SHA to the maintained branch. Do not wait indefinitely
for a pending result or replace a failed result with a success.

Branch protection requires the `Fountain CI` check from GitHub Actions, strict
status checks and administrator enforcement. It disallows force pushes and
deletion. Human review is not an additional branch-protection requirement.
Scratch refs remain available for development and qualification.

A rebased, amended or regenerated commit needs its own qualification. Never move
a qualification tag to different bytes. A PR run may test a synthetic merge
commit; its badge does not qualify a different directly pushed commit. After a
successful push, confirm the maintained-branch admission run too. It reuses only
the same full SHA's successful qualification-tag run of this workflow. PRs,
qualification tags and explicit reruns always execute the push checks. An absent,
failed, pending or mismatched receipt executes checks instead of admitting reuse.

The upstream default branch remains `v17.0/forgejo`. Push-triggered workflows
support the candidate procedure without changing it. Manual dispatch is not the
bootstrap path. A local hook is optional feedback and is not the enforcement
boundary.

## Five-minute push budget

`Fountain CI` is the required push check. Its complete workflow has a 300-second
admission budget, including checkout, setup and all five lanes. The aggregate
rejects expired and future timestamps even when the lanes passed. Each lane also
has a four-minute job limit. A timeout fails closed; it never becomes a success.
GitHub runner queue delays are outside local control and do not qualify a commit.

| Push profile (`tools/fountain-check.sh`) | Required scope |
| --- | --- |
| `push-source` | Repository Go/template formatting; independent SDK and patched-session module integrity, formatting and vet. |
| `push-unit` / `push-native` / `push-race` | Parallel jobs: Fountain SDK, extension lifecycle, settings, sessions, queues, native entry points, pull/automerge and native-operation tests; race checks for the reservation model, native service, pull service, queue and patched session. All three jobs must pass. |
| `push-database` | SQLite native reservation contracts and real PostgreSQL authority, creation recovery and migration tests. The self-contained PG migration contract compiles the real migration sources in Go file-list mode, without the unrelated historical suite’s server fixture. Skipped, empty, malformed or failed PG results fail. |
| `push-browser` | Full frontend lint and nonempty, unfocused unit suite; strict SDK/example TypeScript and production frontend build. |
| `push-packaging` | Release-script Bash syntax, checksum-pinned Hadolint at warning severity, and pinned Actionlint for both CI workflows. |

Fresh qualification requires every lane to report `success`. Reuse requires a
successful exact-commit admission lookup and every lane to report `skipped`.
Mixed, missing, failed, cancelled and neutral results fail. Compiler/download
caches do not replace test execution; required Go runs use `-count=1`.

## Deep regression CI

`.github/workflows/fountain-full-ci.yml` preserves the complete source profile
under a separate `Fountain full CI` check. It runs after maintained-branch pushes,
on PRs targeting that branch, and on `ci/fountain-full-FULL_COMMIT_SHA` tags.
**It does not block routine pushes.** A fast push pass is not a full regression or
release qualification receipt. Inspect deep CI before qualifying a release.

The existing `fountain-check-source`, `unit`, `database`, `browser` and `packaging`
Make targets retain full upstream generated/dependency/security/Go/Swagger checks,
the complete root Go unit selection, full extension-manager race tests, all
SQLite integrations and migrations, mandatory PostgreSQL tests, and the real
example-extension Chromium journey. The deep aggregate requires every lane to
succeed. Long suites remain visible separately instead of extending push admission.

The upstream Go unit selection excludes integration, migration and E2E packages;
deep CI has dedicated lanes for those boundaries. Optional upstream Redis,
Elasticsearch, MinIO, provider and platform matrices remain outside this Linux
amd64 profile. Downstream Soda browser joins remain opt-in.

The integration baseline uses asynchronous fixture queues for deferred push,
PR, automerge and Actions work. Sequential fixture helpers wait for queued completion;
dedicated contention tests retain held reservations. Runtime PR sweeps retain
work across contention and admit only the private head refs belonging to their
bound source repository and branch, including fork PRs. Nested ref recording
preserves that binding, and bounded waits do not replay callbacks after they start.
Automerge retains its schedule and queue item when the merge reservation refuses
before effects. It consumes the schedule only after a successful merge; callback
errors after entry never authorize replay.

The example journey exercises panel preservation across native page navigation,
native branch controls, presentation context, admin disable/enable, denied actors,
guest rejection and logout revocation. Existing Go tests retain detailed manager,
HTTP and WebSocket coverage. Full OCI image builds, s6 startup and persistence
smoke checks belong to [release qualification](fountain-release.md).

## Run locally in disposable source

Upstream checks rewrite source and stage generated SVGs. Use a fresh committed
checkout or archive with a Git baseline; keep unrelated work outside it. The
entry point requires `FOUNTAIN_CHECK_ISOLATED=1` as an explicit acknowledgement
of this constraint. It checks both worktree and index before and after the lane,
and rejects unexpected untracked source.

Prerequisites are Linux amd64, Go 1.26.10, Node from `.node-version`, npm from
`package.json`, Git, GCC, GNU make, and the selected lane's tools. Database and
browser integration use checksum-verified Git LFS v3.8.0, installed by the shared
CI setup. Temporary paths
must be short and under `/home`, because native Unix sockets have a bounded path
length. Use the existing Go/module caches under `/home` and put generated test
results in ignored `.artifacts/fountain-ci/`.

For example, from the canonical repository:

```sh
candidate=$(git rev-parse HEAD)
scratch=$(mktemp -d "$HOME/fc.XXXXXXXX")
git clone --no-hardlinks --no-checkout . "$scratch/source"
git -C "$scratch/source" checkout --detach "$candidate"
cd "$scratch/source"
export TMPDIR="$scratch/tmp"
mkdir "$TMPDIR"
npm ci --no-audit --no-fund
FOUNTAIN_CHECK_ISOLATED=1 make fountain-check-source
```

Choose a writable parent under `/home` for the scratch directory. Keep its path
short; a long nested scratch path can exceed the socket limit even though the
checkout itself fits. Use the other targets from that same clean checkout.
Hadolint is v2.15.1, SHA-256
`c7187db94eeeeca956519a6af171adc31453941a1e777961f6e680f697c8c507`;
Actionlint is v1.7.12. The workflow contains the exact installation commands.
The browser lane requires the locked Playwright Chromium build and its system
libraries (`npx --no-install playwright install --with-deps chromium`).

The database lane requires `FORGEJO_TEST_PG_DSN` for a disposable PostgreSQL 17
database. The fixtures delete rows and drop native-operation tables. Use isolated
fixture storage, never a retained database. The hosted service uses synthetic
credentials and a loopback-only port. Packages run sequentially against it to
avoid competing schema resets. Required JSON test results remain in
`.artifacts/fountain-ci/`.

The separate deep-regression database job has a 150-minute limit, including builds,
migrations and PostgreSQL checks. The complete SQLite integration binary has a 120-minute deadline.

## Lint and release boundaries

Fountain retains the upstream model/service import naming conventions and lint
rules. Mechanical corrections are ordinary source changes. Exceptions stay next
to the exact declaration/import with their reason: standard-library strict or
persisted JSON, optional-result absence, common operation callback signatures,
and short Unix-socket/external-fixture paths. The independently built example
imports only the public SDK. Do not substitute the host's limited JSON wrapper
for strict native protocol decoding or create a broad generated failure baseline.

`lint-backend-analysis` runs the complete Go lint, vet, error-type and supporting
backend checks. Upstream's additional dead-code comparison is scoped to the main
executable and does not model independent SDK consumers or the separate test
programs. It remains available through `make lint-backend`; it is outside the
required CI profile, and its baseline is not regenerated to make the gate pass.

`fountain-check-release` preserves the existing local release source profile and
adds the strict SDK browser check. It shares source commands with hosted CI but
does not provision its PostgreSQL or browser fixtures. `fountain-release.sh`
already creates the isolated archive baseline and invokes that target before its
single image build. Local release-source success alone is not full hosted
qualification.

## Integration fixture completion

Ordinary synthetic HTTP mutation helpers drain the asynchronous queue graph before
expected successful mutations and after successful requests. Native and private protocols, authentication, accepted
background jobs and real HTTP/Git clients retain explicit synchronization. Direct
service/Git fixture writers also wait before inspecting deferred effects. Busy
mutations are never retried after effects; the parallel issue-create fixture only
retries the specific refusal that occurs before creation and checks the exact
final count.

Optional extension fixtures persist activation intent and wait for readiness;
startup deliberately does not block the host on optional backends. Git's newer
`preparing` notification is recognized without using its unresolved symbolic
references as scope evidence. The locked `prepared` callback remains mandatory
for admission. Ordinary main-repository receive owners can complete validated
push options; conditional owners still refuse them. Interrupted receive options
remain fenced because Git ref evidence cannot reconcile repository settings.
