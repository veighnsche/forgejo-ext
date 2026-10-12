// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

// I07 closed-history join proof (lane L assist). The embedded packet and
// delivered-issue contents are exact bytes captured from Soda main e608a916
// (I ea3298a6): control acceptedGenerationFixture revisions 1-2,
// g.Payload verbatim plus fake issue states. Provenance:
// .coord/i07-captured-packets.json
// SHA256: 9ed37838ac45d31815a89981ca51c9089c6476b66b7b909769952a63d8e049c1
// I owns packet/case authority (see L-I07-CAPTURE); Q owns acceptance.
// Fixture proof only: this does not establish installed/VM behavior.

import (
	"encoding/json"
	"testing"

	sdk "forgejo.org/extension-sdk"
	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	model "forgejo.org/models/nativeoperation"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/require"
)

const i07JoinPacket = `{"creates": [{"key": "dbbbbbbbbbbbbbbbbbbbbbbbb", "body": "Accepted product objective:\nBuild a request tracker with exports\n\nTask objective:\nRequest list with filters\n\nAcceptance outcomes:\n- Show requests\n\nAccepted product choice (requested, human) (d333333333333333333333333):\nTrack requests\nConsequence: Clear ownership\n\nAccepted product intent: d222222222222222222222222\nAccepted revision: d777777777777777777777777\nIntent digest: ce56549aa8308a03d2e16fc75e308c32d1915c93ba5ccc99654a6afc4bea8eac\nTask: dbbbbbbbbbbbbbbbbbbbbbbbb\nTask digest: e98a60f9b74014d1b971c36458e6b368d2372a1dd4b4d1f8bfef9d4a0842220c\nProduct choices: d333333333333333333333333\n", "title": "Request list with filters"}, {"key": "ddddddddddddddddddddddddd", "body": "Accepted product objective:\nBuild a request tracker with exports\n\nTask objective:\nRequest export\n\nAcceptance outcomes:\n- Export requests\n\nAccepted product choice (requested, human) (d333333333333333333333333):\nTrack requests\nConsequence: Clear ownership\n\nAccepted product intent: d222222222222222222222222\nAccepted revision: d777777777777777777777777\nIntent digest: ce56549aa8308a03d2e16fc75e308c32d1915c93ba5ccc99654a6afc4bea8eac\nTask: ddddddddddddddddddddddddd\nTask digest: 2c921be3274cd05b428ac5952914dacec66e06e6601d5cc28e843cf38a60e9f6\nProduct choices: d333333333333333333333333\n", "title": "Request export"}], "dependencies": [{"issue": {"key": "dbbbbbbbbbbbbbbbbbbbbbbbb"}, "prerequisite": {"issue_id": 101}}, {"issue": {"key": "ddddddddddddddddddddddddd"}, "prerequisite": {"issue_id": 101}}, {"issue": {"issue_id": 102}, "remove": true, "prerequisite": {"issue_id": 101}}]}`

const i07Closed101Title = `Request entry`

const i07Closed101Body = `Accepted product objective:
Build a request tracker

Task objective:
Request entry

Acceptance outcomes:
- Capture requests

Accepted product choice (requested, human) (d333333333333333333333333):
Track requests
Consequence: Clear ownership

Accepted product intent: d222222222222222222222222
Accepted revision: d666666666666666666666666
Intent digest: 35be93f13d7a96f946ee68fb2eafeaa2f81fcbedb258f31e9a62750520268b2d
Task: daaaaaaaaaaaaaaaaaaaaaaaa
Task digest: ea24e935afc278e23cf9af983113460266d196db661d76ad5185cfa09f9d01c4
Product choices: d333333333333333333333333
`

const i07Closed102Title = `Request list`

