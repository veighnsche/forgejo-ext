// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package forgefed

import (
	"testing"

	ap "github.com/go-ap/activitypub"
	"github.com/go-ap/jsonld"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_ForgePushMarshalJSON(t *testing.T) {
	push := NewForgePush(
		ap.IRI("https://example.com/api/v1/activitypub/repository-id/1/outbox/push/main/abc123"),
		ap.IRI("https://example.com/api/v1/activitypub/repository-id/1"),
	)
	push.AttributedTo = ap.IRI("https://example.com/api/v1/activitypub/user-id/7")
	push.To = ap.ItemCollection{ap.IRI("https://example.com/api/v1/activitypub/repository-id/1/followers")}
	push.Context = ap.IRI("https://example.com/api/v1/activitypub/repository-id/1")
	push.Target = ap.IRI("https://example.com/api/v1/activitypub/repository-id/1/branches/main")
	push.HashBefore = "0000000000000000000000000000000000000000"
	push.HashAfter = "abc123def456abc123def456abc123def456abc123"

	collection := ap.OrderedCollectionNew(ap.IRI("https://example.com/api/v1/activitypub/repository-id/1/outbox"))
	collection.TotalItems = 1
	collection.OrderedItems = ap.ItemCollection{
		NewForgeCommit(
			ap.IRI("https://example.com/api/v1/activitypub/repository-id/1/commits/abc123def456abc123def456abc123def456abc123"),
			"abc123def456abc123def456abc123def456abc123",
			"Add widget",
		),
	}
	push.Object = collection

	data, err := jsonld.WithContext(jsonld.IRI(ap.ActivityBaseURI)).Marshal(push)
	require.NoError(t, err)

	raw := string(data)
	t.Log(raw)
	for _, want := range []string{
		`"type":"Push"`,
		`"actor":"https://example.com/api/v1/activitypub/repository-id/1"`,
		`"attributedTo":"https://example.com/api/v1/activitypub/user-id/7"`,
		`"target":"https://example.com/api/v1/activitypub/repository-id/1/branches/main"`,
		`"hashBefore":"0000000000000000000000000000000000000000"`,
		`"hashAfter":"abc123def456abc123def456abc123def456abc123"`,
		`"type":"OrderedCollection"`,
		`"totalItems":1`,
		`"type":"Commit"`,
		`"hash":"abc123def456abc123def456abc123def456abc123"`,
		`"summary":"Add widget"`,
	} {
		assert.Contains(t, raw, want, "missing %s in %s", want, raw)
	}
}

func Test_ForgePushUnmarshalJSON(t *testing.T) {
	raw := `{"type":"Push",` +
		`"actor":"https://example.com/api/v1/activitypub/repository-id/1",` +
		`"hashBefore":"aaaa","hashAfter":"bbbb",` +
		`"object":{"type":"OrderedCollection","totalItems":1,"orderedItems":[` +
		`{"type":"Commit","hash":"bbbb","summary":"Fix it"}]}}`

	var push ForgePush
	require.NoError(t, push.UnmarshalJSON([]byte(raw)))
	assert.Equal(t, PushType, push.Type)
	assert.Equal(t, "aaaa", push.HashBefore)
	assert.Equal(t, "bbbb", push.HashAfter)

	col, ok := push.Object.(*ap.OrderedCollection)
	require.True(t, ok, "object should be an OrderedCollection, got %T", push.Object)
	require.GreaterOrEqual(t, int(col.TotalItems), 1, "collection should report the pushed commits")
}
