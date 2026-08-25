// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package federation

import (
	"context"
	"fmt"
	"net/url"

	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/activitypub"
	fm "forgejo.org/modules/forgefed"

	ap "github.com/go-ap/activitypub"
	"github.com/go-ap/jsonld"
)

// FetchRepositoryActor fetches and parses the ForgeFed Repository actor
// document at the given URI. It is used by the federated mirror flow to
// discover the remote repository's native git clone endpoint (cloneUri).
func FetchRepositoryActor(ctx context.Context, actorURI string) (*fm.Repository, error) {
	actionsUser := user_model.NewAPServerActor()

	clientFactory, err := activitypub.GetClientFactory(ctx)
	if err != nil {
		return nil, err
	}

	parsed, err := url.Parse(actorURI)
	if err != nil {
		return nil, fmt.Errorf("invalid actor URI: %w", err)
	}
	hostURL := &url.URL{Scheme: parsed.Scheme, Host: parsed.Host}

	apClient, err := clientFactory.WithKeys(ctx, actionsUser, actionsUser.KeyID(), []*url.URL{hostURL})
	if err != nil {
		return nil, err
	}

	body, err := apClient.GetBody(actorURI)
	if err != nil {
		return nil, err
	}

	repo := fm.Repository{}
	if err := repo.UnmarshalJSON(body); err != nil {
		return nil, err
	}
	return &repo, nil
}

// SendRepositoryFollow sends a Follow activity from the local repository actor
// to the inbox of the remote repository actor, signed by the repository
// owner. The remote records the mirror as a follower and will deliver Push
// activities to our repository inbox, which is what keeps federated pull
// mirrors in sync.
func SendRepositoryFollow(ctx context.Context, owner *user_model.User, localRepo *repo_model.Repository, remoteActorURI string) error {
	followReq, err := fm.NewForgeFollow(localRepo.APActorID(), remoteActorURI)
	if err != nil {
		return err
	}

	payload, err := jsonld.WithContext(jsonld.IRI(ap.ActivityBaseURI)).Marshal(followReq)
	if err != nil {
		return err
	}

	return deliveryQueue.Push(deliveryQueueItem{
		InboxURL: fmt.Sprintf("%s/inbox", remoteActorURI),
		Doer:     owner,
		Payload:  payload,
	})
}
