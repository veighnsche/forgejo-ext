// Copyright 2022 The Gitea Authors. All rights reserved.
// Copyright 2024 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package structs

// ActivityPub type
type ActivityPub struct {
	Context string `json:"@context"`
}

// swagger:model
type APRemoteFollowOption struct {
	Target string `json:"target"`
}

// FederatedMirrorOption options when creating a federated pull mirror of a
// remote repository (identified by its ForgeFed repository actor URI).
// swagger:model
type FederatedMirrorOption struct {
	// RemoteActorURI is the ForgeFed actor URI of the remote repository to mirror.
	RemoteActorURI string `json:"remote_actor_uri" binding:"Required"`
	// RepoOwner is the name of the local user or organization that will own the mirror.
	RepoOwner string `json:"repo_owner"`
	// RepoName is the name of the local mirror repository. Defaults to the remote repository name.
	RepoName string `json:"repo_name"`
	// Description of the local mirror repository.
	Description string `json:"description"`
	// Private makes the local mirror repository private.
	Private bool `json:"private"`
	// MirrorInterval is the scheduled sync interval (e.g. "8h"). Empty disables scheduled syncs.
	MirrorInterval string `json:"mirror_interval"`
}

// FederatedPushMirrorOption options when creating a federated push mirror:
// a local repository pushes its changes to a remote federated repository
// (identified by its ForgeFed repository actor URI).
// swagger:model
type FederatedPushMirrorOption struct {
	// RemoteActorURI is the ForgeFed actor URI of the remote repository to push to.
	RemoteActorURI string `json:"remote_actor_uri" binding:"Required"`
	// Interval is the scheduled push interval (e.g. "8h"). Empty means sync on commit.
	Interval string `json:"interval"`
	// SyncOnCommit pushes to the remote on every commit.
	SyncOnCommit bool `json:"sync_on_commit"`
	// BranchFilter restricts which branches are pushed.
	BranchFilter string `json:"branch_filter"`
	// RemoteUsername/RemotePassword authenticate the git push to the remote.
	RemoteUsername string `json:"remote_username"`
	RemotePassword string `json:"remote_password"`
}

type APPersonFollowItem struct {
	ActorID string `json:"actor_id"`
	Note    string `json:"note"`

	OriginalURL  string `json:"original_url"`
	OriginalItem string `json:"original_item"`
}
