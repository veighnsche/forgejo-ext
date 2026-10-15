// Copyright 2023 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package queue

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManager(t *testing.T) {
	defer test.MockVariableValue(&setting.AppDataPath, t.TempDir())()

	newQueueFromConfig := func(name, cfg string) (*WorkerPoolQueue[int], error) {
		cfgProvider, err := setting.NewConfigProviderFromData(cfg)
		if err != nil {
			return nil, err
		}
		qs, err := setting.GetQueueSettings(cfgProvider, name)
		if err != nil {
			return nil, err
		}
		return newWorkerPoolQueueForTest(name, qs, func(s ...int) (unhandled []int) { return nil }, false)
	}

	// test invalid CONN_STR
	_, err := newQueueFromConfig("default", `
[queue]
DATADIR = temp-dir
CONN_STR = redis://
`)
	require.ErrorContains(t, err, "invalid leveldb connection string")

	// test default config
	q, err := newQueueFromConfig("default", "")
	require.NoError(t, err)
	assert.Equal(t, "default", q.GetName())
	assert.Equal(t, "level", q.GetType())
	assert.Equal(t, filepath.Join(setting.AppDataPath, "queues/common"), q.baseConfig.DataFullDir)
	assert.Equal(t, 100000, q.baseConfig.Length)
	assert.Equal(t, 20, q.batchLength)
	assert.Empty(t, q.baseConfig.ConnStr)
	assert.Equal(t, "default_queue", q.baseConfig.QueueFullName)
	assert.Equal(t, "default_queue_unique", q.baseConfig.SetFullName)
	assert.NotZero(t, q.GetWorkerMaxNumber())
	assert.Equal(t, 0, q.GetWorkerNumber())
	assert.Equal(t, 0, q.GetWorkerActiveNumber())
	assert.Equal(t, 0, q.GetQueueItemNumber())
	assert.Equal(t, "int", q.GetItemTypeName())

	// test inherited config
	cfgProvider, err := setting.NewConfigProviderFromData(`
[queue]
TYPE = channel
DATADIR = queues/dir1
LENGTH = 100
BATCH_LENGTH = 20
CONN_STR = "addrs=127.0.0.1:6379 db=0"
QUEUE_NAME = _queue1

[queue.sub]
TYPE = level
DATADIR = queues/dir2
LENGTH = 102
BATCH_LENGTH = 22
CONN_STR =
QUEUE_NAME = _q2
SET_NAME = _u2
MAX_WORKERS = 123
`)

	require.NoError(t, err)

	q1 := createWorkerPoolQueue[string](t.Context(), "no-such", cfgProvider, nil, false)
	assert.Equal(t, "no-such", q1.GetName())
	assert.Equal(t, "dummy", q1.GetType()) // no handler, so it becomes dummy
	assert.Equal(t, filepath.Join(setting.AppDataPath, "queues/dir1"), q1.baseConfig.DataFullDir)
	assert.Equal(t, 100, q1.baseConfig.Length)
	assert.Equal(t, 20, q1.batchLength)
	assert.Equal(t, "addrs=127.0.0.1:6379 db=0", q1.baseConfig.ConnStr)
	assert.Equal(t, "no-such_queue1", q1.baseConfig.QueueFullName)
	assert.Equal(t, "no-such_queue1_unique", q1.baseConfig.SetFullName)
	assert.NotZero(t, q1.GetWorkerMaxNumber())
	assert.Equal(t, 0, q1.GetWorkerNumber())
	assert.Equal(t, 0, q1.GetWorkerActiveNumber())
	assert.Equal(t, 0, q1.GetQueueItemNumber())
	assert.Equal(t, "string", q1.GetItemTypeName())
	qid1 := GetManager().qidCounter

	q2 := createWorkerPoolQueue(t.Context(), "sub", cfgProvider, func(s ...int) (unhandled []int) { return nil }, false)
	assert.Equal(t, "sub", q2.GetName())
	assert.Equal(t, "level", q2.GetType())
	assert.Equal(t, filepath.Join(setting.AppDataPath, "queues/dir2"), q2.baseConfig.DataFullDir)
	assert.Equal(t, 102, q2.baseConfig.Length)
	assert.Equal(t, 22, q2.batchLength)
	assert.Empty(t, q2.baseConfig.ConnStr)
	assert.Equal(t, "sub_q2", q2.baseConfig.QueueFullName)
	assert.Equal(t, "sub_q2_u2", q2.baseConfig.SetFullName)
	assert.Equal(t, 123, q2.GetWorkerMaxNumber())
	assert.Equal(t, 0, q2.GetWorkerNumber())
	assert.Equal(t, 0, q2.GetWorkerActiveNumber())
	assert.Equal(t, 0, q2.GetQueueItemNumber())
	assert.Equal(t, "int", q2.GetItemTypeName())
	qid2 := GetManager().qidCounter

	assert.Equal(t, q1, GetManager().ManagedQueues()[qid1])

	GetManager().GetManagedQueue(qid1).SetWorkerMaxNumber(120)
	assert.Equal(t, 120, q1.workerMaxNum)

	stop := runWorkerPoolQueue(q2)
	require.NoError(t, GetManager().GetManagedQueue(qid2).FlushWithContext(t.Context(), 0))
	require.NoError(t, GetManager().FlushAll(t.Context(), 0))
	stop()
}

// Model two deferred handlers that keep handing work back to each other.
// FlushAll must drain the graph irrespective of the manager's map order.
type cascadingFlushQueue struct {
	ManagedWorkerPoolQueue
	work []func()
}

func (q *cascadingFlushQueue) GetQueueItemNumber() int    { return len(q.work) }
func (q *cascadingFlushQueue) GetWorkerActiveNumber() int { return 0 }
func (q *cascadingFlushQueue) FlushWithContext(ctx context.Context, _ time.Duration) error {
	for len(q.work) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		item := q.work[0]
		q.work = q.work[1:]
		item()
	}
	return nil
}

func TestManagerFlushCascadingWork(t *testing.T) {
	a, b := &cascadingFlushQueue{}, &cascadingFlushQueue{}
	m := &Manager{Queues: map[int64]ManagedWorkerPoolQueue{1: a, 2: b}}
	processed := 0
	var schedule func(*cascadingFlushQueue, *cascadingFlushQueue)
	schedule = func(from, to *cascadingFlushQueue) {
		from.work = append(from.work, func() {
			processed++
			if processed < 20 {
				schedule(to, from)
			}
		})
	}
	schedule(a, b)
	require.NoError(t, m.FlushAll(t.Context(), time.Second))
	assert.Equal(t, 20, processed)
}

func TestManagerFlushCascadingWorkDeadline(t *testing.T) {
	a := &cascadingFlushQueue{}
	var repeat func()
	repeat = func() { a.work = append(a.work, repeat) }
	repeat()
	m := &Manager{Queues: map[int64]ManagedWorkerPoolQueue{1: a}}
	require.ErrorIs(t, m.FlushAll(t.Context(), time.Millisecond), context.DeadlineExceeded)
}
