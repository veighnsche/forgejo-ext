// Copyright 2025 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package federation

import (
	"context"

	fm "forgejo.org/modules/forgefed"

	ap "github.com/go-ap/activitypub"
)

func ProcessRepositoryInbox(ctx context.Context, activity *ap.Activity, repositoryID int64) (ServiceResult, error) {
	switch activity.Type {
	case ap.LikeType:
		return ProcessLikeActivity(ctx, activity, repositoryID)
	case ap.UndoType:
		// An Undo(Follow) is an unfollow; anything else is an unstar.
		if inner, ok := activity.Object.(*ap.Activity); ok && inner.Type == ap.FollowType {
			return processRepositoryUnfollow(ctx, activity, repositoryID)
		}
		return ProcessUndoLikeActivity(ctx, activity, repositoryID)
	case ap.FollowType:
		return processRepositoryFollow(ctx, activity, repositoryID)
	case ap.OfferType:
		return processRepositoryOffer(ctx, activity, repositoryID)
	case fm.PushType:
		return processRepositoryPush(ctx, activity, repositoryID)
	case ap.FlagType:
		return processFlagActivity(ctx, activity)
	default:
		return ServiceResult{}, NewErrNotAcceptablef("Not a like, undo, follow, offer, push or flag activity: %v", activity.Type)
	}
}
