// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"context"
	"fmt"

	"forgejo.org/models/db"
	"forgejo.org/modules/validation"
)

// FederatedMirror records the federation-side metadata of a local repository
// that is a mirror of a remote federated repository:
//
//   - a pull mirror (IsPush=false): the local repository clones from the remote
//     repository's git endpoint (RemoteCloneURI) and follows the remote actor
//     (RemoteActorURI) so it receives Push activities that trigger syncs;
//   - a push mirror (IsPush=true): the local repository pushes to the remote
//     repository (RemoteActorURI) over the native git protocol.
//
// The remote actor URI is stored separately from the git URL because ForgeFed
// actor URIs are not derivable from the git clone URL.
type FederatedMirror struct {
	ID             int64  `xorm:"pk autoincr"`
	RepoID         int64  `xorm:"repo_id NOT NULL UNIQUE"`
	RemoteActorURI string `xorm:"remote_actor_uri NOT NULL"`
	RemoteCloneURI string `xorm:"remote_clone_uri NOT NULL"`
	IsPush         bool   `xorm:"is_push NOT NULL DEFAULT false"`
}

func init() {
	db.RegisterModel(new(FederatedMirror))
}

// NewFederatedMirror creates a FederatedMirror. The created struct is asserted
// to be valid.
func NewFederatedMirror(repoID int64, remoteActorURI, remoteCloneURI string, isPush bool) (FederatedMirror, error) {
	result := FederatedMirror{
		RepoID:         repoID,
		RemoteActorURI: remoteActorURI,
		RemoteCloneURI: remoteCloneURI,
		IsPush:         isPush,
	}
	if valid, err := validation.IsValid(result); !valid {
		return FederatedMirror{}, err
	}
	return result, nil
}

func (f FederatedMirror) Validate() []string {
	var result []string
	result = append(result, validation.ValidateNotEmpty(f.RepoID, "RepoID")...)
	result = append(result, validation.ValidateNotEmpty(f.RemoteActorURI, "RemoteActorURI")...)
	result = append(result, validation.ValidateNotEmpty(f.RemoteCloneURI, "RemoteCloneURI")...)
	return result
}

// GetFederatedMirrorByRepoID returns the federated mirror record for the given
// repository, if present.
func GetFederatedMirrorByRepoID(ctx context.Context, repoID int64) (*FederatedMirror, error) {
	mirror := new(FederatedMirror)
	has, err := db.GetEngine(ctx).Where("repo_id = ?", repoID).Get(mirror)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrFederatedMirrorNotExists{RepoID: repoID}
	}
	return mirror, nil
}

// ErrFederatedMirrorNotExists represents a repository that has no federated
// mirror record.
type ErrFederatedMirrorNotExists struct {
	RepoID int64
}

func (err ErrFederatedMirrorNotExists) Error() string {
	return fmt.Sprintf("no federated mirror record found for repository %d", err.RepoID)
}

func IsErrFederatedMirrorNotExists(err error) bool {
	_, ok := err.(ErrFederatedMirrorNotExists)
	return ok
}

// AddFederatedMirror records the federated mirror metadata for a repository.
func AddFederatedMirror(ctx context.Context, repoID int64, remoteActorURI, remoteCloneURI string, isPush bool) error {
	mirror, err := NewFederatedMirror(repoID, remoteActorURI, remoteCloneURI, isPush)
	if err != nil {
		return err
	}
	_, err = db.GetEngine(ctx).Insert(&mirror)
	return err
}

// RemoveFederatedMirror removes the federated mirror record for a repository,
// if present.
func RemoveFederatedMirror(ctx context.Context, repoID int64) error {
	_, err := db.GetEngine(ctx).Delete(&FederatedMirror{RepoID: repoID})
	return err
}
