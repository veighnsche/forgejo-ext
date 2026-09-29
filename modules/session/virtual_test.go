// Copyright 2026 The Forgejo Authors.
// SPDX-License-Identifier: MIT

package session

import (
	"testing"

	"code.forgejo.org/go-chi/session"
	"forgejo.org/modules/json"
	"github.com/stretchr/testify/require"
)

func TestVirtualFileStoreRetainsGeneration(t *testing.T) {
	config, err := json.Marshal(session.Options{Provider: "file", ProviderConfig: t.TempDir()})
	require.NoError(t, err)
	first, second := new(VirtualSessionProvider), new(VirtualSessionProvider)
	require.NoError(t, first.Init(60, string(config)))
	require.NoError(t, second.Init(60, string(config)))
	sid := "0123456789abcdef"
	initial, err := first.Read(sid)
	require.NoError(t, err)
	require.IsType(t, &VirtualStore{}, initial)
	require.NoError(t, initial.Set("uid", int64(42)))
	require.NoError(t, initial.Release())
	require.NoError(t, initial.Set("note", "second write"))
	require.NoError(t, initial.Release())
	current, err := second.Read(sid)
	require.NoError(t, err)
	require.Equal(t, "second write", current.Get("note"))
	require.NoError(t, second.Destroy(sid))
	recreated, err := second.Read(sid)
	require.NoError(t, err)
	require.NoError(t, recreated.Set("uid", int64(77)))
	require.NoError(t, recreated.Release())
	require.NoError(t, initial.Set("stale", true))
	require.NoError(t, initial.Release())
	current, err = second.Read(sid)
	require.NoError(t, err)
	require.Equal(t, int64(77), current.Get("uid"))
	require.Nil(t, current.Get("stale"))
	require.NoError(t, recreated.Flush())
	require.NoError(t, recreated.Release())
	current, err = second.Read(sid)
	require.NoError(t, err)
	require.True(t, current.Empty())
}

func TestVirtualStoreRegenerationDoesNotAdoptNewAuthority(t *testing.T) {
	for _, provider := range []string{"memory", "file"} {
		t.Run(provider, func(t *testing.T) {
			config, err := json.Marshal(session.Options{Provider: provider, ProviderConfig: t.TempDir()})
			require.NoError(t, err)
			p := new(VirtualSessionProvider)
			require.NoError(t, p.Init(60, string(config)))
			original, err := p.Read("0123456789abcdef")
			require.NoError(t, err)
			require.NoError(t, original.Set("uid", int64(42)))
			require.NoError(t, original.Release())
			regenerated, err := p.Regenerate(original.ID(), "abcdef0123456789")
			require.NoError(t, err)
			require.NoError(t, regenerated.Set("uid", int64(77)))
			require.NoError(t, regenerated.Release())
			require.NoError(t, original.Set("stale", true))
			require.NoError(t, original.Release())
			current, err := p.Read(regenerated.ID())
			require.NoError(t, err)
			require.Equal(t, int64(77), current.Get("uid"))
			require.Nil(t, current.Get("stale"))
		})
	}
}

func TestMemoryRawStoreCannotMutateRegeneratedSession(t *testing.T) {
	config, err := json.Marshal(session.Options{Provider: "memory"})
	require.NoError(t, err)
	p := new(VirtualSessionProvider)
	require.NoError(t, p.Init(60, string(config)))
	original, err := p.Read("0123456789abcdef")
	require.NoError(t, err)
	require.NoError(t, original.Set("uid", int64(42)))
	require.NoError(t, original.Release())
	held, err := p.Read(original.ID())
	require.NoError(t, err)
	require.IsType(t, &session.MemStore{}, held)
	regenerated, err := p.Regenerate(original.ID(), "abcdef0123456789")
	require.NoError(t, err)
	require.NoError(t, regenerated.Set("uid", int64(77)))
	require.NoError(t, held.Set("uid", int64(42)))
	require.NoError(t, held.Release())
	current, err := p.Read(regenerated.ID())
	require.NoError(t, err)
	require.Equal(t, int64(77), current.Get("uid"))
	require.Equal(t, original.ID(), held.ID())
}
