# Native extensions

This experimental extension interface adds independently installed packages to
Forgejo 15.0.9. A package contains a native backend executable, a manifest and
browser assets. Installing or replacing it does not rebuild Forgejo. Packages
start with Forgejo; changes require a stop and restart.

Extensions are **administrator-trusted code**. Their backend runs as the Forgejo
OS user, and their JavaScript runs on Forgejo's origin with access to the native
page and browser session. Process separation handles lifecycle and crashes; it
is not a security sandbox. Only install packages whose code you trust. Ordinary
users cannot upload or activate packages.

## Build and install the example

Build the host with the normal Forgejo build instructions. Build the example
separately from this source checkout with its pinned Go toolchain:

```sh
mkdir -p .artifacts/packages/example/assets
go build -o .artifacts/packages/example/backend ./contrib/extensions/example
./node_modules/.bin/tsc -p sdk/browser
cp contrib/extensions/example/extension.json .artifacts/packages/example/
cp contrib/extensions/example/assets/*.js .artifacts/packages/example/assets/
```

The runtime uses Unix sockets; the executable must target the Unix host's OS
and architecture. The Go SDK is the nested `sdk/` module with the task-local
module identity `forgejo.org/extension-sdk`. It builds independently of the
Forgejo host checkout; the root module uses a local replacement while this
unreleased interface is developed. Authors ship only the built package. The
host never compiles installed packages.

Configure the same `app.ini` used by the server:

```ini
[extensions]
ENABLED = true
; Defaults to APP_DATA_PATH/extensions. Relative paths use Forgejo's work path.
PATH = data/extensions
```

Stop Forgejo, then run the commands as its service account using its normal
configuration and work path:

```sh
forgejo --config /home/forgejo/custom/conf/app.ini --work-path /home/forgejo extensions install .artifacts/packages/example
forgejo --config /home/forgejo/custom/conf/app.ini --work-path /home/forgejo extensions list
```

Start Forgejo and sign in through its browser login. The example adds a global
page, user settings page, repository tab, administrator page, and a **Workspace**
navigation link. The workspace's notes panel remains mounted while native pages
navigate. **Save notes** stores up to 16 KiB of UTF-8 text for the signed-in actor
in the extension's private data directory.

Use `extensions install --replace DIRECTORY` to replace a package,
`extensions disable ID` to deactivate it, and `extensions enable ID` to activate
it. Stop Forgejo first: an OS lock blocks package changes while the extension
manager is running. Replacement preserves activation and data. Installation
validates paths and entry files, rejects symlinks and special files, stages the
copy before replacing the old package, and limits packages to 10,000 files and
512 MiB. There is no upload endpoint, package downloader or hot reload.

Enabled packages are discovered once at startup. A startup failure stops all
extensions and fails server initialization. A later backend crash removes that
extension's contributions when the registry next checks it; restart Forgejo to
start it again. Data under `PATH/.data/ID` persists across replacement, disabling
and restarts. Keep that directory in service backups when its content matters.
The package directory and runtime sockets are not a database API.

## Manifest and native contributions

See [example/extension.json](example/extension.json) for a complete manifest.
`protocol` must equal `1`; unsupported protocols are rejected. `id`, page IDs and
panel IDs use lowercase letters, digits and hyphens. `name`, `version`, and a
relative `executable` path are required. Page and panel `entry` paths are relative
to the package's `assets/` directory and identify ES modules.
`capabilities` declares native reads and an optional contribution authorizer;
`policies` names registered policy handlers. `preferred_workspace` opts a
trusted package into the persistent workspace host.

Pages declare an optional `route` selecting a host-owned slot. The package ID and
page ID remain private contribution identities, never public page URLs or menu
groups. A missing `route` declares an API-only contribution. Routes are unique per
scope across installed running packages; startup refuses collisions with existing
accounts. Pages use the corresponding native layout and navigation:

| Scope | Permission | Route |
| --- | --- | --- |
| `global` | omitted | `/workspace`, `/tailnet` |
| `user` | `user` | `/user/settings/workspace`, `/user/settings/identity` |
| `repository` | `read`, `write`, or `admin` | `/OWNER/REPO/workspace` |
| `admin` | `admin` | `/admin/workspace` |

Every route requires a native signed-in browser session and the existing native
account-state checks. Tokens, Basic authentication and reverse-proxy login are
not accepted as extension credentials. Repository contributions use native code
unit read/write permission or repository administration, and inaccessible pages
are omitted from navigation and rejected by direct requests. Repository `admin`
pages use the native repository settings layout. Site administration requires a
site administrator.

