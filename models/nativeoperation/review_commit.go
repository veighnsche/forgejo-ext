// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"context"
)

// CommitReviewSubmitPrimary atomically orders one pull_request.review.submit
// primary commit against cancellation and expiry, runs insert, and records
// the committed receipt while the owner stays held for bounded completion.
// The caller verifies exact refs and native authority under the held
// reservation before calling; this transaction performs SQL only, never Git.
//
// The shared conditional-primary ordering applies: a concurrent cancellation
// either wins (nothing commits) or loses (the committed row reads too_late).
// An insert failure rolls the whole transaction back: no partial review rows
// survive and the operation row is untouched for the caller to refuse.
func CommitReviewSubmitPrimary(ctx context.Context, installationID, operationID, owner string, nowUnix int64, insert func(ctx context.Context) (receipt string, err error)) (*Operation, error) {
	return commitConditionalPrimary(ctx, installationID, operationID, owner, nowUnix, insert)
}
