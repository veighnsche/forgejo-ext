// Copyright 2024 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repository

import (
	"context"

	"forgejo.org/models/repo"
	"forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/services/federation"
)

func StarRepoAndSendLikeActivities(ctx context.Context, doer user.User, repoID int64, star bool) error {
	if err := repo.StarRepo(ctx, doer.ID, repoID, star); err != nil {
		return err
	}

	if !setting.Federation.Enabled {
		return nil
	}

	// When a repository is starred we announce it with a Like activity;
	// when it is unstarred we send an Undo(Like) activity so distant
	// instances can keep their star count consistent.
	if star {
		if err := federation.SendLikeActivities(ctx, doer, repoID); err != nil {
			return err
		}
	} else {
		if err := federation.SendUndoLikeActivities(ctx, doer, repoID); err != nil {
			return err
		}
	}

	return nil
}
