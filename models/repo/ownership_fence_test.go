// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"testing"

	nativeoperation_model "forgejo.org/models/nativeoperation"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/require"
)

func claimTestOwner(t *testing.T) string {
	t.Helper()
	claimed, err := nativeoperation_model.ClaimOrdinary(t.Context(), "ord:repository/test/abc123", `{"kind":"ordinary","family":"repository"}`, "v")
	require.NoError(t, err)
	t.Cleanup(func() { _ = nativeoperation_model.ReleaseOwner(t.Context(), claimed.Owner) })
	return claimed.Owner
}

func TestRepoWritersFencesWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	claimTestOwner(t)

	require.ErrorIs(t, UpdateRepoSize(ctx, 1, 100, 10), nativeoperation_model.ErrBusy)
	require.ErrorIs(t, UpdateRepoUnit(ctx, &RepoUnit{ID: 1}), nativeoperation_model.ErrBusy)
	require.ErrorIs(t, UpdateAttachment(ctx, &Attachment{ID: 1}), nativeoperation_model.ErrBusy)
	require.ErrorIs(t, UpdateAttachmentByUUID(ctx, &Attachment{UUID: "nope"}), nativeoperation_model.ErrBusy)
	require.ErrorIs(t, DeleteAttachmentsByRelease(ctx, 1), nativeoperation_model.ErrBusy)
	require.ErrorIs(t, DeleteOrphanedAttachments(ctx), nativeoperation_model.ErrBusy)
	_, err := DeleteOrphanedTopics(ctx)
	require.ErrorIs(t, err, nativeoperation_model.ErrBusy)
	_, err = FixNullArchivedRepository(ctx)
	require.ErrorIs(t, err, nativeoperation_model.ErrBusy)

	_, err = DeleteAttachments(ctx, []*Attachment{{ID: 1}}, false)
	require.ErrorIs(t, err, nativeoperation_model.ErrBusy)
}

func TestAttachmentDownloadCounterProceedsWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	claimTestOwner(t)

	attach := unittest.AssertExistsAndLoadBean(t, &Attachment{ID: 1})
	require.NoError(t, attach.IncreaseDownloadCount(ctx))
}
