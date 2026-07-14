// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package actions

import (
	"io"
	"net/http"

	actions_model "forgejo.org/models/actions"
	"forgejo.org/modules/log"
)

const jobSummaryRouteBase = "/_apis/pipelines/workflows/{run_id}/summary"

func uploadJobSummary(ctx *ArtifactContext) {
	task, _, ok := validateRunID(ctx)
	if !ok {
		return
	}

	content, err := io.ReadAll(io.LimitReader(ctx.Req.Body, actions_model.MaxJobSummarySize))
	if err != nil {
		log.Error("Error reading job summary: %v", err)
		ctx.Error(http.StatusInternalServerError, "Error reading job summary")
		return
	}

	summary := &actions_model.ActionRunJobSummary{
		JobID:   task.Job.ID,
		Attempt: task.Job.Attempt,
		RunID:   task.Job.RunID,
		RepoID:  task.RepoID,
		Content: string(content),
	}
	if err := actions_model.SetJobSummary(ctx, summary); err != nil {
		log.Error("Error saving job summary: %v", err)
		ctx.Error(http.StatusInternalServerError, "Error saving job summary")
		return
	}

	ctx.JSON(http.StatusOK, struct{}{})
}
