# Service callback peer proof (B5/N-IA2)

The Unix callback sockets authenticate peers with kernel credentials
(`SO_PEERCRED`) through the existing SDK primitives (`PeerCredential`,
`ParseUnixPeer`/`FormatUnixPeer`). There is no second peer
implementation: the earlier `internal/callback` duplicate had no
production imports and was removed.

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
- The SDK dial side verifies the host UID before sending anything, on both
  the service bootstrap (`sdk/background.go`,
  `BootstrapServiceBackground` with the operator-configured
  `ExpectedHostUID`) and the native client (`sdk/native.go`,
  `nativeClient` against the extension's own UID, which is the host UID
  because the host spawns the extension without changing users). A UID
  mismatch or unreadable credentials closes the connection before any
  admission-bearing bytes flow.

Native admissions are derived from a browser request/session
(`routers/web/extensions/callback.go`, `createAdmission`), not from the
service bootstrap; the native dial check above is what binds each native
callback connection to the host peer.

## Shipping-caller proof

- `TestNativeClientVerifiesHostPeer` drives the real `nativeClient`
  methods: a wrong-UID listener is refused with zero request bytes
  observed, and a missing socket fails.
- `TestNativeClientUsesPrivateCallback` drives `Authority.Native()` end
  to end against a real same-UID listener: the correct deployed peer is
  accepted.
- `TestBootstrapRejectsUnexpectedHostPeer` covers the service bootstrap
  dial with a wrong expected UID.

Existing host-side coverage this complements (rather than replaces):
`TestPeerCredentialListenerExposesKernelPeer`,
`TestBackgroundBootstrapRequiresServiceMapping`,
`TestServiceBootstrapLifecycle`.

## Reachability assessment (conditional, unchanged)

Peer verification binds the listener UID, not the path: it rejects a
listener running as another UID but cannot distinguish a same-UID
counterfeit on a redirected socket path. Exploitability of that residual
stays conditional on actual mount/UID/MAC access and endpoint lifecycle,
as in the original finding. No new trust subsystem is introduced.
