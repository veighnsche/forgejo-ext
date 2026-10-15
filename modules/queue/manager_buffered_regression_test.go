// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package queue

import (
	"sync/atomic"
	"testing"
	"time"

	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"

	"github.com/stretchr/testify/require"
)

func TestManagerFlushBufferedCascadeRegression(t *testing.T) {
	// Keep follow-up work buffered until the manager explicitly flushes it.
	// A processed handler sends back to a queue already visited this pass.
	defer test.MockVariableValue(&batchDebounceDuration, time.Minute)()
	var processed atomic.Int32
	var failed atomic.Bool
	var a, b *WorkerPoolQueue[int]
	handler := func(destination **WorkerPoolQueue[int]) HandlerFuncT[int] {
		return func(items ...int) []int {
			for _, n := range items {
				processed.Add(1)
				if n > 1 {
					if err := (*destination).Push(n - 1); err != nil {
						failed.Store(true)
						return nil
					}
					deadline := time.Now().Add(time.Second)
					for (*destination).GetQueueItemNumber() != 0 && time.Now().Before(deadline) {
						time.Sleep(time.Millisecond)
					}
				}
			}
			return nil
		}
	}
	var err error
	config := setting.QueueSettings{Type: "channel", Length: 100, BatchLength: 100, MaxWorkers: 1}
	a, err = newWorkerPoolQueueForTest("cascade-a", config, handler(&b), false)
	require.NoError(t, err)
	b, err = newWorkerPoolQueueForTest("cascade-b", config, handler(&a), false)
	require.NoError(t, err)
	stopA, stopB := runWorkerPoolQueue(a), runWorkerPoolQueue(b)
	defer stopA()
	defer stopB()
	m := &Manager{Queues: map[int64]ManagedWorkerPoolQueue{1: a, 2: b}}
	require.NoError(t, a.Push(6))
	require.NoError(t, m.FlushAll(t.Context(), 3*time.Second))
	require.False(t, failed.Load())
	require.EqualValues(t, 6, processed.Load())
}
