// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package federation

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/user"
	fm "forgejo.org/modules/forgefed"
	"forgejo.org/modules/log"
	"forgejo.org/modules/repository"
	"forgejo.org/modules/setting"

	ap "github.com/go-ap/activitypub"
	"github.com/go-ap/jsonld"
)

// SendRepositoryPushActivity publishes a ForgeFed Push activity to the
// federated followers of a repository after a local git push. The activity is
// authored by the Repository actor (whose public key is the repository
// owner's key) and carries the pushed commits together with the hashes before
// and after the push, as defined by https://forgefed.org/ns#Push.
//
// Remote pull mirrors of the repository receive this activity and use it to
// trigger an immediate sync over the native git protocol (see
// processRepositoryPush on the receiving side).
func SendRepositoryPushActivity(ctx context.Context, pusher *user.User, repo *repo_model.Repository, opts *repository.PushUpdateOptions, commits *repository.PushCommits) error {
	if !setting.Federation.Enabled {
		return nil
	}
	if repo.IsPrivate {
		// Private repositories are never federated.
		return nil
	}
	// Only branch pushes carry new commits; tag, delete and new-branch
	// notifications are not Push activities.
	if !opts.RefFullName.IsBranch() || opts.IsDelRef() {
		return nil
	}

	owner := repo.Owner
	if owner == nil {
		var err error
		if owner, err = user.GetUserByID(ctx, repo.OwnerID); err != nil {
			return err
		}
	}

	branch := opts.RefFullName.BranchName()
	repoActorID := repo.APActorID()

	push := fm.NewForgePush(
		ap.IRI(fmt.Sprintf("%s/outbox/push/%s/%s", repoActorID, url.PathEscape(branch), opts.NewCommitID)),
		ap.IRI(repoActorID),
	)
	push.AttributedTo = ap.IRI(pusher.APActorID())
	push.To = ap.ItemCollection{ap.IRI(repoActorID + "/followers")}
	push.Context = ap.IRI(repoActorID)
	push.Target = ap.IRI(fmt.Sprintf("%s/branches/%s", repoActorID, url.PathEscape(branch)))
	push.HashBefore = opts.OldCommitID
	push.HashAfter = opts.NewCommitID

	// The pushed commits, newest first (the order produced by the push
	// notification pipeline).
	items := make(ap.ItemCollection, 0, len(commits.Commits))
	for _, pc := range commits.Commits {
		summary := strings.SplitN(pc.Message, "\n", 2)[0]
		items = append(items, fm.NewForgeCommit(
			ap.IRI(fmt.Sprintf("%s/commits/%s", repoActorID, pc.Sha1)),
			pc.Sha1,
			summary,
		))
	}
	collection := ap.OrderedCollectionNew(ap.IRI(repoActorID + "/outbox"))
	collection.TotalItems = uint(len(items))
	collection.OrderedItems = items
	push.Object = collection

	payload, err := jsonld.WithContext(jsonld.IRI(ap.ActivityBaseURI)).Marshal(push)
	if err != nil {
		return err
	}

	// Deliver to the repository's federated followers, signed by the
	// repository owner (the actor speaking for the repository).
	repoFollowers, err := repo_model.GetFederatedRepoFollowersByRepoID(ctx, repo.ID)
	if err != nil {
		return err
	}
	for _, repoFollower := range repoFollowers {
		if err := deliverToFederatedUser(ctx, owner, payload, repoFollower.UserID); err != nil {
			log.Warn("Unable to deliver Push activity to federated follower %d of repository %d: %v", repoFollower.UserID, repo.ID, err)
		}
	}

	// Deliver to remote repository followers (pull mirrors of this repository)
	// directly at their repository inboxes.
	repositoryFollowers, err := repo_model.GetFederatedRepositoryFollowersByRepoID(ctx, repo.ID)
	if err != nil {
		return err
	}
	for _, repositoryFollower := range repositoryFollowers {
		if err := deliveryQueue.Push(deliveryQueueItem{
			InboxURL: repositoryFollower.InboxURL,
			Doer:     owner,
			Payload:  payload,
		}); err != nil {
			log.Warn("Unable to deliver Push activity to mirror follower %s of repository %d: %v", repositoryFollower.RemoteActorURI, repo.ID, err)
		}
	}

	return nil
}
