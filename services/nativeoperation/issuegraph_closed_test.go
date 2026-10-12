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
	"crypto/sha256"
	"encoding/hex"
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

// I07 golden-history join (I-7c3415d9, integrated 4658fbd1). Golden packet
// bytes embedded verbatim; digests pin delivered title/body. Provenance:
// internal/factory/control/testdata/closed-history/ on Soda main.
// r1-fresh.packet.json sha256 44fbe1da174c247f21b225c64df5ccf30d4b0d5a2755c216a38954619bbbcfa4
// r2-delta.packet.json sha256 b0a7a3f02f52c9d2681ef1a6ab9dfb339bb8d79796b9113e77ca8bda1c9eb0e5
// r3-redelta.packet.json sha256 92a06cf9c6dde7ad736dbbd6096b4def5673b8326c535f7e3124ea674436f730
// Fixture IDs are local per I manifest: repo 42/actor 91 map to fixture
// repo 1/actor 2 (owner); ID-bearing entries translate through the test's
// own receipts. Assertions compare receipt SHAPE plus digest-pinned
// delivered histories, never fixture IDs.

const i07GoldenR1 = `{
  "creates": [
    {
      "key": "daaaaaaaaaaaaaaaaaaaaaaaa",
      "title": "Request entry",
      "body": "Accepted product objective:\nBuild a request tracker\n\nTask objective:\nRequest entry\n\nAcceptance outcomes:\n- Capture requests\n\nAccepted product choice (requested, human) (d333333333333333333333333):\nTrack requests\nConsequence: Clear ownership\n\nAccepted product intent: d222222222222222222222222\nAccepted revision: d666666666666666666666666\nIntent digest: 35be93f13d7a96f946ee68fb2eafeaa2f81fcbedb258f31e9a62750520268b2d\nTask: daaaaaaaaaaaaaaaaaaaaaaaa\nTask digest: ea24e935afc278e23cf9af983113460266d196db661d76ad5185cfa09f9d01c4\nProduct choices: d333333333333333333333333\n"
    },
    {
      "key": "dbbbbbbbbbbbbbbbbbbbbbbbb",
      "title": "Request list",
      "body": "Accepted product objective:\nBuild a request tracker\n\nTask objective:\nRequest list\n\nAcceptance outcomes:\n- Show requests\n\nAccepted product choice (requested, human) (d333333333333333333333333):\nTrack requests\nConsequence: Clear ownership\n\nAccepted product intent: d222222222222222222222222\nAccepted revision: d666666666666666666666666\nIntent digest: 35be93f13d7a96f946ee68fb2eafeaa2f81fcbedb258f31e9a62750520268b2d\nTask: dbbbbbbbbbbbbbbbbbbbbbbbb\nTask digest: c009be6a27d9e40e05b40185a69b5764fe66392996d48f9c665fd7271c17de0e\nProduct choices: d333333333333333333333333\n"
    },
    {
      "key": "dcccccccccccccccccccccccc",
      "title": "Request search",
      "body": "Accepted product objective:\nBuild a request tracker\n\nTask objective:\nRequest search\n\nAcceptance outcomes:\n- Find requests\n\nAccepted product choice (requested, human) (d333333333333333333333333):\nTrack requests\nConsequence: Clear ownership\n\nAccepted product intent: d222222222222222222222222\nAccepted revision: d666666666666666666666666\nIntent digest: 35be93f13d7a96f946ee68fb2eafeaa2f81fcbedb258f31e9a62750520268b2d\nTask: dcccccccccccccccccccccccc\nTask digest: f0467b7af1839bc0154a38b6933509f1e2c0098cef43f0d72861ab0e7404cd0c\nProduct choices: d333333333333333333333333\n"
    }
  ],
  "dependencies": [
    {
      "issue": {
        "key": "dbbbbbbbbbbbbbbbbbbbbbbbb"
      },
      "prerequisite": {
        "key": "daaaaaaaaaaaaaaaaaaaaaaaa"
      }
    }
  ]
}
`

