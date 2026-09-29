# Local native-session dependency patch

Source: `code.forgejo.org/go-chi/session` **v1.1.0**, copied without changing its
module path. The upstream Apache-2.0 license and notices are retained. The root
module selects this one implementation with a local `replace` directive.

Local changes are limited to `file.go`, `file_lifecycle_test.go`, this note, and
adding the already selected `github.com/gofrs/flock v0.12.1` dependency in the
module manifests. All other upstream source and tests are unchanged.

The file provider now persists a `forgejo-session-1` record containing a random
generation, a revision and the upstream gob-encoded session data. Missing,
expired, destroyed or regenerated authority cannot be restored by an older
request's `Release`. Read-only releases neither write nor extend expiry. Dirty
releases commit only the same live generation and revision; conflicting dirty
requests return `ErrSessionConflict`. They do not merge disjoint changes or retry
a stale snapshot. Ordinary subsequent writes from the same successfully released
store continue using its updated revision. Empty `Flush` is persisted.

The existing provider mutex and a stable `.session.lock` file serialize every
file lifecycle operation, including GC, across provider instances. Atomic rename
publishes each complete record. The containing directory must be a local
filesystem supporting the lock and rename semantics, writable only by the
trusted service. Do not remove the lock file while that directory is in use.
The lock file is excluded from session counting and GC. Temporary record files
are cleaned on failure, and expired orphaned temporary files can be collected.
No external writers may bypass the provider or run an older binary against the
same directory. Power-loss durability is not claimed; records are not fsynced.

Unversioned or malformed records are rejected. There is no legacy record reader
or migration. Existing file-session cookies do not authorize access to the new
format; development deployments should begin with an empty disposable session
directory. Provider/config errors remain errors, not fresh authenticated state.
There is no new browser credential, session daemon, or per-SID coordination map.

`NewFileStore` remains source-compatible as an in-memory constructor, but it does
not bind a persisted generation. Use `FileProvider.Read` to obtain a persistable
store. Mutations must use `Set`, `Delete`, or `Flush`; mutating a returned object
without calling `Set` does not mark the store dirty.

Run the upstream package suite and local regression suite from the root module:

```sh
go test code.forgejo.org/go-chi/session
```

The Forgejo `VirtualStore` retains the first real raw store across repeated
releases so it cannot obtain a newer generation by rereading the same SID.
Real TLS middleware regression coverage lives in
`routers/common/session_stream_test.go`; it exercises both ordinary delayed
HTTP completion and WebSocket completion for memory/file providers.

When updating this dependency, compare against the selected upstream source,
reapply or remove only the above delta, and rerun the lifecycle and real-server
checks. Do not claim an upstream release contains this correction.
