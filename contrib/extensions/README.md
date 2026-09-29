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

Pages use the corresponding native layout and navigation:

| Scope | Permission | Route |
| --- | --- | --- |
| `global` | omitted | `/-/extensions/pages/ID/PAGE` |
| `user` | `user` | `/user/settings/extensions/ID/PAGE` |
| `repository` | `read`, `write`, or `admin` | `/OWNER/REPO/extensions/ID/PAGE` |
| `admin` | `admin` | `/admin/extensions/ID/PAGE` |

Every route requires a native signed-in browser session and the existing native
account-state checks. Tokens, Basic authentication and reverse-proxy login are
not accepted as extension credentials. Repository contributions use native code
unit read/write permission or repository administration, and inaccessible pages
are omitted from navigation and rejected by direct requests. Repository `admin`
pages use the native repository settings layout. Site administration requires a
site administrator.

A page's backend API is its route plus `/api/`. Assets are served from
`/-/extensions/assets/ID/` to signed-in users; never put secrets in assets. Panel
APIs use `/-/extensions/panels/ID/PANEL/api/`. Paths include Forgejo's configured
subpath when applicable. Consume the supplied URLs instead of constructing them.

## Browser lifecycle

An entry module exports `mount(root, context)`. It may be asynchronous and may
return a cleanup function or an object with `dispose()`. Mount is called once per
root; cleanup runs on departures that do not retain the document in the browser's
back/forward cache. Cached documents retain their mounts while suspended. The context contains
`extensionId`, `pageId` or `panelId`, `apiBase`, and `assetBase`.

```js
export function mount(root, {apiBase}) {
  const controller = new AbortController();
  fetch(`${apiBase}context`, {signal: controller.signal})
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

Use same-origin fetches, handle errors, render untrusted values as text, and
release listeners, timers and requests in cleanup. Native cross-origin request
protection applies to backend calls. Extensions own their UI and styling inside
their mount roots.

The opt-in workspace at `/-/extensions/workspace` retains panel roots beside a
same-origin iframe displaying ordinary Forgejo pages. Following native links
changes the iframe document while the panel's DOM, timers and unsaved state stay
alive. The outer address records the current native path. Authentication and
external navigation leave the workspace. Reloading or returning without a cached
document creates a new mount; persist durable state in the backend. A panel has actor
authority only: the repository displayed by the iframe does not confer
repository authority on the panel.

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
bodies are limited to 8 MiB. Responses are buffered and only Content-Type and
status are propagated, with caching disabled. Redirects, WebSocket upgrades and
server-sent events are rejected. The example demonstrates per-actor storage and
atomic note replacement without placing extension data in Forgejo tables.

## Current boundaries

This foundation provides native contributions, independently built backend
packages, bounded authenticated HTTP, persistent panel mounts and local package
management. It does not yet provide long-lived terminal streams, core mutation
capabilities, service veto hooks, durable event delivery, a marketplace or an
untrusted-code sandbox. Streaming needs a separately verified session-revocation
and response-writer integration with this Forgejo version before it can carry a
terminal. Extend these contracts for demonstrated callers instead of exposing
arbitrary internal objects or adding parallel authentication systems.