const i07GoldenR2 = `{
  "creates": [
    {
      "key": "dbbbbbbbbbbbbbbbbbbbbbbbb",
      "title": "Request list with filters",
      "body": "Accepted product objective:\nBuild a request tracker with exports\n\nTask objective:\nRequest list with filters\n\nAcceptance outcomes:\n- Show requests\n\nAccepted product choice (requested, human) (d333333333333333333333333):\nTrack requests\nConsequence: Clear ownership\n\nAccepted product intent: d222222222222222222222222\nAccepted revision: d777777777777777777777777\nIntent digest: ce56549aa8308a03d2e16fc75e308c32d1915c93ba5ccc99654a6afc4bea8eac\nTask: dbbbbbbbbbbbbbbbbbbbbbbbb\nTask digest: e98a60f9b74014d1b971c36458e6b368d2372a1dd4b4d1f8bfef9d4a0842220c\nProduct choices: d333333333333333333333333\n"
    },
    {
      "key": "ddddddddddddddddddddddddd",
      "title": "Request export",
      "body": "Accepted product objective:\nBuild a request tracker with exports\n\nTask objective:\nRequest export\n\nAcceptance outcomes:\n- Export requests\n\nAccepted product choice (requested, human) (d333333333333333333333333):\nTrack requests\nConsequence: Clear ownership\n\nAccepted product intent: d222222222222222222222222\nAccepted revision: d777777777777777777777777\nIntent digest: ce56549aa8308a03d2e16fc75e308c32d1915c93ba5ccc99654a6afc4bea8eac\nTask: ddddddddddddddddddddddddd\nTask digest: 2c921be3274cd05b428ac5952914dacec66e06e6601d5cc28e843cf38a60e9f6\nProduct choices: d333333333333333333333333\n"
    }
  ],
  "dependencies": [
    {
      "issue": {
        "key": "dbbbbbbbbbbbbbbbbbbbbbbbb"
      },
      "prerequisite": {
        "issue_id": 101
      }
    },
    {
      "issue": {
        "key": "ddddddddddddddddddddddddd"
      },
      "prerequisite": {
        "issue_id": 101
      }
    },
    {
      "issue": {
        "issue_id": 102
      },
      "prerequisite": {
        "issue_id": 101
      },
      "remove": true
    }
  ]
}
`

const i07GoldenR3 = `{
  "creates": [
    {
      "key": "daaaaaaaaaaaaaaaaaaaaaaaa",
      "title": "Request entry with validation",
      "body": "Accepted product objective:\nBuild a request tracker with exports and audit\n\nTask objective:\nRequest entry with validation\n\nAcceptance outcomes:\n- Capture requests\n\nAccepted product choice (requested, human) (d333333333333333333333333):\nTrack requests\nConsequence: Clear ownership\n\nAccepted product intent: d222222222222222222222222\nAccepted revision: d888888888888888888888888\nIntent digest: 1d2f7b9c1e19983b675bde829757c8333e1d0da3ce721b1ba83631e886f3be5a\nTask: daaaaaaaaaaaaaaaaaaaaaaaa\nTask digest: c1e881ccff77dc951b4df8de37a1baf5918193d5dda03a5d62f86dae02ea5580\nProduct choices: d333333333333333333333333\n"
    }
  ],
  "dependencies": [
    {
      "issue": {
        "issue_id": 104
      },
      "prerequisite": {
        "key": "daaaaaaaaaaaaaaaaaaaaaaaa"
      }
    },
    {
      "issue": {
        "issue_id": 105
      },
      "prerequisite": {
        "key": "daaaaaaaaaaaaaaaaaaaaaaaa"
      }
    },
    {
      "issue": {
        "issue_id": 104
      },
      "prerequisite": {
        "issue_id": 101
      },
      "remove": true
    },
    {
      "issue": {
        "issue_id": 105
      },
      "prerequisite": {
        "issue_id": 101
      },
      "remove": true
    }
  ]
}
`

type i07GoldenEntry struct {
	titleDigest, contentDigest string
	version                    int
}

var i07PreR2 = map[int64]i07GoldenEntry{101: {"4635e50c0bacfcecbe8c1f8261ddb1f996805e628b5bc5a40ad55ea78a2c06cf", "79ee0fc847b365cc40bacb59415be8e970b05b9398dc85b013889d1548ec8102", 0}, 102: {"a4e224e002de1842472dbb99715fd3d2ed9f5b158c33a674f4902de0c1ab6235", "f869e408c48d5e8a20c70cb7c46bb5745e25e631a8d7da6110bbb12ba1114aba", 0}, 103: {"f704dc85ed5495b697c0e6078a9877877f241b34c2d7265bd2e492db3322d58d", "615b4afb19b518feab09683b615ea6fe87aa8b151794cd08d3a43e8862a6663c", 0}}

