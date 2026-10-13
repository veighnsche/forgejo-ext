// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package runner

import (
	"context"
	"testing"
	"time"

	actions_model "forgejo.org/models/actions"
	"forgejo.org/models/nativeoperation"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/actions"

	runnerv1 "code.forgejo.org/forgejo/actions-proto/runner/v1"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestUpdateLogFinalization(t *testing.T) {
	for _, emptyFinal := range []bool{false, true} {
		name := "overlapping final rows"
		if emptyFinal {
			name = "empty final rows"
		}
		t.Run(name, func(t *testing.T) {
			unittest.PrepareTestEnv(t)
			task := unittest.AssertExistsAndLoadBean(t, &actions_model.ActionTask{ID: 47})
			task.LogFilename = t.Name() + ".log"
			task.LogInStorage = false
			task.LogLength = 0
			task.LogSize = 0
			task.LogIndexes = nil
			require.NoError(t, actions_model.UpdateTask(t.Context(), task, "log_filename", "log_in_storage", "log_length", "log_size", "log_indexes"))

			ctx := context.WithValue(t.Context(), runnerCtxKey{}, &actions_model.ActionRunner{ID: task.RunnerID})
			service := &Service{}
			rows := []*runnerv1.LogRow{
				{Time: timestamppb.New(time.Unix(1, 0)), Content: "first"},
				{Time: timestamppb.New(time.Unix(2, 0)), Content: "second"},
				{Time: timestamppb.New(time.Unix(3, 0)), Content: "last"},
			}
			upload := func(t *testing.T, index int64, batch []*runnerv1.LogRow, final bool, wantAck int64) {
				t.Helper()
				resp, err := service.UpdateLog(ctx, connect.NewRequest(&runnerv1.UpdateLogRequest{
					TaskId: task.ID,
					Index:  index,
					Rows:   batch,
					NoMore: final,
				}))
				require.NoError(t, err)
				assert.Equal(t, wantAck, resp.Msg.AckIndex)
			}
			upload(t, 0, rows[:2], false, 2)

			// Log telemetry and acknowledgements must continue while another
			// mutation owns the reservation. A no-op must not update task state.
			claimed, err := nativeoperation.ClaimOrdinary(ctx, "ord:log-test", `{"kind":"ordinary"}`, "v")
			require.NoError(t, err)
			defer func() { require.NoError(t, nativeoperation.ReleaseOwner(ctx, claimed.Owner)) }()
			before := unittest.AssertExistsAndLoadBean(t, &actions_model.ActionTask{ID: task.ID})
			for _, tc := range []struct {
				name  string
				index int64
				rows  []*runnerv1.LogRow
				final bool
			}{
				{"future final rows", 3, rows[2:], true},
				{"future empty final", 3, nil, true},
				{"stale final rows", 0, rows[:1], true},
				{"stale empty final", 1, nil, true},
				{"duplicate rows", 0, rows[:2], false},
				{"empty heartbeat", 2, nil, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					upload(t, tc.index, tc.rows, tc.final, 2)
					after := unittest.AssertExistsAndLoadBean(t, &actions_model.ActionTask{ID: task.ID})
					assert.Equal(t, before, after)
				})
			}

			finalIndex, finalRows := int64(1), rows[1:]
			if emptyFinal {
				upload(t, 1, rows[1:], false, 3)
				finalIndex, finalRows = 3, nil
			}
			upload(t, finalIndex, finalRows, true, 3)
			archived := unittest.AssertExistsAndLoadBean(t, &actions_model.ActionTask{ID: task.ID})
			require.True(t, archived.LogInStorage)
			logs, err := actions.ReadLogs(ctx, true, archived.LogFilename, 0, -1)
			require.NoError(t, err)
			require.Len(t, logs, len(rows))
			for i := range rows {
				assert.Equal(t, rows[i].Content, logs[i].Content)
			}

			// Retrying the final request must acknowledge the archive without
			// trying to transfer the already-removed temporary file again.
			upload(t, finalIndex, finalRows, true, 3)
		})
	}
}
