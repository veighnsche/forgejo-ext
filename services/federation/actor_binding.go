// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package federation

import (
	"context"
	"errors"
	"net/url"
	"strings"

	app_context "forgejo.org/services/context"

	ap "github.com/go-ap/activitypub"
)

// ErrActorNotVerified is returned when the actor claimed by an activity does
// not match the identity verified from the request's HTTP signature.
var ErrActorNotVerified = errors.New("activity actor does not match the verified signature")

// VerifyPersonActivityActor checks that an activity delivered to a Person
// inbox is authored by the verified principal: the signature key must belong
// to the same person as the actor claimed in the payload. The claimed actor
// URI is resolved to its canonical form first, so aliases of the same person
// are accepted.
//
// Instance-level keys (Application actors) and repository keys never verify
// person activities.
func VerifyPersonActivityActor(ctx context.Context, principal *app_context.FederationPrincipal, activity *ap.Activity) error {
	if principal == nil || principal.ActorURI == "" {
		// Instance-level keys have no actor URI and cannot act as a person.
		return ErrActorNotVerified
	}
	if activity == nil || activity.Actor == nil {
		return ErrActorNotVerified
	}
	actorURI := activity.Actor.GetLink().String()
	if actorURI == "" {
		return ErrActorNotVerified
	}
	_, federatedUser, _, err := FindOrCreateFederatedUser(ctx, actorURI)
	if err != nil {
		return ErrActorNotVerified
	}
	if principal.ActorURI != federatedUser.NormalizedOriginalURL {
		return ErrActorNotVerified
	}
	return nil
}

// VerifyRepositoryActivityActor checks that an activity delivered to a
// Repository inbox is authored by the verified principal. Two cases are
// accepted:
//
//   - the activity actor is the principal itself (a person acting as
//     themselves, or a repository actor signing with its own key);
//   - the activity actor is hosted on the same host as the verified key:
//     repository actors sign their activities with their owner's or their
//     instance's key, both of which are served by the repository's host.
//
// Cross-host actor claims are always rejected: an instance must not be able
// to act on behalf of actors hosted elsewhere.
//
// The same-host fallback means an instance can act as any of its own users
// for repository-targeted activities (a person, another repository, ...).
// This is consistent with the custodial key model of the fediverse: the
// instance already holds every local user's private key.
func VerifyRepositoryActivityActor(ctx context.Context, principal *app_context.FederationPrincipal, activity *ap.Activity) error {
	if principal == nil {
		return ErrActorNotVerified
	}
	if activity == nil || activity.Actor == nil {
		return ErrActorNotVerified
	}
	actorURI := activity.Actor.GetLink().String()
	if actorURI == "" {
		return ErrActorNotVerified
	}
	if principal.ActorURI != "" && principal.ActorURI == actorURI {
		return nil
	}
	// Repository actors sign with keys hosted on the repository's host.
	actorURL, actorErr := url.Parse(actorURI)
	keyURL, keyErr := url.Parse(principal.KeyID)
	if actorErr == nil && keyErr == nil && strings.EqualFold(actorURL.Host, keyURL.Host) {
		return nil
	}
	return ErrActorNotVerified
}