Page APIs use the private native-web prefix `/-/native/pages/ID/PAGE/api/`,
preceded by `/user/settings`, `/admin`, or `/OWNER/REPO` for their native scope.
Assets use `/-/native/assets/ID/`; panel APIs use
`/-/native/panels/ID/PANEL/api/`. These are session-protected web handlers, not token
REST endpoints. Consume the supplied `apiBase`, `assetBase` and explicit
`appSubUrl`; never infer product routes from package names or private APIs.

An extension declaring `native.contribution.authorize` is consulted before its
page appears, before the workspace mounts each panel, and for every page or panel
API request. The callback may narrow native access but cannot grant access denied
by Forgejo. A denied or unavailable callback hides the contribution or rejects the
request; it does not fall back to manifest-only visibility.

## Browser lifecycle

The canonical browser definition is
[sdk/browser/extension-contract.d.ts](../../sdk/browser/extension-contract.d.ts).
Consumers keep a local type-only mirror, recording its source revision and
SHA-256 and checking the copied body against that pin. Re-sync the mirror from the
canonical definition when it changes; keep its provenance and pin check together.
An isolated consumer compile needs only this definition and the consumer source.
The example typecheck above checks its actual browser entries; emitted JavaScript
has no runtime SDK import. The host injects `request` and `stream` through context.

An entry module exports `mount(root, context)`. It may be asynchronous and may
return a cleanup function, an object with `dispose()`, or a lifecycle handle with
`update()`, `suspend()`, `resume()` and `retire()`. Mount is called once per root.
Visible retained owners receive presentation/context updates; sheltered owners
suspend and resume while retaining the same root, resources and stream. Final
retirement follows authority loss or contribution removal. The context contains
`extensionId`, `pageId` or `panelId`, `apiBase`, `assetBase`, `appSubUrl`,
`sessionGeneration`, `contractVersion`, scoped `request(relativePath, options)`
and `stream(relativePath, options)`, and optional observed `repositoryId`.
Check the contract version expected by the consumer build before using either
helper. The helpers apply the native URL/session contract. The repository ID
selects a display only; it does not widen a global contribution's authority.

```js
export function mount(root, {contractVersion, request}) {
  if (contractVersion !== 1) throw new Error('Unsupported browser contract');
  const controller = new AbortController();
  request('context', {signal: controller.signal})
    .then((response) => {
      if (!response.ok) throw new Error('Request failed');
      return response.json();
    })
    .then((context) => { root.textContent = JSON.stringify(context); })
    .catch(() => {
      if (!controller.signal.aborted) root.textContent = 'Could not load context.';
    });
  return () => controller.abort();
}
```

Use the supplied helpers, handle errors, render untrusted values as text, and
release listeners, timers and requests in cleanup. Native cross-origin request
protection applies to backend calls. Extensions own their UI and styling inside
their mount roots.

The bounded native route proof establishes retained notes roots, resources and
streams through admitted native transitions using actual templates, assets and
initializers. Broader native feature/route cutover and installed consumer proof
remain separate qualifications.

Authentication, account restrictions, credential flows, partial/failed responses
and external navigation retain native handling. The host checks the current
session at `/-/native/session` before exposing private cached state. A panel has
actor authority only; an observed repository does not grant repository authority.

## Native frontend correction target

Fountain owns the existing Forgejo frontend and its extension integration.
Consumers render product content through that frontend. The bounded native proof
establishes the retention mechanism; the table defines ownership for the remaining
feature/route cutover and consumer integration. The reusable capability works
without Soda, its template payload, product IDs or domain models.

| Responsibility | Fountain | Consumer |
| --- | --- | --- |
| Native shell | Native navigation, account controls, repository headers, notices, footer and approved contribution placement. | Contribution labels and product content; no replacement shell or chrome. |
| Native page transitions | Canonical URL/appSubUrl/query/fragment/title/history, focus/scroll, page configuration, native feature initialization and teardown, conditional scripts, forms and HTMX. | Content routes and product parameter meaning; invoke native navigation without intercepting core pages. |
| Contribution registration | Guarded native routes, namespace/collision checks and declared page/panel relationships. | Declare pages, subordinate content routes, panels, scopes and required permissions; validate product identities in its backend. |
| Mount ownership | Select one instance for a declared shared page/panel owner; retain its connected root while presentation changes. | Render once in the supplied root and retain its own view/resources; no inspection of another mount or host ancestors. |
| Presentation and context | Publish page/panel mode, current route and parameters, observed repository, visibility and lifecycle updates. | Update displayed content from the supplied facts; browsing must not retarget an existing execution. |
| Geometry and styling | Usable responsive native containers and the native theme/component contract; initialize native UI mechanisms. | Local content layout, terminal cell sizing and correctly built scoped styles/assets. No body/navbar/footer sizing or global theme replacement. |
| Browser authority | Native admission, session suspension/revalidation, revocation, guarded request/stream facilities and retirement signals. | Consume the lifecycle and clean up its resources when retired; enforce additional domain/membership/operator policy on every protected operation. |
| Product and execution | Native collaboration primitives and their native authorization. | Product workflows, durable state, factory policy, terminal protocol/renderer and external process/session ownership. |

