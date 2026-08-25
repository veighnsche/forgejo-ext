// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package federation

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"forgejo.org/models/forgefed"
	repo_model "forgejo.org/models/repo"
	fm "forgejo.org/modules/forgefed"
	"forgejo.org/modules/log"

	ap "github.com/go-ap/activitypub"
)

// processRepositoryUnfollow handles an inbound Undo(Follow) activity targeting
// a local repository actor: the remote actor (a user or a repository mirror)
// no longer wants to follow the repository, so the follow record is removed.
func processRepositoryUnfollow(ctx context.Context, activity *ap.Activity, repositoryID int64) (ServiceResult, error) {
	innerFollow, ok := activity.Object.(*ap.Activity)
	if !ok {
		return ServiceResult{}, NewErrNotAcceptablef("object is not a Follow activity")
	}

	// The undo must target this repository actor.
	objectID, err := fm.NewRepositoryID(innerFollow.Object.GetID().String(), string(forgefed.ForgejoSourceType))
	if err != nil {
		return ServiceResult{}, NewErrNotAcceptablef("Parsing repo objectID failed: %v", err)
	}
	if objectID.ID != fmt.Sprint(repositoryID) {
		return ServiceResult{}, NewErrNotAcceptablef("Invalid repoId: %v", err)
	}

	actorURI := activity.Actor.GetID().String()

	// Repository actors (remote pull mirrors) vs person actors. The URI path
	// pattern is used (rather than fetching the actor document) because the
	// unfollowing repository may already have been deleted.
	if strings.Contains(actorURI, "/repository-id/") {
		if err := repo_model.RemoveRepositoryFollower(ctx, repositoryID, actorURI); err != nil {
			return ServiceResult{}, NewErrInternalf("RemoveRepositoryFollower: %v", err)
		}
		log.Info("Repository %s stopped following repository %d", actorURI, repositoryID)
		return NewServiceResultStatusOnly(http.StatusNoContent), nil
	}

	_, federatedUser, _, err := FindOrCreateFederatedUser(ctx, actorURI)
	if err != nil {
		return ServiceResult{}, NewErrNotAcceptablef("FindOrCreateFederatedUser failed: %v", err)
	}
	if err := repo_model.RemoveRepoFollower(ctx, repositoryID, federatedUser.UserID); err != nil {
		return ServiceResult{}, NewErrInternalf("RemoveRepoFollower: %v", err)
	}

	return NewServiceResultStatusOnly(http.StatusNoContent), nil
}
