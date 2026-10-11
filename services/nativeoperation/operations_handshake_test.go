// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"testing"

	model "forgejo.org/models/nativeoperation"

	"github.com/stretchr/testify/require"
)

func TestSupportedOperationsStable(t *testing.T) {
	svc := NewService()
	first, err := svc.SupportedOperations(t.Context())
	require.NoError(t, err)
	second, err := svc.SupportedOperations(t.Context())
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.NotEmpty(t, first.Generation)
	require.Equal(t, model.SupportedKinds(), first.Kinds)
	require.Contains(t, first.Kinds, model.KindLifecycleIssue)
	require.Contains(t, first.Kinds, model.KindLifecycleReview)
	require.Contains(t, first.Kinds, model.KindLifecycleNote)
	for i := 1; i < len(first.Kinds); i++ {
		require.Greater(t, first.Kinds[i], first.Kinds[i-1])
	}
}

func TestSupportedKindsMatchesValidKind(t *testing.T) {
	for _, kind := range model.SupportedKinds() {
		require.True(t, model.ValidKind(kind), kind)
	}
	require.False(t, model.ValidKind("unknown.kind"))
}
