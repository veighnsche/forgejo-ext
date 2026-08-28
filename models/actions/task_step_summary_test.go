// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package actions

import (
	"strings"
	"testing"
	"unicode/utf8"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskStepSummary(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	const taskID, repoID = int64(47), int64(4)
	stepOne := &ActionTaskStep{TaskID: taskID, Index: 0, RepoID: repoID}
	stepTwo := &ActionTaskStep{TaskID: taskID, Index: 1, RepoID: repoID}
	require.NoError(t, db.Insert(t.Context(), stepOne, stepTwo))

	summaries, err := GetTaskStepSummaries(t.Context(), taskID)
	require.NoError(t, err)
	assert.Empty(t, summaries)

	require.NoError(t, SetTaskStepSummaries(t.Context(), []*ActionTaskStepSummary{
		{StepID: stepOne.ID, TaskID: taskID, RepoID: repoID, Content: "## first"},
		{StepID: stepTwo.ID, TaskID: taskID, RepoID: repoID, Content: "## second"},
	}))
	summaries, err = GetTaskStepSummaries(t.Context(), taskID)
	require.NoError(t, err)
	require.Len(t, summaries, 2)
	contents := map[int64]string{}
	for _, summary := range summaries {
		contents[summary.StepID] = summary.Content
	}
	assert.Equal(t, map[int64]string{stepOne.ID: "## first", stepTwo.ID: "## second"}, contents)

	// resending the full content of a step does an update instaead of duplicating it
	require.NoError(t, SetTaskStepSummaries(t.Context(), []*ActionTaskStepSummary{
		{StepID: stepOne.ID, TaskID: taskID, RepoID: repoID, Content: "### updated"},
	}))
	summaries, err = GetTaskStepSummaries(t.Context(), taskID)
	require.NoError(t, err)
	require.Len(t, summaries, 2)
	for _, summary := range summaries {
		if summary.StepID == stepOne.ID {
			assert.Equal(t, "### updated", summary.Content)
		}
	}

	// content beyond the size limit is truncated without invalidating the UTF-8
	require.NoError(t, SetTaskStepSummaries(t.Context(), []*ActionTaskStepSummary{
		{StepID: stepOne.ID, TaskID: taskID, RepoID: repoID, Content: strings.Repeat("😁", MaxStepSummarySize+42)},
	}))
	summaries, err = GetTaskStepSummaries(t.Context(), taskID)
	require.NoError(t, err)
	for _, summary := range summaries {
		if summary.StepID == stepOne.ID {
			assert.LessOrEqual(t, len(summary.Content), MaxStepSummarySize)
			assert.True(t, utf8.ValidString(summary.Content))
			assert.True(t, strings.HasSuffix(summary.Content, "…"))
		}
	}

	require.NoError(t, DeleteTaskStepSummaries(t.Context(), taskID))
	summaries, err = GetTaskStepSummaries(t.Context(), taskID)
	require.NoError(t, err)
	assert.Empty(t, summaries)
}
