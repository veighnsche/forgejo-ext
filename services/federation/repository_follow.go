// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package federation

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/user"
	"forgejo.org/modules/forgefed"
	"forgejo.org/modules/log"
	app_context "forgejo.org/services/context"

	ap "github.com/go-ap/activitypub"
	"github.com/go-ap/jsonld"
)

// FollowRemoteRepository sends a Follow activity to the inbox of a remote
// repository actor so the local user subscribes to it. It is the
// repository-level mirror of FollowRemoteActor. The relationship is recorded
// on the remote side (like user follows); the local side has no record, which
// matches the existing user-follow behaviour.
func FollowRemoteRepository(ctx *app_context.APIContext, localUser *user.User, repoURI string) error {
	// Light validation: the target must parse as a repository actor.
	if _, err := forgefed.NewActorID(repoURI); err != nil {
		log.Error("Invalid repository URI (%s): %v", repoURI, err)
		ctx.Error(http.StatusNotAcceptable, "Invalid repository URI", err)
		return err
	}
	if !strings.Contains(repoURI, "/repository-id/") {
		log.Error("Not a repository actor URI (%s)", repoURI)
		ctx.Error(http.StatusNotAcceptable, "Not a repository actor URI", repoURI)
		return fmt.Errorf("not a repository actor URI: %s", repoURI)
	}

	followReq, err := forgefed.NewForgeFollow(localUser.APActorID(), repoURI)
	if err != nil {
		return err
	}

	payload, err := jsonld.WithContext(jsonld.IRI(ap.ActivityBaseURI)).
		Marshal(followReq)
	if err != nil {
		return err
	}

	return deliveryQueue.Push(deliveryQueueItem{
		InboxURL: fmt.Sprintf("%v/inbox", repoURI),
		Doer:     localUser,
		Payload:  payload,
	})
}

// UnfollowRemoteRepository sends an Undo(Follow) activity to the inbox of a
// remote repository actor, notifying it that the local repository no longer
// follows (mirrors) it. It is used when a federated pull mirror is deleted.
func UnfollowRemoteRepository(ctx context.Context, localUser *user.User, localRepo *repo_model.Repository, repoURI string) error {
	unfollowReq, err := forgefed.NewForgeUndoFollow(localRepo.APActorID(), repoURI, time.Now())
	if err != nil {
		return err
	}

	payload, err := jsonld.WithContext(jsonld.IRI(ap.ActivityBaseURI)).Marshal(unfollowReq)
	if err != nil {
		return err
	}

	return deliveryQueue.Push(deliveryQueueItem{
		InboxURL: fmt.Sprintf("%v/inbox", repoURI),
		Doer:     localUser,
		Payload:  payload,
	})
}
