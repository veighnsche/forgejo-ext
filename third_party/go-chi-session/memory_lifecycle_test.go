// Copyright 2026 The Forgejo Authors.
// SPDX-License-Identifier: Apache-2.0

package session

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMemoryRegenerationDetachesHeldValues(t *testing.T) {
	p := new(MemProvider)
	require.NoError(t, p.Init(60, ""))
	held, err := p.Read("old")
	require.NoError(t, err)
	require.NoError(t, held.Set("uid", int64(42)))
	nested := map[string]string{"value": "original"}
	require.NoError(t, held.Set("nested", nested))
	next, err := p.Regenerate("old", "new")
	require.NoError(t, err)
	require.NoError(t, next.Set("uid", int64(77)))
	require.NoError(t, held.Set("uid", int64(42)))
	nested["value"] = "stale"
	require.NoError(t, held.Release())
	require.Equal(t, "old", held.ID())
	current, err := p.Read("new")
	require.NoError(t, err)
	require.Equal(t, int64(77), current.Get("uid"))
	require.Equal(t, "original", current.Get("nested").(map[string]string)["value"])
	require.False(t, p.Exist("old"))
}

func TestMemoryLifecycleIsAtomic(t *testing.T) {
	p := new(MemProvider)
	require.NoError(t, p.Init(60, ""))
	expired, err := p.Read("expired")
	require.NoError(t, err)
	require.NoError(t, expired.Set("uid", int64(42)))
	expired.(*MemStore).lastAccess = time.Now().Add(-2 * time.Minute)
	next, err := p.Regenerate("expired", "fresh")
	require.NoError(t, err)
	require.True(t, next.Empty(), "regeneration must not revive expired values")
	require.NoError(t, expired.Set("uid", int64(42)))
	require.NoError(t, expired.Release())
	require.True(t, next.Empty())
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			for range 25 {
				s, err := p.Read("shared")
				if err != nil {
					t.Error(err)
					return
				}
				if err := s.Set("note", true); err != nil {
					t.Error(err)
					return
				}
				_ = s.Empty()
				_ = p.Count()
				p.GC()
				if err := p.Destroy("shared"); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	workers.Wait()
}