The canonical definition expresses initial mount, context/presentation updates,
suspension, validated resumption and final retirement. Host-injected HTTP/stream
helpers enforce the contribution URL and session lifetime; consumers own their
application wire protocol and render within the supplied root.
Browser context remains display information, never operation authority.

Persistent contributions retain the same root and local resources, including a
renderer/socket where used, through ordinary native navigation. The host changes
presentation within the native layout without disconnecting that owner. Reload
creates a fresh browser mount through normal
admission. Authentication, installation, credential flows, errors and departures
retain their appropriate native handling. Cached return stays private until the
host revalidates the session; consumers receive the resulting lifecycle instead
of independently interpreting `pagehide`/`pageshow`. Loss of authority retires the
mount and its transport, without becoming a product execution command.

Fountain must not encode a consumer's saved-request IDs, Project/terminal-session
records or factory rules. Consumers declare content routes; Fountain enforces
their bounded registration and conflicts. The same distinction applies to styles:
native components and light/dark selection belong to the host, while a consumer
must compile valid local styles and keep its own forms/settings/dialogs usable.

### Native capability proof and cutover

1. The bounded critical-boundary proof is established using actual native templates,
   assets, configuration and initializers with the generic notes example's HTTP
   and admitted native stream. Extend qualification as further routes are admitted.
   No Soda imports, template overrides, request IDs or host-DOM inspection belong
   in the example. Exercise native → contribution → native and the reverse entry
   order, global/repository context, a working native control after each change,
   Back/Forward, query/fragment/title, focus/scroll and session invalidation.
   Assert one mount and identical retained node, resource and socket objects.

   NFX-A01 reconciliation (against Fountain `6abafdb409`): the retained
   diagnostic drivers (`9e4bce97e2`, originally `test.fails`) named the first
   failing subject — the entered document's live page configuration never
   followed the transition across both shapes. The two actual native template
   shapes are the direct contribution document (`templates/extensions/page.tmpl`
   rendering `extensions/content` inside `base/head`/`base/footer`) and the
   plain native document carrying the shared panels home hidden and inert
   (`templates/base/footer.tmpl` rendering `extensions/panels`; error, auth and
   install documents stay free of contribution roots). The transplant in
   `web_src/js/features/extensions.js` now brackets each swap with
   retire → `setPageConfig` → init (`web_src/js/native-lifecycle.js`), fed by
   the machine-readable body embed (`templates/base/head.tmpl`), so
   `window.config.pageData` follows each entered document while one retained
   owner survives both entry orders; the drivers pass unmarked
   (`1b631320f0`, `7e9bbe6338`). First real cases: native control — the admin
   user-list search form (`web_src/js/features/admin/users.js`, submitted via
   `requestSubmit` so the host intercepts the GET navigation); configuration —
   `adminUserListSearchForm` versus the extension page data carried per
   document; admitted stream — the notes panel stream
   (`GET /stream` in `contrib/extensions/example/main.go`, consumed through
   `stream('stream')` in `contrib/extensions/example/assets/panel.js`). Seam
   writers: A owns the transplant, B owns the lifecycle seam and the control,
   C owns the retained-owner mount. NFX-A01 therefore closes on its original
   finish condition; the Soda task ledger count remains for the delivery lane
   to reconcile.
2. Supply the reusable manifest/browser lifecycle contract and native container
   behavior. Integrate the needed initialization/cleanup into native feature
   owners; use the existing Forgejo frontend mechanisms rather than a parallel
   component framework. A whole-document reload cannot establish same-object
   persistence. A failed native proof is an unresolved capability, not permission
   to substitute consumer reconnection or another wrapper.
3. Complete the wider native feature/route cutover through the proved lifecycle and
   remove remaining obsolete wrapper callers. Retain native handlers, contribution
   guards, admission, callbacks and stream revocation. Qualify affected native
   feature classes before widening navigation support; the bounded route proof
   does not establish the whole site's lifecycle.
