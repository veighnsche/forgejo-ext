// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"context"
)

// CommitLifecycleReviewPrimary atomically orders one
// pull_request.review.request primary commit against cancellation and
// expiry, runs insert, and records the committed receipt while the owner
// stays held for bounded completion. An insert failure rolls the whole
// transaction back: no partial review-request rows survive and the
// operation row is untouched for the caller to refuse.
func CommitLifecycleReviewPrimary(ctx context.Context, installationID, operationID, owner string, nowUnix int64, insert func(ctx context.Context) (receipt string, err error)) (*Operation, error) {
	return commitConditionalPrimary(ctx, installationID, operationID, owner, nowUnix, insert)
}
