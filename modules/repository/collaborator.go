// Copyright 2022 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repository

import (
	"context"
	"fmt"

	"forgejo.org/models/db"
	"forgejo.org/models/perm"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"

	"xorm.io/builder"
)

func AddCollaborator(ctx context.Context, repo *repo_model.Repository, u *user_model.User) error {
	if user_model.IsBlocked(ctx, repo.OwnerID, u.ID) || user_model.IsBlocked(ctx, u.ID, repo.OwnerID) {
		return user_model.ErrBlockedByUser
	}

	return db.WithTx(ctx, func(ctx context.Context) error {
		has, err := db.Exist[repo_model.Collaboration](ctx, builder.Eq{
			"repo_id": repo.ID,
			"user_id": u.ID,
		})
		if err != nil {
			return err
		} else if has {
			return nil
		}

		if err = db.Insert(ctx, &repo_model.Collaboration{
			RepoID: repo.ID,
			UserID: u.ID,
			Mode:   perm.AccessModeWrite,
		}); err != nil {
			return err
		}

		return access_model.RecalculateUserAccess(ctx, repo, u.ID)
	})
}

// ChangeCollaborationAccessMode sets new access mode for the collaboration.
func ChangeCollaborationAccessMode(ctx context.Context, repo *repo_model.Repository, uid int64, mode perm.AccessMode) error {
	// Discard invalid input. Collaborator should not be able to become owner via
	// collaboration, at most it is a repository admin.
	if mode <= perm.AccessModeNone || mode >= perm.AccessModeOwner {
		return nil
	}

	return db.WithTx(ctx, func(ctx context.Context) error {
		e := db.GetEngine(ctx)

		collaboration := &repo_model.Collaboration{
			RepoID: repo.ID,
			UserID: uid,
		}
		has, err := e.Get(collaboration)
		if err != nil {
			return fmt.Errorf("get collaboration: %w", err)
		} else if !has {
			return nil
		}

		if collaboration.Mode == mode {
			return nil
		}
		collaboration.Mode = mode

		if _, err = e.
			ID(collaboration.ID).
			Cols("mode").
			Update(collaboration); err != nil {
			return fmt.Errorf("update collaboration: %w", err)
		} else if _, err = e.Exec("UPDATE access SET mode = ? WHERE user_id = ? AND repo_id = ?", mode, uid, repo.ID); err != nil {
			return fmt.Errorf("update access table: %w", err)
		}

		return nil
	})
}
