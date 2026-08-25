// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"context"

	"forgejo.org/models/db"
	"forgejo.org/modules/validation"
)

// FederatedRepoFollower represents a remote federated user following a local
// repository actor (the "Following for Repositories" roadmap feature). It is
// the repository-level analogue of models/user.FederatedUserFollower.
type FederatedRepoFollower struct {
	ID     int64 `xorm:"pk autoincr"`
	RepoID int64 `xorm:"NOT NULL unique(frf_rel)"`
	// UserID is the local User record materialised for the remote follower.
	UserID int64 `xorm:"NOT NULL unique(frf_rel)"`
}

func init() {
	db.RegisterModel(new(FederatedRepoFollower))
}

// NewFederatedRepoFollower creates a FederatedRepoFollower. The created struct
// is asserted to be valid.
func NewFederatedRepoFollower(repoID, userID int64) (FederatedRepoFollower, error) {
	result := FederatedRepoFollower{
		RepoID: repoID,
		UserID: userID,
	}
	if valid, err := validation.IsValid(result); !valid {
		return FederatedRepoFollower{}, err
	}
	return result, nil
}

func (f FederatedRepoFollower) Validate() []string {
	var result []string
	result = append(result, validation.ValidateNotEmpty(f.RepoID, "RepoID")...)
	result = append(result, validation.ValidateNotEmpty(f.UserID, "UserID")...)
	return result
}

// IsFollowingRepo reports whether the given user (the local materialisation of
// a remote follower) already follows the repository.
func IsFollowingRepo(ctx context.Context, userID, repoID int64) (bool, error) {
	return db.GetEngine(ctx).Get(&FederatedRepoFollower{
		RepoID: repoID,
		UserID: userID,
	})
}

// GetFederatedRepoFollowersByRepoID returns all federated followers of the
// given repository.
func GetFederatedRepoFollowersByRepoID(ctx context.Context, repoID int64) ([]*FederatedRepoFollower, error) {
	followers := make([]*FederatedRepoFollower, 0, 8)
	err := db.GetEngine(ctx).Where("repo_id = ?", repoID).Find(&followers)
	if err != nil {
		return nil, err
	}
	return followers, nil
}

func AddRepoFollower(ctx context.Context, repoID, userID int64) error {
	follow, err := NewFederatedRepoFollower(repoID, userID)
	if err != nil {
		return err
	}
	_, err = db.GetEngine(ctx).Insert(&follow)
	return err
}

// RemoveRepoFollower removes the follow relationship between the user and the
// repository, if present.
func RemoveRepoFollower(ctx context.Context, repoID, userID int64) error {
	_, err := db.GetEngine(ctx).Delete(&FederatedRepoFollower{
		RepoID: repoID,
		UserID: userID,
	})
	return err
}
