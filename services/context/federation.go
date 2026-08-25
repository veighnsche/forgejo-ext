// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package context

import user_model "forgejo.org/models/user"

// FederationPrincipal is the remote identity whose HTTP signature was
// verified on the current request by the ActivityPub middleware
// (routers/api/v1/activitypub.ReqHTTPSignature).
//
// It is the sole source of authenticated remote identity: values claimed
// inside request payloads (e.g. an activity's "actor" field) are untrusted
// until they have been checked against the principal.
type FederationPrincipal struct {
	// KeyID is the verified key id, e.g. "https://host/actors/alice#main-key".
	KeyID string
	// ActorURI is the normalized URI of the actor owning the key, when known.
	// It is empty for instance-level (Application actor) keys that have no
	// user-mapped URI.
	ActorURI string
	// Host is the host (hostname or host:port) the verified key is served from.
	Host string
	// User is the local UserTypeActivityPubUser row of the key owner, if one
	// exists. It is nil for instance-level and uncached repository keys.
	User *user_model.User
}

// SetFederationPrincipal stores the verified federation principal on the context.
func (ctx *APIContext) SetFederationPrincipal(principal *FederationPrincipal) {
	ctx.federationPrincipal = principal
}

// FederationPrincipal returns the remote identity verified via HTTP signature,
// or nil when the request is not federation-authenticated.
func (ctx *APIContext) FederationPrincipal() *FederationPrincipal {
	return ctx.federationPrincipal
}
