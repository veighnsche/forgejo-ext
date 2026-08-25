// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// Package federationmirror implements the homeserver-side of the ForgeFed
// pull-mirror workflow: cloning a remote federated repository into a local
// pull mirror that follows the remote and keeps in sync via scheduled syncs
// and inbound Push activities.
package federationmirror

import (
	"context"
	"fmt"
	"strings"

	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	fm "forgejo.org/modules/forgefed"
	"forgejo.org/modules/graceful"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/structs"
	"forgejo.org/services/federation"
	"forgejo.org/services/migrations"
	repo_service "forgejo.org/services/repository"
)

// Options describes a federated mirror creation request: a remote federated
// repository (identified by its ForgeFed actor URI) is cloned into a local
// pull mirror.
type Options struct {
	RemoteActorURI string
	RepoName       string
	Description    string
	Private        bool
	MirrorInterval string
}

// CreateFederatedPullMirror creates a local pull mirror of a remote federated
// repository:
//
//  1. fetches the remote repository actor document and extracts its native git
//     clone endpoint (ForgeFed cloneUri);
//  2. creates a local pull mirror from that git URL (standard Forgejo mirror,
//     including the initial sync and the scheduled sync interval);
//  3. records the federation-side metadata (remote actor URI, remote clone
//     URI) so the actor JSON can declare the `mirrors` property and inbound
//     Push activities from the upstream can trigger immediate syncs;
//  4. follows the remote repository actor from the local mirror's repository
//     actor so the upstream notifies us of new pushes.
//
// This is the homeserver-side of the ForgeFed pull-mirror workflow (spec §3.3
// "Mirroring"): the mirror follows the upstream, the upstream publishes Push
// activities, the mirror pulls via cloneUri.
func CreateFederatedPullMirror(ctx context.Context, doer, owner *user_model.User, opts Options) (*repo_model.Repository, error) {
	remoteActorURI := strings.TrimSpace(opts.RemoteActorURI)
	if remoteActorURI == "" {
		return nil, fmt.Errorf("remote actor URI is required")
	}
	if !strings.HasPrefix(remoteActorURI, "http://") && !strings.HasPrefix(remoteActorURI, "https://") {
		return nil, fmt.Errorf("remote actor URI must use http or https")
	}

	// 1. Fetch and validate the remote repository actor.
	remoteRepo, err := federation.FetchRepositoryActor(ctx, remoteActorURI)
	if err != nil {
		return nil, fmt.Errorf("unable to resolve remote repository actor: %w", err)
	}
	if remoteRepo.Type != fm.RepositoryType {
		return nil, fmt.Errorf("remote actor %s is not a Repository actor", remoteActorURI)
	}
	cloneURI := strings.TrimSpace(remoteRepo.CloneURI)
	if !strings.HasPrefix(cloneURI, "http://") && !strings.HasPrefix(cloneURI, "https://") {
		return nil, fmt.Errorf("remote repository %s does not expose an http(s) clone URI (cloneUri)", remoteActorURI)
	}

	// Record the remote host (advisory: fails only for hosts without NodeInfo).
	if _, err := federation.FindOrCreateFederationHost(ctx, remoteActorURI); err != nil {
		log.Warn("Unable to record federation host for %s (mirror proceeds): %v", remoteActorURI, err)
	}

	// 2. Derive the local repository name.
	repoName := strings.TrimSpace(opts.RepoName)
	if repoName == "" {
		if remoteRepo.Name != nil && remoteRepo.Name.First().String() != "" {
			repoName = remoteRepo.Name.First().String()
		} else {
			repoName = repoNameFromCloneURI(cloneURI)
		}
	}

	// 3. Create the pull mirror (standard Forgejo mirror from the git URL).
	repo, err := repo_service.CreateRepositoryDirectly(ctx, doer, owner, repo_service.CreateRepoOptions{
		Name:           repoName,
		Description:    opts.Description,
		OriginalURL:    cloneURI,
		GitServiceType: structs.PlainGitService,
		IsPrivate:      opts.Private || setting.Repository.ForcePrivate,
		IsMirror:       true,
		Status:         repo_model.RepositoryBeingMigrated,
	})
	if err != nil {
		return nil, err
	}

	// If the migration fails after the repository record was created, remove
	// the half-created repository again.
	createdRepoID := repo.ID
	cleanupOnFailure := func() {
		if errDelete := repo_service.DeleteRepositoryDirectly(ctx, createdRepoID, repo_service.DeleteRepositoryOpts{}); errDelete != nil {
			log.Error("DeleteRepositoryDirectly: %v", errDelete)
		}
	}
	migrateOpts := migrations.MigrateOptions{
		CloneAddr:       cloneURI,
		RepoName:        repoName,
		Description:     opts.Description,
		Private:         opts.Private || setting.Repository.ForcePrivate,
		Mirror:          true,
		MirrorInterval:  opts.MirrorInterval,
		GitServiceType:  structs.PlainGitService,
		MigrateToRepoID: repo.ID,
	}

	repo, err = migrations.MigrateRepository(graceful.GetManager().HammerContext(), doer, owner.Name, migrateOpts, nil)
	if err != nil {
		cleanupOnFailure()
		return nil, err
	}

	// 4. Record the federation-side metadata.
	if err := repo_model.AddFederatedMirror(ctx, repo.ID, remoteActorURI, cloneURI, false); err != nil {
		return nil, err
	}

	// 5. Follow the remote repository actor from the local repository actor so
	// the upstream notifies us of new pushes (inbound Push -> sync).
	if err := federation.SendRepositoryFollow(ctx, doer, repo, remoteActorURI); err != nil {
		log.Warn("Unable to send follow for federated mirror %d to %s (scheduled syncs will still apply): %v", repo.ID, remoteActorURI, err)
	}

	return repo, nil
}

// repoNameFromCloneURI derives a repository name from a git clone URL by
// taking the last path segment (minus any ".git" suffix).
func repoNameFromCloneURI(cloneURI string) string {
	trimmed := strings.TrimSuffix(cloneURI, "/")
	if idx := strings.LastIndex(trimmed, "/"); idx >= 0 {
		trimmed = trimmed[idx+1:]
	}
	return strings.TrimSuffix(trimmed, ".git")
}
