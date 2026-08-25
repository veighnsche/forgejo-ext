// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package federationmirror

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	fm "forgejo.org/modules/forgefed"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/util"
	"forgejo.org/services/federation"
	mirror_service "forgejo.org/services/mirror"

	ap "github.com/go-ap/activitypub"
)

// PushMirrorOptions describes a federated push mirror creation request: a local
// repository pushes its changes to a remote federated repository (identified
// by its ForgeFed actor URI), using the remote's ForgeFed pushUri as the git
// endpoint.
type PushMirrorOptions struct {
	RemoteActorURI string
	Interval       string
	SyncOnCommit   bool
	BranchFilter   string
	// RemoteUsername/RemotePassword authenticate the git push to the target.
	RemoteUsername string
	RemotePassword string
}

// CreateFederatedPushMirror sets up a push mirror from a local repository to a
// remote federated repository:
//
//  1. fetches the remote repository actor and extracts its ForgeFed pushUri
//     (falling back to cloneUri);
//  2. creates a native Forgejo push mirror pointing at that git endpoint;
//  3. records the federation-side metadata (FederatedMirror with IsPush=true)
//     so the local repository's actor JSON declares the `mirrorsTo` property.
//
// The actual git pushes use the standard push-mirror machinery; the target
// must accept the push (write access), which is configured on the push mirror
// (credentials or SSH key) like any native push mirror.
func CreateFederatedPushMirror(ctx context.Context, repo *repo_model.Repository, opts PushMirrorOptions) (*repo_model.PushMirror, error) {
	remoteActorURI := strings.TrimSpace(opts.RemoteActorURI)
	if remoteActorURI == "" {
		return nil, fmt.Errorf("remote actor URI is required")
	}

	// 1. Resolve the target repository actor and its git push endpoint.
	remoteRepo, err := federation.FetchRepositoryActor(ctx, remoteActorURI)
	if err != nil {
		return nil, fmt.Errorf("unable to resolve remote repository actor: %w", err)
	}
	pushURI := pushURIFromActor(remoteRepo)
	if pushURI == "" {
		return nil, fmt.Errorf("remote repository %s does not expose a pushUri (or cloneUri)", remoteActorURI)
	}

	// Build the authenticated address.
	address := pushURI
	if opts.RemoteUsername != "" || opts.RemotePassword != "" {
		if parsed, err := url.Parse(pushURI); err == nil {
			parsed.User = url.UserPassword(opts.RemoteUsername, opts.RemotePassword)
			address = parsed.String()
		}
	}

	// 2. Create the native push mirror.
	interval, err := parseMirrorInterval(opts.Interval)
	if err != nil {
		return nil, err
	}
	remoteSuffix := util.CryptoRandomString(util.RandomStringLow)
	pushMirror := &repo_model.PushMirror{
		RepoID:        repo.ID,
		Repo:          repo,
		RemoteName:    fmt.Sprintf("remote_mirror_%s", remoteSuffix),
		Interval:      interval,
		SyncOnCommit:  opts.SyncOnCommit,
		RemoteAddress: address,
		BranchFilter:  opts.BranchFilter,
	}
	if err := db.Insert(ctx, pushMirror); err != nil {
		return nil, err
	}

	// Register the git remote for the push mirror (as the native flow does).
	if err := mirror_service.AddPushMirrorRemote(ctx, pushMirror, address); err != nil {
		_ = repo_model.DeletePushMirrors(ctx, repo_model.PushMirrorOptions{ID: pushMirror.ID, RepoID: pushMirror.RepoID})
		return nil, err
	}

	// 3. Record the federation-side metadata (mirrorsTo).
	if err := repo_model.AddFederatedMirror(ctx, repo.ID, remoteActorURI, pushURI, true); err != nil {
		return nil, err
	}

	return pushMirror, nil
}

// pushURIFromActor extracts the first http(s) push endpoint from the remote
// repository actor (the ForgeFed pushUri, which may be a single IRI or an
// ItemCollection), falling back to its cloneUri.
func pushURIFromActor(remoteRepo *fm.Repository) string {
	var candidates []ap.Item
	switch v := remoteRepo.PushURI.(type) {
	case ap.IRI:
		candidates = []ap.Item{v}
	case *ap.IRI:
		candidates = []ap.Item{*v}
	case ap.ItemCollection:
		candidates = v
	case *ap.ItemCollection:
		candidates = *v
	}
	for _, candidate := range candidates {
		s := candidate.GetLink().String()
		if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
			return s
		}
	}
	if strings.HasPrefix(remoteRepo.CloneURI, "http://") || strings.HasPrefix(remoteRepo.CloneURI, "https://") {
		return remoteRepo.CloneURI
	}
	return ""
}

// parseMirrorInterval parses a mirror interval; empty means sync on commit
// only, and any non-zero interval must be at least the configured minimum.
func parseMirrorInterval(interval string) (time.Duration, error) {
	if interval == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(interval)
	if err != nil || (d != 0 && d < setting.Mirror.MinInterval) {
		return 0, fmt.Errorf("invalid mirror interval %q", interval)
	}
	return d, nil
}
