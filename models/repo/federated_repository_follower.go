// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"context"

	"forgejo.org/models/db"
	"forgejo.org/modules/validation"
)

// FederatedRepositoryFollower represents a remote federated *repository*
// following a local repository actor. Unlike FederatedRepoFollower (remote
// users), this records remote Repository actors — typically pull mirrors of
// the local repository — so outbound Push activities can be delivered to
// their repository inboxes.
type FederatedRepositoryFollower struct {
	ID             int64  `xorm:"pk autoincr"`
	RepoID         int64  `xorm:"repo_id NOT NULL unique(frf_rel)"`
	RemoteActorURI string `xorm:"remote_actor_uri NOT NULL unique(frf_rel)"`
	// InboxURL is the repository inbox of the remote mirror.
	InboxURL string `xorm:"inbox_url NOT NULL"`
}

func init() {
	db.RegisterModel(new(FederatedRepositoryFollower))
}

// NewFederatedRepositoryFollower creates a FederatedRepositoryFollower. The
// created struct is asserted to be valid.
func NewFederatedRepositoryFollower(repoID int64, remoteActorURI, inboxURL string) (FederatedRepositoryFollower, error) {
	result := FederatedRepositoryFollower{
		RepoID:         repoID,
		RemoteActorURI: remoteActorURI,
		InboxURL:       inboxURL,
	}
	if valid, err := validation.IsValid(result); !valid {
		return FederatedRepositoryFollower{}, err
	}
	return result, nil
}

func (f FederatedRepositoryFollower) Validate() []string {
	var result []string
	result = append(result, validation.ValidateNotEmpty(f.RepoID, "RepoID")...)
	result = append(result, validation.ValidateNotEmpty(f.RemoteActorURI, "RemoteActorURI")...)
	result = append(result, validation.ValidateNotEmpty(f.InboxURL, "InboxURL")...)
	return result
}

// IsFollowingRepo reports whether the given remote repository actor already
// follows the local repository.
func IsRemoteRepositoryFollowingRepo(ctx context.Context, repoID int64, remoteActorURI string) (bool, error) {
	return db.GetEngine(ctx).Get(&FederatedRepositoryFollower{
		RepoID:         repoID,
		RemoteActorURI: remoteActorURI,
	})
}

// GetFederatedRepositoryFollowersByRepoID returns all remote repository
// followers of the given local repository.
func GetFederatedRepositoryFollowersByRepoID(ctx context.Context, repoID int64) ([]*FederatedRepositoryFollower, error) {
	followers := make([]*FederatedRepositoryFollower, 0, 4)
	err := db.GetEngine(ctx).Where("repo_id = ?", repoID).Find(&followers)
	if err != nil {
		return nil, err
	}
	return followers, nil
}

// AddRepositoryFollower records a remote repository actor following the local
// repository.
func AddRepositoryFollower(ctx context.Context, repoID int64, remoteActorURI, inboxURL string) error {
	follow, err := NewFederatedRepositoryFollower(repoID, remoteActorURI, inboxURL)
	if err != nil {
		return err
	}
	_, err = db.GetEngine(ctx).Insert(&follow)
	return err
}

// RemoveRepositoryFollower removes the follow relationship between the remote
// repository actor and the local repository, if present.
func RemoveRepositoryFollower(ctx context.Context, repoID int64, remoteActorURI string) error {
	_, err := db.GetEngine(ctx).Delete(&FederatedRepositoryFollower{
		RepoID:         repoID,
		RemoteActorURI: remoteActorURI,
	})
	return err
}
