// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package federation

import (
	"context"
	"fmt"
	"net/http"

	"forgejo.org/models/forgefed"
	"forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	fm "forgejo.org/modules/forgefed"
	"forgejo.org/modules/log"

	ap "github.com/go-ap/activitypub"
	"github.com/go-ap/jsonld"
)

// processRepositoryFollow handles an inbound Follow activity targeting a local
// repository actor: a remote user subscribes to the repository. The follower
// is recorded and an Accept activity is delivered back to the remote user's
// inbox, completing the handshake.
func processRepositoryFollow(ctx context.Context, activity *ap.Activity, repositoryID int64) (ServiceResult, error) {
	follow, err := fm.NewForgeFollowFromAp(*activity)
	if err != nil {
		log.Error("Invalid follow activity: %s", err)
		return ServiceResult{}, NewErrNotAcceptablef("Invalid follow activity: %v", err)
	}

	// The Follow must target this repository actor.
	objectID, err := fm.NewRepositoryID(follow.Object.GetID().String(), string(forgefed.ForgejoSourceType))
	if err != nil {
		return ServiceResult{}, NewErrNotAcceptablef("Parsing repo objectID failed: %v", err)
	}
	if objectID.ID != fmt.Sprint(repositoryID) {
		return ServiceResult{}, NewErrNotAcceptablef("Invalid repoId: %v", err)
	}

	// Resolve (or materialise) the remote follower. Repository actors (e.g.
	// remote pull mirrors) are recorded as repository followers; person actors
	// go through the existing federated-user path.
	actorURI := follow.Actor.GetID().String()
	if remoteRepo, err := FetchRepositoryActor(ctx, actorURI); err == nil && remoteRepo.Type == fm.RepositoryType {
		return processRepositoryActorFollow(ctx, repositoryID, actorURI, remoteRepo)
	}

	follower, federatedUser, federationHost, err := FindOrCreateFederatedUser(ctx, actorURI)
	if err != nil {
		log.Error("Federated user not found (%s): %v", actorURI, err)
		return ServiceResult{}, NewErrNotAcceptablef("Federated user not found: %v", err)
	}

	// Idempotent: if the remote user already follows this repository there is
	// nothing to do.
	alreadyFollowing, err := repo.IsFollowingRepo(ctx, follower.ID, repositoryID)
	if err != nil {
		return ServiceResult{}, NewErrInternalf("IsFollowingRepo: %v", err)
	}
	if alreadyFollowing {
		log.Trace("User[%d] is already following repository[%d]", follower.ID, repositoryID)
		return NewServiceResultStatusOnly(http.StatusNoContent), nil
	}

	if err := repo.AddRepoFollower(ctx, repositoryID, follower.ID); err != nil {
		log.Error("Unable to add repo follower: %v", err)
		return ServiceResult{}, NewErrNotAcceptablef("Unable to add repo follower: %v", err)
	}

	// Reply with an Accept so the remote side completes the handshake, signed
	// by the repository owner (the actor speaking for the repository).
	localRepo, err := repo.GetRepositoryByID(ctx, repositoryID)
	if err != nil {
		log.Error("Unable to load repository %d: %v", repositoryID, err)
		return ServiceResult{}, NewErrInternalf("Unable to load repository: %v", err)
	}
	owner, err := user_model.GetUserByID(ctx, localRepo.OwnerID)
	if err != nil {
		log.Error("Unable to load repository owner %d: %v", localRepo.OwnerID, err)
		return ServiceResult{}, NewErrInternalf("Unable to load repository owner: %v", err)
	}
	accept := ap.AcceptNew(ap.IRI(fmt.Sprintf(
		"%s#accepts/follow/%d", localRepo.APActorID(), repositoryID,
	)), follow)
	accept.Actor = ap.IRI(localRepo.APActorID())
	payload, err := jsonld.WithContext(jsonld.IRI(ap.ActivityBaseURI)).Marshal(accept)
	if err != nil {
		log.Error("Unable to Marshal JSON: %v", err)
		return ServiceResult{}, NewErrInternalf("MarshalJSON: %v", err)
	}

	hostURL := federationHost.AsURL()
	if err := deliveryQueue.Push(deliveryQueueItem{
		InboxURL: hostURL.JoinPath(federatedUser.InboxPath).String(),
		Doer:     owner,
		Payload:  payload,
	}); err != nil {
		log.Error("Unable to push to pending queue: %v", err)
		return ServiceResult{}, NewErrInternalf("Unable to push to pending queue: %v", err)
	}

	return NewServiceResultStatusOnly(http.StatusNoContent), nil
}

// processRepositoryActorFollow handles a Follow activity from a remote
// Repository actor (typically a pull mirror of the local repository). The
// follower is recorded so outbound Push activities are delivered to its
// repository inbox. Idempotent.
func processRepositoryActorFollow(ctx context.Context, repositoryID int64, actorURI string, remoteRepo *fm.Repository) (ServiceResult, error) {
	alreadyFollowing, err := repo.IsRemoteRepositoryFollowingRepo(ctx, repositoryID, actorURI)
	if err != nil {
		return ServiceResult{}, NewErrInternalf("IsRemoteRepositoryFollowingRepo: %v", err)
	}
	if alreadyFollowing {
		log.Trace("Repository[%s] is already following repository[%d]", actorURI, repositoryID)
		return NewServiceResultStatusOnly(http.StatusNoContent), nil
	}

	inboxURL := remoteRepo.Inbox.GetLink().String()
	if inboxURL == "" {
		return ServiceResult{}, NewErrNotAcceptablef("follower repository %s does not expose an inbox", actorURI)
	}

	if err := repo.AddRepositoryFollower(ctx, repositoryID, actorURI, inboxURL); err != nil {
		log.Error("Unable to add repository follower: %v", err)
		return ServiceResult{}, NewErrNotAcceptablef("Unable to add repository follower: %v", err)
	}

	log.Info("Repository %s is now following repository %d (pull mirror)", actorURI, repositoryID)
	return NewServiceResultStatusOnly(http.StatusNoContent), nil
}