var i07PreR3 = map[int64]i07GoldenEntry{101: {"4635e50c0bacfcecbe8c1f8261ddb1f996805e628b5bc5a40ad55ea78a2c06cf", "79ee0fc847b365cc40bacb59415be8e970b05b9398dc85b013889d1548ec8102", 0}, 102: {"a4e224e002de1842472dbb99715fd3d2ed9f5b158c33a674f4902de0c1ab6235", "f869e408c48d5e8a20c70cb7c46bb5745e25e631a8d7da6110bbb12ba1114aba", 0}, 103: {"f704dc85ed5495b697c0e6078a9877877f241b34c2d7265bd2e492db3322d58d", "615b4afb19b518feab09683b615ea6fe87aa8b151794cd08d3a43e8862a6663c", 0}, 104: {"fc99a8bd75f1640a9ae313cce806273b0f71af3dbe4f20a653f542260e80cf69", "f76749cdb7203724c7b9325bd5b5b80b27ea97df6e8861aed3f8d124b174d419", 0}, 105: {"c2b7ea6e50679e346fc6a7205f5c45e67b58e82a17af889e8080600c6820b940", "ce30737b7d7dc939dbdc23a862aaf1cec4823ef2fd45aa923cea6ecdea8ff3bc", 0}}

var i07GoldenKeyToFixture = map[string]int64{
	"daaaaaaaaaaaaaaaaaaaaaaaa": 101,
	"dbbbbbbbbbbbbbbbbbbbbbbbb": 102,
	"dcccccccccccccccccccccccc": 103,
}

func i07DigestHex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func i07CloseIssue(t *testing.T, id int64) {
	t.Helper()
	_, err := db.GetEngine(t.Context()).Exec(`UPDATE issue SET is_closed=? WHERE id=?`, true, id)
	require.NoError(t, err)
}

func i07PinDelivered(t *testing.T, nativeID int64, want i07GoldenEntry) {
	t.Helper()
	kept, err := issues_model.GetIssueByID(t.Context(), nativeID)
	require.NoError(t, err)
	require.Equal(t, want.titleDigest, i07DigestHex(kept.Title))
	require.Equal(t, want.contentDigest, i07DigestHex(kept.Content))
	require.Equal(t, want.version, kept.ContentVersion)
	require.True(t, kept.IsClosed)
}

func i07TranslateIDs(t *testing.T, raw string, mapping map[int64]int64) sdk.IssueGraphPayload {
	t.Helper()
	var packet sdk.IssueGraphPayload
	require.NoError(t, json.Unmarshal([]byte(raw), &packet))
	translate := func(id int64) int64 {
		mapped, ok := mapping[id]
		require.True(t, ok, "unmapped golden id %d", id)
		return mapped
	}
	for i := range packet.Dependencies {
		if packet.Dependencies[i].Issue.IssueID != 0 {
			packet.Dependencies[i].Issue.IssueID = translate(packet.Dependencies[i].Issue.IssueID)
		}
		if packet.Dependencies[i].Prerequisite.IssueID != 0 {
			packet.Dependencies[i].Prerequisite.IssueID = translate(packet.Dependencies[i].Prerequisite.IssueID)
		}
	}
	return packet
}

func i07ReceiptKeys(t *testing.T, record sdk.OperationRecord) map[string]int64 {
	t.Helper()
	var receipt sdk.IssueGraphReceipt
	require.NoError(t, json.Unmarshal(record.Receipt, &receipt))
	out := map[string]int64{}
	for _, item := range receipt.Issues {
		out[item.Key] = item.IssueID
	}
	return out
}

