// Copyright 2021 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package private

import (
	"fmt"
	"net/http"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/private"
	app_context "forgejo.org/services/context"
	nativeoperation "forgejo.org/services/nativeoperation"
)

// SetDefaultBranch updates the default branch
func SetDefaultBranch(ctx *app_context.PrivateContext) {
	ownerName := ctx.Params(":owner")
	repoName := ctx.Params(":repo")
	branch := ctx.Params(":branch")

	// Nested participating writers below reuse the bound execution
	// instead of claiming again. While idle this returns the context
	// unchanged; under a foreign hold without a matching proof it
	// refuses before any effect.
	reqCtx, err := nativeoperation.BoundCallbackContext(ctx, ctx.FormString("exec_proof"), "")
	if err != nil {
		ctx.JSON(http.StatusForbidden, private.Response{
			UserMsg: "native operation in progress",
		})
		return
	}

	ctx.Repo.Repository.DefaultBranch = branch
	if err := gitrepo.SetDefaultBranch(reqCtx, ctx.Repo.Repository, ctx.Repo.Repository.DefaultBranch); err != nil {
		ctx.JSON(http.StatusInternalServerError, private.Response{
			Err: fmt.Sprintf("Unable to set default branch on repository: %s/%s Error: %v", ownerName, repoName, err),
		})
		return
	}

	if err := repo_model.UpdateDefaultBranch(reqCtx, ctx.Repo.Repository); err != nil {
		ctx.JSON(http.StatusInternalServerError, private.Response{
			Err: fmt.Sprintf("Unable to set default branch on repository: %s/%s Error: %v", ownerName, repoName, err),
		})
		return
	}
	ctx.PlainText(http.StatusOK, "success")
}
