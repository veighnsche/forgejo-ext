// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package actions

import (
	"strings"
	"testing"

	"forgejo.org/models/unittest"
	"forgejo.org/modules/util"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJobSummary(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	const jobID, attempt = int64(1001), int64(1)

	_, err := GetJobSummary(t.Context(), jobID, attempt)
	require.ErrorIs(t, err, util.ErrNotExist)

	require.NoError(t, SetJobSummary(t.Context(), &ActionRunJobSummary{
		JobID: jobID, Attempt: attempt, RunID: 1, RepoID: 1, Content: "## initial",
	}))
	got, err := GetJobSummary(t.Context(), jobID, attempt)
	require.NoError(t, err)
	assert.Equal(t, "## initial", got.Content)

	require.NoError(t, SetJobSummary(t.Context(), &ActionRunJobSummary{
		JobID: jobID, Attempt: attempt, RunID: 1, RepoID: 1, Content: "### updated",
	}))
	got, err = GetJobSummary(t.Context(), jobID, attempt)
	require.NoError(t, err)
	assert.Equal(t, "### updated", got.Content)

	// The summary has to respect the apis upper body limit.
	// Therefore we need to truncate it
	require.NoError(t, SetJobSummary(t.Context(), &ActionRunJobSummary{
		JobID: jobID, Attempt: attempt, RunID: 1, RepoID: 1, Content: strings.Repeat("😁", MaxJobSummarySize+42),
	}))
	got, err = GetJobSummary(t.Context(), jobID, attempt)
	require.NoError(t, err)
	assert.Len(t, got.Content, MaxJobSummarySize)

	require.NoError(t, DeleteJobSummaries(t.Context(), jobID))
	_, err = GetJobSummary(t.Context(), jobID, attempt)
	assert.ErrorIs(t, err, util.ErrNotExist)
}
