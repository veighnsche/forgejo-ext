// Copyright 2024 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package federation

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"forgejo.org/models/forgefed"
	"forgejo.org/models/repo"
	"forgejo.org/models/user"
	fm "forgejo.org/modules/forgefed"
	"forgejo.org/modules/log"
	"forgejo.org/modules/validation"
	app_context "forgejo.org/services/context"

	ap "github.com/go-ap/activitypub"
)

// ProcessLikeActivity receives a ForgeLike activity and does the following:
// Validation of the activity
// Creation of a (remote) federationHost if not existing
// Creation of a forgefed Person if not existing
// Validation of incoming RepositoryID against Local RepositoryID
// Star the repo if it wasn't already stared
// Do some mitigation against out of order attacks
func ProcessLikeActivity(ctx context.Context, activity *ap.Activity, repositoryID int64) (ServiceResult, error) {
	constructorLikeActivity, _ := fm.NewForgeLike(activity.Actor.GetLink().String(), activity.Object.GetLink().String(), activity.StartTime)
	if res, err := validation.IsValid(constructorLikeActivity); !res {
		return ServiceResult{}, NewErrNotAcceptablef("Invalid activity: %v", err)
	}
	log.Trace("Activity validated: %#v", activity)

	// parse actorID (person)
	actorURI := activity.Actor.GetID().String()
	user, _, _, err := FindOrCreateFederatedUser(ctx, actorURI)
	if err != nil {
		log.Error("Federated user not found (%s): %v", actorURI, err)
		return ServiceResult{}, NewErrNotAcceptablef("FindOrCreateFederatedUser failed: %v", err)
	}

	if activitySeenByActor(actorURI, constructorLikeActivity.StartTime) {
		return ServiceResult{}, NewErrNotAcceptablef("activity already processed")
	}

	// parse objectID (repository)
	objectID, err := fm.NewRepositoryID(constructorLikeActivity.Object.GetID().String(), string(forgefed.ForgejoSourceType))
	if err != nil {
		return ServiceResult{}, NewErrNotAcceptablef("Parsing repo objectID failed: %v", err)
	}
	if objectID.ID != fmt.Sprint(repositoryID) {
		return ServiceResult{}, NewErrNotAcceptablef("Invalid repoId: %v", err)
	}
	log.Trace("Object accepted: %#v", objectID)

	// execute the activity if the repo was not stared already
	alreadyStared := repo.IsStaring(ctx, user.ID, repositoryID)
	if !alreadyStared {
		err = repo.StarRepo(ctx, user.ID, repositoryID, true)
		if err != nil {
			return ServiceResult{}, NewErrNotAcceptablef("Staring failed: %v", err)
		}
	}

	return NewServiceResultStatusOnly(http.StatusNoContent), nil
}

// Create or update a list of FollowingRepo structs
func StoreFollowingRepoList(ctx *app_context.Context, localRepoID int64, followingRepoList []string) (int, string, error) {
	followingRepos := make([]*repo.FollowingRepo, 0, len(followingRepoList))
	for _, uri := range followingRepoList {
		federationHost, err := FindOrCreateFederationHost(ctx.Base, uri)
		if err != nil {
			return http.StatusInternalServerError, "Wrong FederationHost", err
		}
		followingRepoID, err := fm.NewRepositoryID(uri, string(federationHost.NodeInfo.SoftwareName))
		if err != nil {
			return http.StatusNotAcceptable, "Invalid federated repo", err
		}
		followingRepo, err := repo.NewFollowingRepo(localRepoID, followingRepoID.ID, federationHost.ID, uri)
		if err != nil {
			return http.StatusNotAcceptable, "Invalid federated repo", err
		}
		followingRepos = append(followingRepos, &followingRepo)
	}

	if err := repo.StoreFollowingRepos(ctx, localRepoID, followingRepos); err != nil {
		return 0, "", err
	}

	return 0, "", nil
}

func DeleteFollowingRepos(ctx context.Context, localRepoID int64) error {
	return repo.StoreFollowingRepos(ctx, localRepoID, []*repo.FollowingRepo{})
}

// forgeCapableFollowingRepos filters the following repos of a local repository
// down to those hosted on instances that speak the ForgeFed vocabulary (forge
// peers such as Forgejo or Gitea). Repository-level activities such as Like
// (star) and Undo(Like) must only be delivered to forge peers: microblogging
// servers (Mastodon, GoToSocial, ...) do not understand the ForgeFed
// vocabulary and would reject them, which is a protocol mismatch.
func forgeCapableFollowingRepos(ctx context.Context, followingRepos []*repo.FollowingRepo) ([]*repo.FollowingRepo, error) {
	result := make([]*repo.FollowingRepo, 0, len(followingRepos))
	for _, followingRepo := range followingRepos {
		host, err := forgefed.GetFederationHost(ctx, followingRepo.FederationHostID)
		if err != nil {
			return nil, err
		}
		if host.NodeInfo.SupportsRepositoryActivities() {
			result = append(result, followingRepo)
		} else {
			log.Trace("Skipping non-forge host %s for repository activity", host.HostFqdn)
		}
	}
	return result, nil
}

