// Copyright 2026 The Soda Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package issues

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"forgejo.org/modules/indexer/issues/internal"
	"forgejo.org/modules/queue"
	"forgejo.org/modules/setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countingIssueIndexer struct {
	internal.Indexer
	closes atomic.Int32
}

func (idx *countingIssueIndexer) Close() { idx.closes.Add(1) }

func TestRetireIssueIndexerWaitsForHandler(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseHandler := func() { releaseOnce.Do(func() { close(release) }) }
	started := make(chan struct{})
	q, err := queue.NewWorkerPoolQueueWithContext(ctx, "issue-indexer-retirement", setting.QueueSettings{
		Type: "channel", Length: 10, BatchLength: 1, MaxWorkers: 1,
	}, func(items ...int) []int {
		close(started)
		<-release
		return nil
	}, false)
	require.NoError(t, err)
	// Register before starting or pushing: every assertion failure must release
	// the blocked handler and stop this exact queue, including startup failures.
	t.Cleanup(func() {
		releaseHandler()
		q.ShutdownWait(5 * time.Second)
		assert.Eventually(t, func() bool {
			return q.GetWorkerNumber() == 0 && q.GetWorkerActiveNumber() == 0
		}, 5*time.Second, time.Millisecond, "test queue did not retire")
	})
	go q.Run()
	require.NoError(t, q.Push(1))
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not start")
	}

	idx := &countingIssueIndexer{}
	var closeOnce sync.Once
	require.False(t, retireIssueIndexer(q, idx, 50*time.Millisecond, &closeOnce))
	require.Zero(t, idx.closes.Load(), "shutdown timeout must not authorize Close")

	releaseHandler()
	require.Eventually(t, func() bool {
		return q.GetWorkerNumber() == 0 && q.GetWorkerActiveNumber() == 0
	}, 5*time.Second, time.Millisecond, "released handler did not retire")
	require.True(t, retireIssueIndexer(q, idx, time.Second, &closeOnce))
	require.Equal(t, int32(1), idx.closes.Load())
	require.True(t, retireIssueIndexer(q, idx, time.Second, &closeOnce))
	require.Equal(t, int32(1), idx.closes.Load(), "repeat retirement must not close again")
}
