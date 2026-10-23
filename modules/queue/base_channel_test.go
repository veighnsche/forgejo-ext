// Copyright 2023 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package queue

import (
	"testing"
	"time"

	"forgejo.org/modules/log"
	"forgejo.org/modules/test"

	"github.com/stretchr/testify/require"
)

func TestBaseChannel(t *testing.T) {
	checker, cleanup := test.NewLogChecker(log.DEFAULT, log.INFO)
	defer cleanup()
	checker.Filter("synchronization failure").StopMark("base-channel-check-complete")

	testQueueBasic(t, newBaseChannelSimple, &BaseConfig{ManagedName: "baseChannel", Length: 10}, false)
	testQueueBasic(t, newBaseChannelUnique, &BaseConfig{ManagedName: "baseChannel", Length: 10}, true)

	// Non-unique queues accept duplicate items without uniqueness tracking.
	// Exercise both implementations before checking the asynchronous log writer.
	log.Info("base-channel-check-complete")
	filtered, stopped := checker.Check(time.Second)
	require.True(t, stopped, "queue log observation did not complete")
	require.Equal(t, []bool{false}, filtered, "valid queue operations reported corruption")
}