4. Migrate consumers to the host contract and remove obsolete shell adapters with
   their callers/fixtures. Verify the actual installed consumer composition and
   resources separately; a generic example does not prove an application's xterm
   behavior, content geometry or emitted stylesheet correctness.

## Backend interface

The SDK's `Serve(extensions.Application)` starts a private Unix HTTP listener and the
HashiCorp go-plugin lifecycle handshake. The host provides a restricted
environment, package working directory and writable `extensions.DataDir()`.
Do not write handshake output to stdout; use stderr for diagnostics without
credentials. The SDK is an HTTP integration boundary, not access to Forgejo's
internal database or service packages.

For each request, call `extensions.RequestContext(request)` to obtain native
authority. The host supplies the extension and process instance IDs, contribution
ID/kind/scope/action, session generation, actor ID, username, site-admin display
fact and, when a native repository route admits one, repository ID, owner, name
and current permission. Actor and repository IDs are decimal strings. The
admission handle remains private when authority is encoded as JSON. The host
replaces client-supplied context and does not forward
browser cookies, authorization headers or arbitrary client headers. Treat this
context as authority for that request only. Use `authority.Native()` for bounded
current native reads; each callback is checked by the host against its private
admission and current session. Enforce endpoint-specific policy in
the extension: a read page does not make every backend operation appropriate for
a reader. Partition private state by authoritative IDs rather than browser input.

The proxy supports ordinary GET, HEAD, POST, PUT, PATCH and DELETE requests,
forwards Accept, Accept-Language and Content-Type, and preserves the relative
API path and query. Requests have a 30-second deadline; request and response
bodies are limited to 8 MiB. Ordinary responses are buffered and only Content-Type
and status are propagated, with caching disabled. Redirects and server-sent events
are rejected. WebSocket requests use a separate same-origin admitted stream path
that checks continuing native authority and closes on revocation. These streaming
limits do not inherit the buffered-response contract. The example demonstrates
per-actor storage and atomic note replacement without placing extension data in
Forgejo tables.

## Current boundaries

This foundation provides native contributions, independently built backend
packages, bounded authenticated HTTP, admitted WebSocket transport, persistent
panel mounts, bounded native operations and local package management. Ordinary
buffered HTTP and live-stream handling have separate response/lifetime contracts;
the browser persistence correction above remains unproved. It does not provide a
marketplace or an untrusted-code sandbox. Extend demonstrated native capabilities
instead of exposing arbitrary internal objects or adding parallel authentication.

## Conditional issue graph changes

The background native operation `issue.graph.mutate` accepts the SDK's
`IssueGraphPayload`: bounded native issue creations, exact title/body edits and
explicit dependency additions/removals in one repository. New issues use
request-local keys; existing issues use database IDs, not issue numbers.
`IssueGraphReceipt` returns each created key's native ID/index, edited content
versions and changed edges. At most 64 changes and 48 KiB of encoded payload fit
one packet within the existing private callback limit. A cycle of any length,
missing removal, duplicate addition, stale edit or invalid anchor refuses the
whole packet. Existing dependencies may reference closed prerequisite issues;
closed, locked or pull-request issues cannot receive requirement edits.

The submitting installation needs its own enrolled `issue.graph.mutate` actor
binding, a live repository-write token restricted to the target repository, and
current native code-write and issue-write permission. Another operation kind's
binding grants no graph authority. Current native revision, expiry, cancellation,
immutable operation ID and credential-generation checks use the existing
conditional-operation ledger. Native SQL effects and their attributable receipt
commit together; replay returns that receipt without creating duplicate issues.
Native content history and dependency comments retain the actual submitting
actor. Notifications follow the primary commit, with incomplete delivery reported
as `needs_intervention`; offline recovery reconciles the exact native receipt and
never replays issue changes or notification delivery.

The host supplies native mutations only. The extension owns accepted intent,
objective provenance, split/consolidation meaning, material scope decisions,
planning invalidation and spending grants. This operation never accepts an
objective, closes an issue, declares an outcome or grants execution authority.

`repository_writers` snapshots provide current-owner permission observations
without a browser session. `SnapshotRequest.WriterIDs` selects at most eight
native actors; `NativeSnapshot.Writers` returns their current code-write
permission. The enrolled requesting actor must itself currently have native
code-write permission and the credential must grant repository-read scope.
Missing, inactive or prohibited actors return false. This observation confers no
authority; clients bracket it against the native revision and verify that
revision at the conditional mutation boundary.
