// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"context"
)

// CommitLifecycleNotePrimary atomically orders one issue.comment.publish
// primary commit against cancellation and expiry, runs insert, and records
// the committed receipt while the owner stays held for bounded completion.
// An insert failure rolls the whole transaction back: no partial comment
// survives and the operation row is untouched for the caller to refuse.
func CommitLifecycleNotePrimary(ctx context.Context, installationID, operationID, owner string, nowUnix int64, insert func(ctx context.Context) (receipt string, err error)) (*Operation, error) {
	return commitConditionalPrimary(ctx, installationID, operationID, owner, nowUnix, insert)
}
