// Copyright 2024 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package federation

import (
	"context"

	activities_model "forgejo.org/models/activities"
	"forgejo.org/models/forgefed"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/convert"

	ap "github.com/go-ap/activitypub"
	"github.com/go-ap/jsonld"
)

func SendUserActivity(ctx context.Context, doer *user.User, activity *activities_model.Action) error {
	userActivity, err := convert.ActionToForgeUserActivity(ctx, activity)
	if err != nil {
		return err
	}

	payload, err := jsonld.WithContext(
		jsonld.IRI(ap.ActivityBaseURI),
	).Marshal(userActivity)
	if err != nil {
		return err
	}

	// 1. Deliver to the actor's federated followers.
	followers, err := user.GetFollowersForUser(ctx, doer)
	if err != nil {
		return err
	}
	for _, follower := range followers {
		if err := deliverToFederatedUser(ctx, doer, payload, follower.FollowingUserID); err != nil {
			return err
		}
	}

	// 2. Deliver to the repository's federated followers (the "Following for
	// Repositories" feature): followers of the repository receive the same
	// activity note.
	if activity.Repo != nil {
		repoFollowers, err := repo_model.GetFederatedRepoFollowersByRepoID(ctx, activity.Repo.ID)
		if err != nil {
			return err
		}
		for _, repoFollower := range repoFollowers {
			if err := deliverToFederatedUser(ctx, doer, payload, repoFollower.UserID); err != nil {
				return err
			}
		}
	}

	return nil
}

// deliverToFederatedUser queues the payload to the inbox of the given local
// user record (which must be the materialisation of a remote federated user).
func deliverToFederatedUser(ctx context.Context, doer *user.User, payload []byte, userID int64) error {
	_, federatedUser, err := user.GetFederatedUserByUserID(ctx, userID)
	if err != nil {
		return err
	}

	federationHost, err := forgefed.GetFederationHost(ctx, federatedUser.FederationHostID)
	if err != nil {
		return err
	}

	hostURL := federationHost.AsURL()
	return deliveryQueue.Push(deliveryQueueItem{
		InboxURL: hostURL.JoinPath(federatedUser.InboxPath).String(),
		Doer:     doer,
		Payload:  payload,
	})
}

func NotifyActivityPubFollowers(ctx context.Context, actions []activities_model.Action) error {
	if !setting.Federation.Enabled {
		return nil
	}
	for _, act := range actions {
		private, err := act.IsActionPrivate(ctx)
		if err != nil {
			log.Error("Failed to check if action is private: %s", err.Error())
			continue
		}

		if private {
			continue
		}

		act.LoadActUser(ctx)
		if act.ActUser == nil {
			log.Error("Failed to load sending user")
			continue
		}

		if err := SendUserActivity(ctx, act.ActUser, &act); err != nil {
			return err
		}
	}
	return nil
}