const i07Closed102Body = `Accepted product objective:
Build a request tracker

Task objective:
Request list

Acceptance outcomes:
- Show requests

Accepted product choice (requested, human) (d333333333333333333333333):
Track requests
Consequence: Clear ownership

Accepted product intent: d222222222222222222222222
Accepted revision: d666666666666666666666666
Intent digest: 35be93f13d7a96f946ee68fb2eafeaa2f81fcbedb258f31e9a62750520268b2d
Task: dbbbbbbbbbbbbbbbbbbbbbbbb
Task digest: c009be6a27d9e40e05b40185a69b5764fe66392996d48f9c665fd7271c17de0e
Product choices: d333333333333333333333333
`

const i07Closed103Title = `Request search`

const i07Closed103Body = `Accepted product objective:
Build a request tracker

Task objective:
Request search

Acceptance outcomes:
- Find requests

Accepted product choice (requested, human) (d333333333333333333333333):
Track requests
Consequence: Clear ownership

Accepted product intent: d222222222222222222222222
Accepted revision: d666666666666666666666666
Intent digest: 35be93f13d7a96f946ee68fb2eafeaa2f81fcbedb258f31e9a62750520268b2d
Task: dcccccccccccccccccccccccc
Task digest: f0467b7af1839bc0154a38b6933509f1e2c0098cef43f0d72861ab0e7404cd0c
Product choices: d333333333333333333333333
`

func insertI07ClosedIssue(t *testing.T, id int64, title, body string) {
	t.Helper()
	_, err := db.GetEngine(t.Context()).Insert(&issues_model.Issue{ID: id, RepoID: 1, Index: id, PosterID: 2, Title: title, Content: body, ContentVersion: 0, IsClosed: true})
	require.NoError(t, err)
}

func TestIssueGraphClosedEditRefusedAtomically(t *testing.T) {
	unittest.PrepareTestEnv(t)
	useIsolatedAppData(t)
	ctx := t.Context()
	decision := graphDecision(t)
	svc := NewService()
	insertI07ClosedIssue(t, 101, i07Closed101Title, i07Closed101Body)
	open, err := issues_model.GetIssueByID(ctx, 1)
	require.NoError(t, err)
	require.False(t, open.IsClosed)
	var before int64
	_, err = db.GetEngine(ctx).SQL(`SELECT COUNT(*) FROM issue WHERE repo_id=1`).Get(&before)
	require.NoError(t, err)

	closed := sdk.IssueGraphPayload{
		Edits:   []sdk.IssueGraphEdit{{IssueID: 101, ExpectedTitle: i07Closed101Title, ExpectedBody: i07Closed101Body, ExpectedContentVersion: 0, Title: "mutated", Body: "mutated"}},
		Creates: []sdk.IssueGraphCreate{{Key: "must-not-exist", Title: "Must Not Exist", Body: "Must not exist"}},
	}
	refused, err := svc.Submit(ctx, decision, operationTestInstallation, graphIntent(t, "graph-closed-1", closed))
	require.NoError(t, err)
	require.Equal(t, model.EffectNotCommitted, refused.EffectState)
	require.Equal(t, model.ReasonNativeRefused, refused.ReasonCode)

	mixed := sdk.IssueGraphPayload{
		Edits: []sdk.IssueGraphEdit{
			{IssueID: 1, ExpectedTitle: open.Title, ExpectedBody: open.Content, ExpectedContentVersion: open.ContentVersion, Title: "open revised", Body: "open revised"},
			{IssueID: 101, ExpectedTitle: i07Closed101Title, ExpectedBody: i07Closed101Body, ExpectedContentVersion: 0, Title: "mutated", Body: "mutated"},
		},
	}
	mixedRefused, err := svc.Submit(ctx, decision, operationTestInstallation, graphIntent(t, "graph-closed-2", mixed))
	require.NoError(t, err)
	require.Equal(t, model.EffectNotCommitted, mixedRefused.EffectState)
	require.Equal(t, model.ReasonNativeRefused, mixedRefused.ReasonCode)

	var after int64
	_, err = db.GetEngine(ctx).SQL(`SELECT COUNT(*) FROM issue WHERE repo_id=1`).Get(&after)
	require.NoError(t, err)
	require.Equal(t, before, after)
	kept, err := issues_model.GetIssueByID(ctx, 101)
	require.NoError(t, err)
	require.Equal(t, i07Closed101Title, kept.Title)
	require.Equal(t, i07Closed101Body, kept.Content)
	require.Equal(t, 0, kept.ContentVersion)
	require.True(t, kept.IsClosed)
	untouched, err := issues_model.GetIssueByID(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, open.Title, untouched.Title)
	require.Equal(t, open.Content, untouched.Content)
	require.Equal(t, open.ContentVersion, untouched.ContentVersion)
}

