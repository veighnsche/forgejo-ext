// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"encoding/json"
	"testing"
	"time"

	model "forgejo.org/models/nativeoperation"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/require"
)

const (
	testHead = "1111111111111111111111111111111111111111"
	testBase = "2222222222222222222222222222222222222222"
)

func mergePayload(t *testing.T, mutate func(*map[string]any)) []byte {
	t.Helper()
	payload := map[string]any{
		"pull_request_number": 3,
		"head_repository_id":  1,
		"head_ref":            "refs/heads/branch2",
		"base_ref":            "refs/heads/master",
		"expected_head_oid":   testHead,
		"expected_base_oid":   testBase,
		"method":              "fast-forward-only",
	}
	if mutate != nil {
		mutate(&payload)
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	return raw
}

func TestValidateMergeIntent(t *testing.T) {
	now := time.Now().Unix()
	intent, err := ValidateIntent("op-1", 2, 1, model.KindMerge, "rev-1", 7, now+300, mergePayload(t, nil), now)
	require.NoError(t, err)
	require.Equal(t, "op-1", intent.OperationID)
	require.NotEmpty(t, intent.Digest)
	require.NotEmpty(t, intent.Canonical)
	require.Equal(t, int64(3), intent.Merge.PullRequestNumber)

	cases := map[string]func(*map[string]any){
		"other method refuses without fallback": func(p *map[string]any) { (*p)["method"] = "merge" },
		"fork source refuses":                   func(p *map[string]any) { (*p)["head_repository_id"] = 2 },
		"abbreviated OID refuses":               func(p *map[string]any) { (*p)["expected_head_oid"] = "abc123" },
		"no-op refuses":                         func(p *map[string]any) { (*p)["expected_base_oid"] = testHead },
		"short ref refuses":                     func(p *map[string]any) { (*p)["head_ref"] = "branch2" },
		"same refs refuse":                      func(p *map[string]any) { (*p)["head_ref"] = "refs/heads/master" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ValidateIntent("op-1", 2, 1, model.KindMerge, "rev-1", 7, now+300, mergePayload(t, mutate), now)
			require.ErrorIs(t, err, ErrInvalidIntent)
		})
	}

	_, err = ValidateIntent("op-1", 2, 1, model.KindMerge, "rev-1", 7, now+300, []byte(`{"pull_request_number":3,"extra":1}`), now)
	require.ErrorIs(t, err, ErrInvalidIntent)

	t.Run("common field bounds", func(t *testing.T) {
		payload := mergePayload(t, nil)
		_, err := ValidateIntent("", 2, 1, model.KindMerge, "rev-1", 7, now+300, payload, now)
		require.ErrorIs(t, err, ErrInvalidIntent)
		_, err = ValidateIntent("op-1", 2, 1, model.KindMerge, "", 7, now+300, payload, now)
		require.ErrorIs(t, err, ErrInvalidIntent)
		_, err = ValidateIntent("op-1", 2, 1, model.KindMerge, "rev-1", 0, now+300, payload, now)
		require.ErrorIs(t, err, ErrInvalidIntent)
		_, err = ValidateIntent("op-1", 2, 1, model.KindMerge, "rev-1", 7, now-1, payload, now)
		require.ErrorIs(t, err, ErrInvalidIntent)
		_, err = ValidateIntent("op-1", 2, 1, model.KindMerge, "rev-1", 7, now+3700, payload, now)
		require.ErrorIs(t, err, ErrInvalidIntent)
		_, err = ValidateIntent("op-1", 2, 1, "pull_request.delete", "rev-1", 7, now+300, payload, now)
		require.ErrorIs(t, err, ErrInvalidIntent)
	})
}

func TestIntentDigestBindsEverySemanticField(t *testing.T) {
	now := time.Now().Unix()
	base, err := ValidateIntent("op-1", 2, 1, model.KindMerge, "rev-1", 7, now+300, mergePayload(t, nil), now)
	require.NoError(t, err)

	again, err := ValidateIntent("op-1", 2, 1, model.KindMerge, "rev-1", 7, now+300, mergePayload(t, nil), now)
	require.NoError(t, err)
	require.Equal(t, base.Digest, again.Digest)

	changed, err := ValidateIntent("op-1", 2, 1, model.KindMerge, "rev-2", 7, now+300, mergePayload(t, nil), now)
	require.NoError(t, err)
	require.NotEqual(t, base.Digest, changed.Digest)

	later, err := ValidateIntent("op-1", 2, 1, model.KindMerge, "rev-1", 7, now+301, mergePayload(t, nil), now)
	require.NoError(t, err)
	require.NotEqual(t, base.Digest, later.Digest)

	head, err := ValidateIntent("op-1", 2, 1, model.KindMerge, "rev-1", 7, now+300, mergePayload(t, func(p *map[string]any) {
		(*p)["expected_head_oid"] = "3333333333333333333333333333333333333333"
	}), now)
	require.NoError(t, err)
	require.NotEqual(t, base.Digest, head.Digest)
}

func TestReadNativeRevision(t *testing.T) {
	unittest.PrepareTestEnv(t)
	svc := NewService()
	observation, err := svc.ReadNativeRevision(t.Context())
	require.NoError(t, err)
	require.GreaterOrEqual(t, observation.Revision, int64(1))
}
