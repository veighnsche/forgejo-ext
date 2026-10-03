# Service callback peer proof (B5/N-IA2)

The shared service callback socket authenticates peers with kernel Unix
credentials (`SO_PEERCRED`) bound to an explicit operator mapping. This note
records what is already enforced, what the disposable fixture proves, and
the one residual decision for the integrator.

## Enforced chain at this revision

- Accept annotates every connection with kernel `uid/gid/pid`
  (`services/extensions/background.go`, `wrapPeerCredentialListener`).
  Connections without credentials get an unusable address that handlers
  must reject.
- Bootstrap requires the observed UID in the operator mapping
  `SERVICE_BRIDGE_PEERS` (`ParseServiceBridgePeers`,
  `Manager.BootstrapServiceAdmission`). Socket group membership or manifest
  declaration alone never authorizes a peer, and an empty mapping rejects
  every peer.
- Background operations re-check the peer UID against the bootstrapped
  admission on every request (`routers/web/extensions/background.go`,
  `serveBackgroundOperation`).
- The SDK dial side verifies the host UID before sending a bootstrap
  (`sdk/background.go`, `BootstrapServiceBackground`).

## Fixture proof

`internal/callback` is the dependency-free peer primitive
(`Peer`, `Parse`/`Format`, `Authorize`, `DialVerified`,
`VerifyConnection`) with the same `uid=N;gid=N;pid=N` wire form as the
SDK (cross-checked against `extension.ParseUnixPeer`/
`extension.FormatUnixPeer`).

`go test ./internal/callback/` proves over a disposable socket in a temp
directory, with no retained host or VM involved:

- dial reaches the listener and observes the real kernel UID/PID;
- a wrong expected UID is refused before any bootstrap bytes flow;
- the accept side binds to the same UID-to-package mapping and rejects
  unmapped peers, empty mappings, and missing credentials;
- malformed addresses (empty parts, non-positive PID, unknown keys,
  overflow) are all rejected.

Existing coverage this complements (rather than replaces):
`TestPeerCredentialListenerExposesKernelPeer`,
`TestBackgroundBootstrapRequiresServiceMapping`,
`TestServiceBootstrapLifecycle`.

## Residual: native callback path has no per-request peer rebind

`serveBackgroundOperation` re-checks the peer UID on every background
operation, but the native capability path (`callbackHandler` in
`routers/web/extensions/callback.go`) authenticates by admission token
alone after the peer-verified bootstrap. The admission token is a 256-bit
secret issued only over the verified channel, so this is defense-in-depth
rather than an open bypass — but a stolen admission token is usable from
any peer that can reach the socket.

Decision for the integrator: either rebind the admission to the observed
peer UID on every native callback request (using `VerifyConnection` or
the existing `RemoteAddr` credential form), or explicitly accept
token-only authentication for that path and record why. No IPC admission
code was changed in this lane; `internal/callback` is the proven
primitive either option wires in.