func TestIssueGraphSodaDeltaJoinReceipts(t *testing.T) {
	unittest.PrepareTestEnv(t)
	useIsolatedAppData(t)
	ctx := t.Context()
	decision := graphDecision(t)
	svc := NewService()
	var packet sdk.IssueGraphPayload
	require.NoError(t, json.Unmarshal([]byte(i07JoinPacket), &packet))
	require.Empty(t, packet.Edits)
	roundTrip, err := json.Marshal(packet)
	require.NoError(t, err)
	var a, b any
	require.NoError(t, json.Unmarshal([]byte(i07JoinPacket), &a))
	require.NoError(t, json.Unmarshal(roundTrip, &b))
	aa, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	require.Equal(t, string(aa), string(bb))

	insertI07ClosedIssue(t, 101, i07Closed101Title, i07Closed101Body)
	insertI07ClosedIssue(t, 102, i07Closed102Title, i07Closed102Body)
	insertI07ClosedIssue(t, 103, i07Closed103Title, i07Closed103Body)
	_, err = db.GetEngine(ctx).Insert(&issues_model.IssueDependency{UserID: 2, IssueID: 102, DependencyID: 101})
	require.NoError(t, err)

	record, err := svc.Submit(ctx, decision, operationTestInstallation, graphIntent(t, "graph-i07-join", packet))
	require.NoError(t, err)
	require.Equal(t, model.EffectCommitted, record.EffectState, record.ReasonCode)
	var receipt sdk.IssueGraphReceipt
	require.NoError(t, json.Unmarshal(record.Receipt, &receipt))
	require.Len(t, receipt.Issues, 2)
	gotKeys := map[string]int64{}
	for _, item := range receipt.Issues {
		require.NotEmpty(t, item.Key)
		require.NotContains(t, []int64{101, 102, 103}, item.IssueID)
		gotKeys[item.Key] = item.IssueID
	}
	require.Contains(t, gotKeys, "dbbbbbbbbbbbbbbbbbbbbbbbb")
	require.Contains(t, gotKeys, "ddddddddddddddddddddddddd")
	require.Len(t, receipt.Dependencies, 3)

	for id, want := range map[int64][2]string{101: {i07Closed101Title, i07Closed101Body}, 102: {i07Closed102Title, i07Closed102Body}, 103: {i07Closed103Title, i07Closed103Body}} {
		kept, err := issues_model.GetIssueByID(ctx, id)
		require.NoError(t, err)
		require.Equal(t, want[0], kept.Title)
		require.Equal(t, want[1], kept.Content)
		require.Equal(t, 0, kept.ContentVersion)
		require.True(t, kept.IsClosed)
	}
	for _, key := range []string{"dbbbbbbbbbbbbbbbbbbbbbbbb", "ddddddddddddddddddddddddd"} {
		edges, err := issues_model.ListIssueDependencies(ctx, gotKeys[key])
		require.NoError(t, err)
		require.Len(t, edges, 1)
		require.Equal(t, int64(101), edges[0].DependencyID)
	}
	stale, err := issues_model.ListIssueDependencies(ctx, 102)
	require.NoError(t, err)
	require.Empty(t, stale)
}
