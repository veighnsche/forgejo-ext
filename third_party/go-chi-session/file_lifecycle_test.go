// Copyright 2026 The Forgejo Authors.
// SPDX-License-Identifier: Apache-2.0

package session

import (
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const lifecycleSID = "0123456789abcdef"

func lifecycleProviders(t *testing.T) (*FileProvider, *FileProvider) {
	t.Helper()
	root := t.TempDir()
	first, second := new(FileProvider), new(FileProvider)
	require.NoError(t, first.Init(60, root))
	require.NoError(t, second.Init(60, root))
	return first, second
}

func authenticatedSnapshot(t *testing.T, p *FileProvider) RawStore {
	t.Helper()
	s, err := p.Read(lifecycleSID)
	require.NoError(t, err)
	require.NoError(t, s.Set("uid", int64(42)))
	require.NoError(t, s.Release())
	return s
}

func expireFile(t *testing.T, p *FileProvider) {
	t.Helper()
	expired := time.Now().Add(-2 * time.Minute)
	require.NoError(t, os.Chtimes(p.filepath(lifecycleSID), expired, expired))
}

func TestFileDelayedRelease(t *testing.T) {
	for _, invalidation := range []string{"destroy", "regenerate", "expire", "expire-read", "gc", "recreate", "corrupt"} {
		t.Run(invalidation, func(t *testing.T) {
			p, other := lifecycleProviders(t)
			stale := authenticatedSnapshot(t, p)
			require.NoError(t, stale.Set("late", true))
			switch invalidation {
			case "destroy":
				require.NoError(t, other.Destroy(lifecycleSID))
			case "regenerate":
				next, err := other.Regenerate(lifecycleSID, "abcdef0123456789")
				require.NoError(t, err)
				require.Equal(t, int64(42), next.Get("uid"))
			case "expire":
				expireFile(t, p)
			case "expire-read":
				expireFile(t, p)
				empty, err := other.Read(lifecycleSID)
				require.NoError(t, err)
				require.True(t, empty.Empty())
			case "gc":
				expireFile(t, p)
				other.GC()
			case "recreate":
				require.NoError(t, other.Destroy(lifecycleSID))
				// An identical payload under the same SID is still a new generation.
				authenticatedSnapshot(t, other)
			case "corrupt":
				require.NoError(t, os.WriteFile(p.filepath(lifecycleSID), []byte("broken"), 0o600))
			}
			err := stale.Release()
			if invalidation == "corrupt" {
				require.Error(t, err)
				data, err := os.ReadFile(p.filepath(lifecycleSID))
				require.NoError(t, err)
				require.Equal(t, []byte("broken"), data)
				return
			}
			require.NoError(t, err)
			current, err := other.Read(lifecycleSID)
			require.NoError(t, err)
			require.Nil(t, current.Get("late"))
			if invalidation == "recreate" {
				require.Equal(t, int64(42), current.Get("uid"))
			} else {
				require.Nil(t, current.Get("uid"))
			}
		})
	}
}

func TestFileRepeatedReleaseAndReadOnly(t *testing.T) {
	p, other := lifecycleProviders(t)
	s := authenticatedSnapshot(t, p)
	before, err := os.Stat(p.filepath(lifecycleSID))
	require.NoError(t, err)
	require.NoError(t, s.Release())
	after, err := os.Stat(p.filepath(lifecycleSID))
	require.NoError(t, err)
	require.Equal(t, before.ModTime(), after.ModTime(), "read-only release must not renew expiry")
	require.NoError(t, s.Set("second", true))
	require.NoError(t, s.Release())
	current, err := other.Read(lifecycleSID)
	require.NoError(t, err)
	require.Equal(t, true, current.Get("second"))
	require.NoError(t, s.Flush())
	require.NoError(t, s.Release())
	current, err = other.Read(lifecycleSID)
	require.NoError(t, err)
	require.True(t, current.Empty(), "empty flush must persist")
	require.NoError(t, other.Destroy(lifecycleSID))
	require.NoError(t, s.Set("uid", int64(42)))
	require.NoError(t, s.Release())
	require.False(t, p.Exist(lifecycleSID))
	require.Zero(t, p.Count(), "the shared lock is not a session")
}

func TestFileConcurrentRevisionConflict(t *testing.T) {
	p, other := lifecycleProviders(t)
	authenticatedSnapshot(t, p)
	left, err := p.Read(lifecycleSID)
	require.NoError(t, err)
	right, err := other.Read(lifecycleSID)
	require.NoError(t, err)
	require.NoError(t, left.Set("left", true))
	require.NoError(t, right.Set("right", true))
	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for _, s := range []RawStore{left, right} {
		workers.Add(1)
		go func() { defer workers.Done(); <-start; results <- s.Release() }()
	}
	close(start)
	workers.Wait()
	close(results)
	conflicts := 0
	for err := range results {
		if errors.Is(err, ErrSessionConflict) {
			conflicts++
		} else {
			require.NoError(t, err)
		}
	}
	require.Equal(t, 1, conflicts, "exactly one snapshot may commit a given revision")
	current, err := p.Read(lifecycleSID)
	require.NoError(t, err)
	require.NotEqual(t, current.Get("left"), current.Get("right"))
	require.Equal(t, int64(42), current.Get("uid"))
	// A stale whole-store Flush cannot clear the winner's data either.
	require.NoError(t, left.Flush())
	require.NoError(t, right.Flush())
	leftErr, rightErr := left.Release(), right.Release()
	require.True(t, errors.Is(leftErr, ErrSessionConflict) || errors.Is(rightErr, ErrSessionConflict))
}

func TestFileInvalidFormatAndRegeneration(t *testing.T) {
	p, other := lifecycleProviders(t)
	original := authenticatedSnapshot(t, p)
	require.NoError(t, original.Set("uncommitted", true))
	destination := "abcdef0123456789"
	existing, err := other.Read(destination)
	require.NoError(t, err)
	require.NoError(t, existing.Set("destination", true))
	require.NoError(t, existing.Release())
	_, err = p.Regenerate(lifecycleSID, destination)
	require.Error(t, err)
	current, err := p.Read(lifecycleSID)
	require.NoError(t, err)
	require.Equal(t, int64(42), current.Get("uid"))
	expireFile(t, p)
	require.NoError(t, other.Destroy(destination))
	next, err := other.Regenerate(lifecycleSID, destination)
	require.NoError(t, err)
	require.True(t, next.Empty())
	legacy, err := EncodeGob(map[any]any{"uid": int64(42)})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(other.filepath(destination), legacy, 0o600))
	_, err = other.Read(destination)
	require.Error(t, err, "unversioned snapshots are rejected, never given fresh authority")
	// No code path may create a generation by directly constructing a raw store.
	fabricated := NewFileStore(p, lifecycleSID, map[any]any{"uid": int64(42)})
	require.NoError(t, fabricated.Set("uid", int64(42)))
	require.NoError(t, fabricated.Release())
	require.False(t, p.Exist(lifecycleSID))
}
