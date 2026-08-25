// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package federation

import (
	"context"

	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	notify_service "forgejo.org/services/notify"
)

func init() {
	notify_service.RegisterNotifier(&federationNotifier{})
}

type federationNotifier struct {
	notify_service.NullNotifier
}

var _ notify_service.Notifier = &federationNotifier{}

// DeleteRepository removes the federation-side metadata of a deleted
// repository: its federated mirror record (if it was a mirror of a remote
// repository), the remote repositories following it, and the remote users
// following it. A deleted federated mirror also unfollows its upstream so
// the remote side drops us from its followers.
func (f *federationNotifier) DeleteRepository(ctx context.Context, doer *user_model.User, repo *repo_model.Repository) {
	if repo == nil {
		return
	}
	if federatedMirror, err := repo_model.GetFederatedMirrorByRepoID(ctx, repo.ID); err == nil && !federatedMirror.IsPush {
		// Notify the upstream that we no longer mirror it.
		if doer == nil {
			var uerr error
			if doer, uerr = user_model.GetUserByID(ctx, repo.OwnerID); uerr != nil {
				log.Warn("Unable to load owner of deleted mirror %d for unfollow: %v", repo.ID, uerr)
			}
		}
		if doer != nil {
			if uerr := UnfollowRemoteRepository(ctx, doer, repo, federatedMirror.RemoteActorURI); uerr != nil {
				log.Warn("Unable to unfollow upstream %s after deleting mirror %d: %v", federatedMirror.RemoteActorURI, repo.ID, uerr)
			}
		}
	}
	if err := repo_model.RemoveFederatedMirror(ctx, repo.ID); err != nil {
		log.Warn("Unable to remove federated mirror metadata for deleted repository %d: %v", repo.ID, err)
	}
	if repo == nil {
		return
	}
	if err := repo_model.RemoveFederatedMirror(ctx, repo.ID); err != nil {
		log.Warn("Unable to remove federated mirror metadata for deleted repository %d: %v", repo.ID, err)
	}
	repositoryFollowers, err := repo_model.GetFederatedRepositoryFollowersByRepoID(ctx, repo.ID)
	if err != nil {
		log.Warn("Unable to list repository followers of deleted repository %d: %v", repo.ID, err)
	} else {
		for _, follower := range repositoryFollowers {
			if err := repo_model.RemoveRepositoryFollower(ctx, repo.ID, follower.RemoteActorURI); err != nil {
				log.Warn("Unable to remove repository follower %s of deleted repository %d: %v", follower.RemoteActorURI, repo.ID, err)
			}
		}
	}
	repoFollowers, err := repo_model.GetFederatedRepoFollowersByRepoID(ctx, repo.ID)
	if err != nil {
		log.Warn("Unable to list federated followers of deleted repository %d: %v", repo.ID, err)
	} else {
		for _, follower := range repoFollowers {
			if err := repo_model.RemoveRepoFollower(ctx, repo.ID, follower.UserID); err != nil {
				log.Warn("Unable to remove federated follower %d of deleted repository %d: %v", follower.UserID, repo.ID, err)
			}
		}
	}
}
