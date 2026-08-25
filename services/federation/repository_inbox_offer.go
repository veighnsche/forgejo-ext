// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package federation

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"forgejo.org/models/forgefed"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	fm "forgejo.org/modules/forgefed"
	"forgejo.org/modules/git"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	pull_service "forgejo.org/services/pull"

	ap "github.com/go-ap/activitypub"
	"github.com/go-ap/jsonld"
	"github.com/google/uuid"
)

// processRepositoryOffer handles an inbound Offer(MergeRequest) activity: a
// remote user proposes a code change against a local repository. The proposed
// branch is fetched from the source repository via standard git, a local pull
// request is created with the remote user as author, and an Accept activity is
// delivered back.
//
// This is the ForgeFed "Federated Pull Requests" flow, restricted to forge
// peers (see FEP-5620). Cross-instance git access uses the source repository's
// public git endpoint, which keeps the feature interoperable with any git
// server that exposes repositories over HTTP(S).
func processRepositoryOffer(ctx context.Context, activity *ap.Activity, repositoryID int64) (ServiceResult, error) {
	// The offered object must be a MergeRequest (parsed via the ForgeFed
	// custom-type hook, which preserves the custom fields).
	var mr fm.ForgeMergeRequest
	if err := fm.OnMergeRequest(activity.Object, func(m *fm.ForgeMergeRequest) error {
		mr = *m
		return nil
	}); err != nil {
		log.Error("Invalid merge request object: %v", err)
		return ServiceResult{}, NewErrNotAcceptablef("Invalid merge request object: %v", err)
	}
	if mr.Type != fm.MergeRequestType {
		return ServiceResult{}, NewErrNotAcceptablef("offered object is not a MergeRequest")
	}

	// The proposal must target this repository actor.
	objectID, err := fm.NewRepositoryID(activity.Target.GetID().String(), string(forgefed.ForgejoSourceType))
	if err != nil {
		return ServiceResult{}, NewErrNotAcceptablef("Parsing repo objectID failed: %v", err)
	}
	if objectID.ID != fmt.Sprint(repositoryID) {
		return ServiceResult{}, NewErrNotAcceptablef("Invalid repoId: %v", err)
	}

	// Resolve (or materialise) the remote author and their host.
	actorURI := activity.Actor.GetID().String()
	poster, federatedUser, federationHost, err := FindOrCreateFederatedUser(ctx, actorURI)
	if err != nil {
		log.Error("Federated user not found (%s): %v", actorURI, err)
		return ServiceResult{}, NewErrNotAcceptablef("Federated user not found: %v", err)
	}

	localRepo, err := repo_model.GetRepositoryByID(ctx, repositoryID)
	if err != nil {
		return ServiceResult{}, NewErrInternalf("Unable to load repository: %v", err)
	}

	// Fetch the proposed branch from the source repository into the target
	// repository under a temporary ref.
	branch := fmt.Sprintf("federated/%s", uuid.New().String())
	if err := fetchMergeRequestBranch(ctx, localRepo, mr, branch); err != nil {
		log.Error("Unable to fetch federated PR branch from %s: %v", mr.SourceGitURL, err)
		return ServiceResult{}, NewErrNotAcceptablef("Unable to fetch federated PR branch: %v", err)
	}

	// Create the local pull request with the remote user as author.
	pullIssue := &issues_model.Issue{
		RepoID:   localRepo.ID,
		Repo:     localRepo,
		Title:    mr.Name.String(),
		PosterID: poster.ID,
		Poster:   poster,
		IsPull:   true,
		Content:  mr.Content.String(),
	}
	pullRequest := &issues_model.PullRequest{
		HeadRepoID: localRepo.ID,
		BaseRepoID: localRepo.ID,
		HeadBranch: branch,
		BaseBranch: mr.Ref,
		BaseRepo:   localRepo,
		Type:       issues_model.PullRequestGitea,
	}
	if err := pull_service.NewPullRequest(ctx, localRepo, pullIssue, nil, nil, pullRequest, nil); err != nil {
		log.Error("Unable to create federated pull request in repository %d: %v", repositoryID, err)
		return ServiceResult{}, NewErrNotAcceptablef("Unable to create federated pull request: %v", err)
	}

	// Reply with an Accept signed by the repository owner so the remote side
	// completes the handshake.
	accept := ap.AcceptNew(ap.IRI(fmt.Sprintf(
		"%s#accepts/offer/%d", localRepo.APActorID(), pullRequest.ID,
	)), activity)
	accept.Actor = ap.IRI(localRepo.APActorID())

	owner := localRepo.Owner
	if owner == nil {
		owner, err = user_model.GetUserByID(ctx, localRepo.OwnerID)
		if err != nil {
			log.Error("Unable to load repository owner %d: %v", localRepo.OwnerID, err)
			return ServiceResult{}, NewErrInternalf("Unable to load repository owner: %v", err)
		}
	}

	if payload, err := jsonld.WithContext(jsonld.IRI(ap.ActivityBaseURI)).Marshal(accept); err == nil {
		hostURL := federationHost.AsURL()
		if err := deliveryQueue.Push(deliveryQueueItem{
			InboxURL: hostURL.JoinPath(federatedUser.InboxPath).String(),
			Doer:     owner,
			Payload:  payload,
		}); err != nil {
			log.Warn("Unable to deliver Accept for federated PR: %v", err)
		}
	}

	return NewServiceResultStatusOnly(http.StatusNoContent), nil
}

// fetchMergeRequestBranch fetches the source branch from the source
// repository's git endpoint into the target repository under `refs/heads/{branch}`.

// fetchMergeRequestBranch fetches the source branch from the source
// repository's git endpoint into the target repository under `refs/heads/{branch}`.
func fetchMergeRequestBranch(ctx context.Context, localRepo *repo_model.Repository, mr fm.ForgeMergeRequest, branch string) error {
	// Only http/https endpoints are accepted; file endpoints are rejected by
	// validation. Fetch is restricted to forge peers via the source actor.
	if !strings.HasPrefix(mr.SourceGitURL, "http://") && !strings.HasPrefix(mr.SourceGitURL, "https://") {
		return fmt.Errorf("source git URL must use http or https")
	}
	if _, err := url.Parse(mr.SourceGitURL); err != nil {
		return fmt.Errorf("invalid source git URL: %w", err)
	}
	// Ensure the source host is a known federation host (forge peer), unless
	// running in dev mode (InsecureAllowInvalidHosts) where loopback sources
	// are used for testing.
	if !setting.Federation.InsecureAllowInvalidHosts {
		if _, err := FindOrCreateFederationHost(ctx, mr.Source.String()); err != nil {
			return fmt.Errorf("source is not a known federation host: %w", err)
		}
	}

	refspec := fmt.Sprintf("%s:refs/heads/%s", mr.SourceBranch, branch)
	if _, _, err := git.NewCommand(ctx, "fetch").
		AddDynamicArguments(mr.SourceGitURL, refspec).
		RunStdString(&git.RunOpts{
			Timeout: 120 * time.Second,
			Dir:     localRepo.RepoPath(),
		}); err != nil {
		return err
	}
	return nil
}
