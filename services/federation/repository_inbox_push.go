// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package federation

import (
	"context"
	"net/http"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/log"
	mirror_service "forgejo.org/services/mirror"

	ap "github.com/go-ap/activitypub"
)

// processRepositoryPush handles an inbound Push activity delivered to a local
// repository actor: the remote repository (identified by activity.Actor) has
// received new commits. If the local repository is a federated pull mirror of
// that remote repository, an immediate mirror sync is triggered so the local
// copy follows the remote without waiting for the next scheduled sync.
//
// This implements the ForgeFed pull-mirror workflow (spec §3.3 "Mirroring"):
// the mirror follows the upstream, the upstream notifies followers with a Push
// activity, and the mirror pulls via the upstream's cloneUri.
func processRepositoryPush(ctx context.Context, activity *ap.Activity, repositoryID int64) (ServiceResult, error) {
	federatedMirror, err := repo_model.GetFederatedMirrorByRepoID(ctx, repositoryID)
	if err != nil {
		if repo_model.IsErrFederatedMirrorNotExists(err) {
			// A push to a repository that is not a federated mirror is purely
			// informational for us: acknowledge and drop it.
			log.Trace("Ignoring Push activity to non-mirror repository %d", repositoryID)
			return NewServiceResultStatusOnly(http.StatusNoContent), nil
		}
		return ServiceResult{}, NewErrInternalf("GetFederatedMirrorByRepoID: %v", err)
	}

	if federatedMirror.IsPush {
		// Push mirrors receive the changes they push upstream; there is nothing
		// to pull locally.
		return NewServiceResultStatusOnly(http.StatusNoContent), nil
	}

	// Only sync when the pushing actor is the configured upstream of this pull
	// mirror. Any other actor (e.g. a fork of the upstream) must not be able to
	// trigger syncs of arbitrary mirrors.
	actorURI := activity.Actor.GetID().String()
	if actorURI != federatedMirror.RemoteActorURI {
		log.Trace("Ignoring Push activity for mirror %d from non-upstream actor %s", repositoryID, actorURI)
		return NewServiceResultStatusOnly(http.StatusNoContent), nil
	}

	log.Info("Push activity from %s triggered sync of federated pull mirror %d", actorURI, repositoryID)
	mirror_service.AddPullMirrorToQueue(repositoryID)

	return NewServiceResultStatusOnly(http.StatusNoContent), nil
}
