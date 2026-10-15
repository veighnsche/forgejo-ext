# Fountain testing and protected pushes

The maintained branch is `codex/fountain-extensions`. Its required GitHub Actions
check is `Fountain CI`, supplied by `.github/workflows/fountain-ci.yml`.
Qualification runs on every maintained-branch push, pull request targeting that
branch, and `ci/fountain-FULL_COMMIT_SHA` tag push. There are no path-based
exemptions. The inherited upstream `.forgejo` workflows serve a different CI
installation.

## Qualify an exact candidate

Review the complete commits that will advance the maintained branch. Qualification
does not authorize publishing unrelated local work. Commit the intended changes,
then push one candidate tag:

```sh
candidate=$(git rev-parse HEAD)
git tag "ci/fountain-$candidate" "$candidate"
git push origin "refs/tags/ci/fountain-$candidate"
```

Wait for the candidate's `Fountain source CI` workflow and its `Fountain CI`
check to succeed. Verify the full SHA, workflow and GitHub Actions app identity.
Use the Actions UI or `gh run list --commit "$candidate"` and
`gh run watch RUN_ID --exit-status`. Then advance the branch by an ordinary
fast-forward push of that same commit:

```sh
git push origin "$candidate:refs/heads/codex/fountain-extensions"
```

Branch protection requires the `Fountain CI` check from GitHub Actions, strict
status checks and administrator enforcement. It disallows force pushes and
deletion. Human review is not an additional branch-protection requirement.
Scratch refs remain available for development and qualification.

A rebased, amended or regenerated commit needs its own qualification. Never move
a qualification tag to different bytes. A PR run may test a synthetic merge
commit; its badge does not qualify a different directly pushed commit. After a
successful push, confirm the maintained-branch CI run too. Version one runs all
checks again and does not reuse a previous result.

The upstream default branch remains `v17.0/forgejo`. Push-triggered workflows
support the candidate procedure without changing it. Manual dispatch is not the
bootstrap path. A local hook is optional feedback and is not the enforcement
boundary.

## Required checks

| Make target | Scope |
| --- | --- |
| `fountain-check-source` | Upstream generated/dependency/format/security checks, `lint-backend-analysis` and Swagger lint; explicit SDK and patched-session module integrity, tidy, format and vet checks. |
| `fountain-check-unit` | Full upstream root unit selection, SDK tests, and race checks for native operations, extension manager, deferred PR/Actions/repository workers, queue flushing, sessions and the patched file-session package. |
| `fountain-check-database` | SQLite integration and migrations, followed by sequential PostgreSQL native-operation, authority, creation recovery and migration tests. Every selected PG case must actually pass; skipped or empty selections fail. |
| `fountain-check-browser` | Frontend lint and nonempty, unfocused unit tests, strict SDK/example TypeScript, production frontend/backend builds, and the real example-extension Chromium journey. |
| `fountain-check-packaging` | Release-script Bash syntax, checksum-pinned Hadolint at warning severity, and pinned Actionlint for the source workflow. |

The aggregate runs with `always()` and succeeds only when every lane reports
`success`. Missing, skipped, cancelled, failed and neutral lane results cannot
qualify the commit. Dependency and compiler caches save downloads/work; they do
not replace test execution. Required Go runs use `-count=1`.

The Go unit target intentionally follows upstream's package selection, which
excludes integration, migration and E2E packages. Separate lanes cover those
boundaries. Optional upstream Redis/Elasticsearch/MinIO/provider and platform
matrices remain outside this Linux amd64 profile; their skips are not evidence
for those deployments. The downstream Soda browser joins remain opt-in. The
required Fountain browser test uses its own checked-in example and native server.

The integration baseline uses asynchronous fixture queues for deferred push,
PR and Actions work. Sequential fixture helpers wait for queued completion;
dedicated contention tests retain held reservations. Runtime PR sweeps retain
work across contention and admit only the private head refs belonging to their
bound source repository and branch, including fork PRs. Nested ref recording
preserves that binding, and bounded waits do not replay callbacks after they start.

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

Prerequisites are Linux amd64, Go 1.26.9, Node from `.node-version`, npm from
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

The hosted database job has a 120-minute limit, including builds, migrations and
PostgreSQL checks. The complete SQLite integration binary has a 100-minute deadline.

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
