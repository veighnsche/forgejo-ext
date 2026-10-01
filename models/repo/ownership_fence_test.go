// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"testing"

	nativeoperation "forgejo.org/models/nativeoperation"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/require"
)

func claimTestOwner(t *testing.T) string {
	t.Helper()
	claimed, err := nativeoperation.ClaimOrdinary(t.Context(), "ord:repository/test/abc123", `{"kind":"ordinary","family":"repository"}`, "v")
	require.NoError(t, err)
	t.Cleanup(func() { _ = nativeoperation.ReleaseOwner(t.Context(), claimed.Owner) })
	return claimed.Owner
}

func TestRepoWritersFencesWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	claimTestOwner(t)

	require.ErrorIs(t, UpdateRepoSize(ctx, 1, 100, 10), nativeoperation.ErrBusy)
	require.ErrorIs(t, UpdateRepoUnit(ctx, &RepoUnit{ID: 1}), nativeoperation.ErrBusy)
	require.ErrorIs(t, UpdateAttachment(ctx, &Attachment{ID: 1}), nativeoperation.ErrBusy)
	require.ErrorIs(t, UpdateAttachmentByUUID(ctx, &Attachment{UUID: "nope"}), nativeoperation.ErrBusy)
	require.ErrorIs(t, DeleteAttachmentsByRelease(ctx, 1), nativeoperation.ErrBusy)
	require.ErrorIs(t, DeleteOrphanedAttachments(ctx), nativeoperation.ErrBusy)
	_, err := DeleteOrphanedTopics(ctx)
	require.ErrorIs(t, err, nativeoperation.ErrBusy)
	_, err = FixNullArchivedRepository(ctx)
	require.ErrorIs(t, err, nativeoperation.ErrBusy)

	_, err = DeleteAttachments(ctx, []*Attachment{{ID: 1}}, false)
	require.ErrorIs(t, err, nativeoperation.ErrBusy)
}

func TestAttachmentDownloadCounterProceedsWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	claimTestOwner(t)

	attach := unittest.AssertExistsAndLoadBean(t, &Attachment{ID: 1})
	require.NoError(t, attach.IncreaseDownloadCount(ctx))
}
