// Copyright 2024 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package federation

import (
	"context"
	"net/http"
	"time"

	"forgejo.org/models/user"
	"forgejo.org/modules/forgefed"
	"forgejo.org/modules/log"

	ap "github.com/go-ap/activitypub"
	"github.com/go-ap/jsonld"
)

func ProcessPersonInbox(ctx context.Context, user *user.User, activity *ap.Activity) (ServiceResult, error) {
	switch activity.Type {
	case ap.CreateType:
		return processPersonInboxCreate(ctx, user, activity)
	case ap.FollowType:
		return processPersonFollow(ctx, user, activity)
	case ap.UndoType:
		return processPersonInboxUndo(ctx, user, activity)
	case ap.AcceptType:
		return processPersonInboxAccept(activity)
	case ap.FlagType:
		return processFlagActivity(ctx, activity)
	// Fediverse-wide activities that Forgejo does not model but must accept
	// gracefully so peers (Mastodon, GoToSocial, ...) do not get 406 errors:
	// Announce (boost), Delete (removed actor/note), Update (profile edit),
	// Push (a remote repository the user follows has new commits).
	case ap.AnnounceType, ap.DeleteType, ap.UpdateType, forgefed.PushType:
		log.Trace("Ignoring PersonInbox activity: %v", activity.Type)
		return NewServiceResultStatusOnly(http.StatusNoContent), nil
	}

	log.Error("Unsupported PersonInbox activity: %v", activity.Type)
	return ServiceResult{}, NewErrNotAcceptablef("unsupported activity: %v", activity.Type)
}

func FollowRemoteActor(ctx context.Context, localUser *user.User, actorURI string) error {
	_, federatedUser, federationHost, err := FindOrCreateFederatedUser(ctx, actorURI)
	if err != nil {
		log.Error("Federated user not found (%s): %v", actorURI, err)
		return err
	}

	followReq, err := forgefed.NewForgeFollow(localUser.APActorID(), actorURI)
	if err != nil {
		return err
	}

	payload, err := jsonld.WithContext(jsonld.IRI(ap.ActivityBaseURI)).
		Marshal(followReq)
	if err != nil {
		return err
	}

	hostURL := federationHost.AsURL()
	return deliveryQueue.Push(deliveryQueueItem{
		InboxURL: hostURL.JoinPath(federatedUser.InboxPath).String(),
		Doer:     localUser,
		Payload:  payload,
	})
}

// UnfollowRemoteActor sends an Undo(Follow) activity to the inbox of the
// remote actor, notifying it that the local user no longer follows it.
// It is the mirror of FollowRemoteActor and keeps follow state consistent
// between instances.
func UnfollowRemoteActor(ctx context.Context, localUser *user.User, actorURI string) error {
	_, federatedUser, federationHost, err := FindOrCreateFederatedUser(ctx, actorURI)
	if err != nil {
		log.Error("Federated user not found (%s): %v", actorURI, err)
		return err
	}

	unfollowReq, err := forgefed.NewForgeUndoFollow(localUser.APActorID(), actorURI, time.Now())
	if err != nil {
		return err
	}

	payload, err := jsonld.WithContext(jsonld.IRI(ap.ActivityBaseURI)).
		Marshal(unfollowReq)
	if err != nil {
		return err
	}

	hostURL := federationHost.AsURL()
	return deliveryQueue.Push(deliveryQueueItem{
		InboxURL: hostURL.JoinPath(federatedUser.InboxPath).String(),
		Doer:     localUser,
		Payload:  payload,
	})
}