func SendLikeActivities(ctx context.Context, doer user.User, repoID int64) error {
	followingRepos, err := repo.FindFollowingReposByRepoID(ctx, repoID)
	log.Trace("Federated Repos is: %#v", followingRepos)
	if err != nil {
		return err
	}
	followingRepos, err = forgeCapableFollowingRepos(ctx, followingRepos)
	if err != nil {
		return err
	}

	// Deliver the Like to every followed peer repository via the asynchronous
	// delivery queue (the same path used for follows and notes): deliveries
	// are retried on failure instead of blocking the local star action.
	for i, followingRepo := range followingRepos {
		likeActivity, err := fm.NewForgeLike(doer.APActorID(), followingRepo.URI, time.Now().Add(time.Duration(i)*time.Second))
		if err != nil {
			return err
		}

		json, err := likeActivity.MarshalJSON()
		if err != nil {
			return err
		}

		if err := deliveryQueue.Push(deliveryQueueItem{
			InboxURL: fmt.Sprintf("%v/inbox", followingRepo.URI),
			Doer:     &doer,
			Payload:  json,
		}); err != nil {
			return err
		}
	}

	return nil
}

// ProcessUndoLikeActivity receives a ForgeUndoLike activity and does the following:
// Validation of the activity
// Creation of a (remote) federationHost if not existing
// Creation of a forgefed Person if not existing
// Validation of incoming RepositoryID against Local RepositoryID
// Unstar the repo if it was stared
// Do some mitigation against out of order attacks
func ProcessUndoLikeActivity(ctx context.Context, activity *ap.Activity, repositoryID int64) (ServiceResult, error) {
	// For an Undo(Like) the object is a nested Like activity whose own object
	// is the repository IRI that was originally starred.
	innerLike, ok := activity.Object.(*ap.Activity)
	if !ok {
		return ServiceResult{}, NewErrNotAcceptablef("object is not a Like activity")
	}
	repoIRI := innerLike.Object.GetLink().String()

	constructorUndoLikeActivity, _ := fm.NewForgeUndoLike(activity.Actor.GetLink().String(), repoIRI, activity.StartTime)
	if res, err := validation.IsValid(constructorUndoLikeActivity); !res {
		return ServiceResult{}, NewErrNotAcceptablef("Invalid activity: %v", err)
	}
	log.Trace("Activity validated: %#v", activity)

	// parse actorID (person)
	actorURI := activity.Actor.GetID().String()
	user, _, _, err := FindOrCreateFederatedUser(ctx, actorURI)
	if err != nil {
		log.Error("Federated user not found (%s): %v", actorURI, err)
		return ServiceResult{}, NewErrNotAcceptablef("FindOrCreateFederatedUser failed: %v", err)
	}

	// Reject replay/out-of-order activities per actor (see actor_watermark.go).
	if activitySeenByActor(actorURI, constructorUndoLikeActivity.StartTime) {
		return ServiceResult{}, NewErrNotAcceptablef("activity already processed")
	}

	// parse objectID (repository)
	objectID, err := fm.NewRepositoryID(repoIRI, string(forgefed.ForgejoSourceType))
	if err != nil {
		return ServiceResult{}, NewErrNotAcceptablef("Parsing repo objectID failed: %v", err)
	}
	if objectID.ID != fmt.Sprint(repositoryID) {
		return ServiceResult{}, NewErrNotAcceptablef("Invalid repoId: %v", err)
	}
	log.Trace("Object accepted: %#v", objectID)

	// execute the activity if the repo was stared already
	alreadyStared := repo.IsStaring(ctx, user.ID, repositoryID)
	if alreadyStared {
		err = repo.StarRepo(ctx, user.ID, repositoryID, false)
		if err != nil {
			return ServiceResult{}, NewErrNotAcceptablef("Unstaring failed: %v", err)
		}
	}

	return NewServiceResultStatusOnly(http.StatusNoContent), nil
}

// SendUndoLikeActivities sends Undo(Like) activities to all following repos of a given repository
// when the local user unstars it.
func SendUndoLikeActivities(ctx context.Context, doer user.User, repoID int64) error {
	followingRepos, err := repo.FindFollowingReposByRepoID(ctx, repoID)
	log.Trace("Federated Repos is: %#v", followingRepos)
	if err != nil {
		return err
	}
	followingRepos, err = forgeCapableFollowingRepos(ctx, followingRepos)
	if err != nil {
		return err
	}

	// Deliver the Undo(Like) to every followed peer repository via the
	// asynchronous delivery queue.
	for i, followingRepo := range followingRepos {
		undoLikeActivity, err := fm.NewForgeUndoLike(doer.APActorID(), followingRepo.URI, time.Now().Add(time.Duration(i)*time.Second))
		if err != nil {
			return err
		}

		json, err := undoLikeActivity.MarshalJSON()
		if err != nil {
			return err
		}

		// The Undo's object is a nested Like activity, so the inbox must be
		// derived from the followed repository URI rather than the activity ID.
		if err := deliveryQueue.Push(deliveryQueueItem{
			InboxURL: fmt.Sprintf("%v/inbox", followingRepo.URI),
			Doer:     &doer,
			Payload:  json,
		}); err != nil {
			return err
		}
	}

	return nil
}
