// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"encoding/json"
	"testing"
	"time"

	"forgejo.org/models/nativeoperation"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/require"
)

// The envelopes below are the exact bytes emitted by the Soda L07
// production consumer path (helpers + executor payload builders),
// captured in .coord/l07-consumer/captured-work.json (sha256
// 15311d8e10298d9ffd3ecf567536afca73dbd81a7bb538958a165d9596893807).
// Only the time-bound native revision and deadline come from the live
// fixture reservation; every other field is byte-exact Soda output.
var lifecycleConsumerJoinCases = []struct {
	name         string
	operationID  string
	actorID      int64
	repositoryID int64
	kind         string
	authRevision string
	payload      string
}{
	{
		name:         "issue_created",
		operationID:  "lifecycle:issue_created:rev-1:task-new:attempt-1",
		actorID:      2,
		repositoryID: 3,
		kind:         nativeoperation.KindLifecycleIssue,
		authRevision: "rev-1:digest",
		payload:      `{"issue_id":6,"label_id":3,"assignee_id":4}`,
	},
	{
		name:         "pr_created",
		operationID:  "lifecycle:pr_created:pub-1:pr-12-34:attempt-1",
		actorID:      2,
		repositoryID: 1,
		kind:         nativeoperation.KindLifecycleReview,
		authRevision: "pubauth-1",
		payload:      `{"issue_id":2,"reviewer_ids":[2]}`,
	},
	{
		name:         "verified_merge",
		operationID:  "lifecycle:verified_merge:merge-1:merge-0123456789abcdef0123456789abcdef01234567-99:attempt-1",
		actorID:      2,
		repositoryID: 1,
		kind:         nativeoperation.KindLifecycleNote,
		authRevision: "mergeauth-1",
		payload:      `{"issue_id":5,"content":"Factory completed the accepted work.","merge_ref":"0123456789abcdef0123456789abcdef01234567"}`,
	},
}

// TestLifecycleConsumerJoin proves the Soda consumer's exact emitted
// envelopes commit through the real native writer with exact receipts.
func TestLifecycleConsumerJoin(t *testing.T) {
	unittest.PrepareTestEnv(t)
	useIsolatedAppData(t)
	ctx := t.Context()
	svc := NewService()

	issueDecision := lifecycleIssueDecision(t, 3, nativeoperation.KindLifecycleIssue)
	reviewDecision := lifecycleReviewDecision(t)
	noteDecision := lifecycleNoteDecision(t)

	for _, tc := range lifecycleConsumerJoinCases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := nativeoperation.ReadReservation(ctx)
			require.NoError(t, err)
			now := time.Now().Unix()
			intent, err := ValidateIntent(tc.operationID, tc.actorID, tc.repositoryID, tc.kind, tc.authRevision, r.Revision, now+300, []byte(tc.payload), now)
			require.NoError(t, err)
			decision := issueDecision
			switch tc.kind {
			case nativeoperation.KindLifecycleReview:
				decision = reviewDecision
			case nativeoperation.KindLifecycleNote:
				decision = noteDecision
			}
			record, err := svc.Submit(ctx, decision, operationTestInstallation, intent)
			require.NoError(t, err)
			require.Equal(t, nativeoperation.EffectCommitted, record.EffectState, record.ReasonCode)
			require.Equal(t, tc.operationID, record.OperationID)
			switch tc.kind {
			case nativeoperation.KindLifecycleIssue:
				var receipt LifecycleIssueReceipt
				require.NoError(t, json.Unmarshal(record.Receipt, &receipt))
				require.Equal(t, int64(6), receipt.IssueID)
				require.True(t, receipt.LabelAdded)
				require.True(t, receipt.AssigneeAdded)
			case nativeoperation.KindLifecycleReview:
				var receipt LifecycleReviewReceipt
				require.NoError(t, json.Unmarshal(record.Receipt, &receipt))
				require.Equal(t, int64(2), receipt.IssueID)
			case nativeoperation.KindLifecycleNote:
				var receipt LifecycleNoteReceipt
				require.NoError(t, json.Unmarshal(record.Receipt, &receipt))
				require.Equal(t, int64(5), receipt.IssueID)
				require.NotZero(t, receipt.CommentID)
			}
		})
	}
}
