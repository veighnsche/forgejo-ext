// Copyright 2025 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package federation

import (
	"context"

	ap "github.com/go-ap/activitypub"
)

func ProcessRepositoryInbox(ctx context.Context, activity *ap.Activity, repositoryID int64) (ServiceResult, error) {
	switch activity.Type {
	case ap.LikeType:
		return ProcessLikeActivity(ctx, activity, repositoryID)
	case ap.UndoType:
		return ProcessRepositoryInboxUndoLike(ctx, activity, repositoryID)
	default:
		return ServiceResult{}, NewErrNotAcceptablef("Unhandled activity: %v", activity.Type)
	}
}