func TestIssueGraphGoldenHistoryJoin(t *testing.T) {
	unittest.PrepareTestEnv(t)
	useIsolatedAppData(t)
	ctx := t.Context()
	decision := graphDecision(t)
	svc := NewService()
	const keyA, keyB, keyC, keyD = "daaaaaaaaaaaaaaaaaaaaaaaa", "dbbbbbbbbbbbbbbbbbbbbbbbb", "dcccccccccccccccccccccccc", "ddddddddddddddddddddddddd"

	var r1 sdk.IssueGraphPayload
	require.NoError(t, json.Unmarshal([]byte(i07GoldenR1), &r1))
	r1Record, err := svc.Submit(ctx, decision, operationTestInstallation, graphIntent(t, "intent-d666666666666666666666666", r1))
	require.NoError(t, err)
	require.Equal(t, model.EffectCommitted, r1Record.EffectState, r1Record.ReasonCode)
	r1IDs := i07ReceiptKeys(t, r1Record)
	require.Len(t, r1IDs, 3)
	for key, native := range r1IDs {
		created, err := issues_model.GetIssueByID(ctx, native)
		require.NoError(t, err)
		require.False(t, created.IsClosed)
		i07CloseIssue(t, native)
		i07PinDelivered(t, native, i07PreR2[i07GoldenKeyToFixture[key]])
	}
	edges, err := issues_model.ListIssueDependencies(ctx, r1IDs[keyB])
	require.NoError(t, err)
	require.Len(t, edges, 1)
	require.Equal(t, r1IDs[keyA], edges[0].DependencyID)

	r2 := i07TranslateIDs(t, i07GoldenR2, map[int64]int64{101: r1IDs[keyA], 102: r1IDs[keyB]})
	r2Intent := graphIntent(t, "intent-d777777777777777777777777", r2)
	r2Record, err := svc.Submit(ctx, decision, operationTestInstallation, r2Intent)
	require.NoError(t, err)
	require.Equal(t, model.EffectCommitted, r2Record.EffectState, r2Record.ReasonCode)
	r2IDs := i07ReceiptKeys(t, r2Record)
	require.Len(t, r2IDs, 2)
	require.Contains(t, r2IDs, keyB)
	require.Contains(t, r2IDs, keyD)
	for _, id := range r2IDs {
		require.NotContains(t, []int64{r1IDs[keyA], r1IDs[keyB], r1IDs[keyC]}, id)
	}
	for key, native := range r1IDs {
		i07PinDelivered(t, native, i07PreR2[i07GoldenKeyToFixture[key]])
	}
	for _, key := range []string{keyB, keyD} {
		pins, err := issues_model.ListIssueDependencies(ctx, r2IDs[key])
		require.NoError(t, err)
		require.Len(t, pins, 1)
		require.Equal(t, r1IDs[keyA], pins[0].DependencyID)
	}
	stale, err := issues_model.ListIssueDependencies(ctx, r1IDs[keyB])
	require.NoError(t, err)
	require.Empty(t, stale)

	var before int64
	_, err = db.GetEngine(ctx).SQL(`SELECT COUNT(*) FROM issue WHERE repo_id=1`).Get(&before)
	require.NoError(t, err)
	replay, err := svc.Submit(ctx, decision, operationTestInstallation, r2Intent)
	require.NoError(t, err)
	require.Equal(t, r2Record.Receipt, replay.Receipt)
	var after int64
	_, err = db.GetEngine(ctx).SQL(`SELECT COUNT(*) FROM issue WHERE repo_id=1`).Get(&after)
	require.NoError(t, err)
	require.Equal(t, before, after)

	for key, native := range r2IDs {
		i07CloseIssue(t, native)
		i07PinDelivered(t, native, i07PreR3[104+i07KeyOffset(key)])
	}
	r3 := i07TranslateIDs(t, i07GoldenR3, map[int64]int64{101: r1IDs[keyA], 104: r2IDs[keyB], 105: r2IDs[keyD]})
	r3Record, err := svc.Submit(ctx, decision, operationTestInstallation, graphIntent(t, "intent-d888888888888888888888888", r3))
	require.NoError(t, err)
	require.Equal(t, model.EffectCommitted, r3Record.EffectState, r3Record.ReasonCode)
	r3IDs := i07ReceiptKeys(t, r3Record)
	require.Len(t, r3IDs, 1)
	require.Contains(t, r3IDs, keyA)
	for key, native := range r1IDs {
		i07PinDelivered(t, native, i07PreR3[i07GoldenKeyToFixture[key]])
	}
	for key, native := range r2IDs {
		i07PinDelivered(t, native, i07PreR3[104+i07KeyOffset(key)])
	}
	for _, key := range []string{keyB, keyD} {
		pins, err := issues_model.ListIssueDependencies(ctx, r2IDs[key])
		require.NoError(t, err)
		require.Len(t, pins, 1)
		require.Equal(t, r3IDs[keyA], pins[0].DependencyID)
	}

	tampered := sdk.IssueGraphPayload{
		Edits:   []sdk.IssueGraphEdit{{IssueID: r1IDs[keyA], ExpectedTitle: "drifted", ExpectedBody: "drifted", ExpectedContentVersion: 0, Title: "x", Body: "x"}},
		Creates: []sdk.IssueGraphCreate{{Key: "tampered-must-not-exist", Title: "t", Body: "b"}},
	}
	tamperedRefused, err := svc.Submit(ctx, decision, operationTestInstallation, graphIntent(t, "intent-tampered-closed", tampered))
	require.NoError(t, err)
	require.Equal(t, model.EffectNotCommitted, tamperedRefused.EffectState)
	require.Equal(t, model.ReasonNativeRefused, tamperedRefused.ReasonCode)
	var final int64
	_, err = db.GetEngine(ctx).SQL(`SELECT COUNT(*) FROM issue WHERE repo_id=1`).Get(&final)
	require.NoError(t, err)
	require.Equal(t, after+1, final)
	i07PinDelivered(t, r1IDs[keyA], i07PreR3[101])
}

func i07KeyOffset(key string) int64 {
	if key == "dbbbbbbbbbbbbbbbbbbbbbbbb" {
		return 0
	}
	return 1
}
