// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	stdCtx "context"
	"errors"
	"net/http"
	"strings"
	"time"

	"forgejo.org/models"
	git_model "forgejo.org/models/git"
	issues_model "forgejo.org/models/issues"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/modules/git"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/util"
	"forgejo.org/modules/web"
	asymkey_service "forgejo.org/services/asymkey"
	"forgejo.org/services/context"
	"forgejo.org/services/forms"
	pull_service "forgejo.org/services/pull"
	repo_service "forgejo.org/services/repository"
)

type pullListMergeResult struct {
	Merged  bool   `json:"merged"`
	Message string `json:"message"`
	Warning string `json:"warning,omitempty"`
}

// MergePullRequestFromList merges one selected PR. The list submits these requests
// sequentially so every PR is checked against the result of the preceding merge.
func MergePullRequestFromList(ctx *context.Context) {
	form := web.GetForm(ctx).(*forms.MergePullRequestFromListForm)
	if ctx.HasError() {
		ctx.JSON(http.StatusBadRequest, pullListMergeResult{Message: ctx.GetErrMsg()})
		return
	}

	pr, err := issues_model.GetPullRequestByIndex(ctx, ctx.Repo.Repository.ID, ctx.ParamsInt64(":index"))
	if err != nil {
		if issues_model.IsErrPullRequestNotExist(err) {
			ctx.NotFound("GetPullRequestByIndex", err)
		} else {
			ctx.ServerError("GetPullRequestByIndex", err)
		}
		return
	}
	pr.BaseRepo = ctx.Repo.Repository
	if err := pr.LoadIssue(ctx); err != nil {
		ctx.ServerError("LoadIssue", err)
		return
	}
	pr.Issue.Repo = pr.BaseRepo

	prUnit, err := pr.BaseRepo.GetUnit(ctx, unit.TypePullRequests)
	if err != nil {
		ctx.ServerError("GetUnit", err)
		return
	}
	config := prUnit.PullRequestsConfig()
	mergeStyle := repo_model.MergeStyle(form.Do)
	if form.Do == "default" {
		mergeStyle = config.GetDefaultMergeStyle()
		if mergeStyle == repo_model.MergeStyleManuallyMerged || !config.IsMergeStyleAllowed(mergeStyle) {
			mergeStyle = ""
			for _, style := range []repo_model.MergeStyle{
				repo_model.MergeStyleMerge, repo_model.MergeStyleRebase, repo_model.MergeStyleRebaseMerge,
				repo_model.MergeStyleSquash, repo_model.MergeStyleFastForwardOnly,
			} {
				if config.IsMergeStyleAllowed(style) {
					mergeStyle = style
					break
				}
			}
		}
	}
	if !config.IsMergeStyleAllowed(mergeStyle) {
		ctx.JSON(http.StatusConflict, pullListMergeResult{Message: ctx.Locale.TrString("repo.pulls.invalid_merge_option")})
		return
	}

	// As with individual merges, finish the current operation if the browser goes
	// away. The client must not retry a request whose result is unknown.
	workCtx, cancel := stdCtx.WithTimeout(stdCtx.WithoutCancel(ctx), time.Duration(setting.Git.Timeout.Default)*time.Second)
	defer cancel()
	err = pull_service.MergeWithChecks(workCtx, pr, ctx.Doer, ctx.Repo.GitRepo, mergeStyle, form.HeadCommitID, form.BaseBranch, ctx.Authentication.Reducer())
	if err != nil {
		if message := pullListMergeError(ctx, err); message != "" {
			status := http.StatusConflict
			if errors.Is(err, pull_service.ErrUserNotAllowedToMerge) {
				status = http.StatusForbidden
			}
			ctx.JSON(status, pullListMergeResult{Message: message})
			return
		}

		// A notification or other follow-up may fail after Git has already merged
		// the PR. Do not report a completed merge as a failed merge or retry it.
		log.Error("MergeWithChecks for pull request %d: %v", pr.ID, err)
		resultCtx, cancelResult := stdCtx.WithTimeout(stdCtx.WithoutCancel(ctx), 5*time.Second)
		defer cancelResult()
		current, loadErr := issues_model.GetPullRequestByID(resultCtx, pr.ID)
		if loadErr == nil && current.HasMerged {
			ctx.JSON(http.StatusOK, pullListMergeResult{
				Merged: true, Message: ctx.Locale.TrString("repo.pulls.merged"), Warning: ctx.Locale.TrString("repo.pulls.bulk_merge_follow_up_failed"),
			})
			return
		}
		ctx.JSON(http.StatusInternalServerError, pullListMergeResult{Message: ctx.Locale.TrString("repo.pulls.bulk_merge_unknown")})
		return
	}

	var warnings []string
	if err := stopTimerIfAvailable(workCtx, ctx.Doer, pr.Issue); err != nil {
		log.Error("stopTimerIfAvailable after merging pull request %d: %v", pr.ID, err)
		warnings = append(warnings, ctx.Locale.TrString("repo.pulls.bulk_merge_follow_up_failed"))
	}
	if form.DeleteBranchAfterMerge {
		if warning := deletePullListHeadBranch(ctx, workCtx, pr); warning != "" {
			warnings = append(warnings, warning)
		}
	}
	ctx.JSON(http.StatusOK, pullListMergeResult{Merged: true, Message: ctx.Locale.TrString("repo.pulls.merged"), Warning: strings.Join(warnings, " ")})
}

func pullListMergeError(ctx *context.Context, err error) string {
	switch {
	case errors.Is(err, pull_service.ErrUserNotAllowedToMerge):
		return ctx.Locale.TrString("repo.pulls.no_merge_access")
	case errors.Is(err, pull_service.ErrHasMerged), models.IsErrPullRequestHasMerged(err):
		return ctx.Locale.TrString("repo.pulls.has_merged")
	case errors.Is(err, pull_service.ErrIsClosed):
		return ctx.Locale.TrString("repo.pulls.is_closed")
	case errors.Is(err, pull_service.ErrIsWorkInProgress):
		return ctx.Locale.TrString("repo.pulls.no_merge_wip")
	case errors.Is(err, pull_service.ErrIsChecking):
		return ctx.Locale.TrString("repo.pulls.is_checking")
	case errors.Is(err, pull_service.ErrNotMergeableState), models.IsErrMergeConflicts(err), models.IsErrRebaseConflicts(err):
		return ctx.Locale.TrString("repo.pulls.cannot_auto_merge_desc")
	case errors.Is(err, pull_service.ErrDependenciesLeft):
		return ctx.Locale.TrString("repo.issues.dependency.pr_close_blocked")
	case errors.Is(err, pull_service.ErrPullRequestBaseBranchChanged):
		return ctx.Locale.TrString("repo.pulls.bulk_merge_target_changed")
	case errors.Is(err, pull_service.ErrPullRequestBaseCommitChanged), git.IsErrPushOutOfDate(err):
		return ctx.Locale.TrString("repo.pulls.merge_out_of_date")
	case models.IsErrSHADoesNotMatch(err):
		return ctx.Locale.TrString("repo.pulls.head_out_of_date")
	case models.IsErrInvalidMergeStyle(err):
		return ctx.Locale.TrString("repo.pulls.invalid_merge_option")
	case models.IsErrMergeDivergingFastForwardOnly(err):
		return ctx.Locale.TrString("repo.pulls.bulk_merge_not_fast_forward")
	case models.IsErrDisallowedToMerge(err):
		return ctx.Locale.TrString("repo.pulls.bulk_merge_not_ready", err.Error())
	case asymkey_service.IsErrWontSign(err):
		return ctx.Locale.TrString("repo.pulls.require_signed_wont_sign")
	case models.IsErrMergeUnrelatedHistories(err):
		return ctx.Locale.TrString("repo.pulls.unrelated_histories")
	case git.IsErrPushRejected(err):
		return ctx.Locale.TrString("repo.pulls.push_rejected")
	case errors.Is(err, util.ErrNotExist):
		return ctx.Locale.TrString("repo.pulls.bulk_merge_branch_missing")
	default:
		return ""
	}
}

func deletePullListHeadBranch(ctx *context.Context, workCtx stdCtx.Context, pr *issues_model.PullRequest) string {
	if err := pr.LoadHeadRepo(workCtx); err != nil {
		log.Error("LoadHeadRepo after merging pull request %d: %v", pr.ID, err)
		return ctx.Locale.TrString("repo.pulls.bulk_merge_delete_failed")
	}
	if pr.HeadRepo == nil {
		return ctx.Locale.TrString("repo.pulls.bulk_merge_delete_failed")
	}
	if reducer := ctx.Authentication.Reducer(); reducer != nil {
		permission, err := access_model.GetUserRepoPermissionWithReducer(workCtx, pr.HeadRepo, ctx.Doer, reducer)
		if err != nil {
			log.Error("GetUserRepoPermissionWithReducer after merging pull request %d: %v", pr.ID, err)
			return ctx.Locale.TrString("repo.pulls.bulk_merge_delete_failed")
		}
		if !permission.CanWrite(unit.TypeCode) {
			return ctx.Locale.TrString("repo.pulls.delete_after_merge.head_branch.insufficient_branch")
		}
	}
	headRepo, err := gitrepo.OpenRepository(workCtx, pr.HeadRepo)
	if err != nil {
		log.Error("OpenRepository after merging pull request %d: %v", pr.ID, err)
		return ctx.Locale.TrString("repo.pulls.bulk_merge_delete_failed")
	}
	defer headRepo.Close()
	if err := repo_service.DeleteBranchAfterMerge(workCtx, ctx.Doer, pr, headRepo); err != nil {
		switch {
		case errors.Is(err, repo_service.ErrBranchIsDefault):
			return ctx.Locale.TrString("repo.pulls.delete_after_merge.head_branch.is_default")
		case errors.Is(err, git_model.ErrBranchIsProtected):
			return ctx.Locale.TrString("repo.pulls.delete_after_merge.head_branch.is_protected")
		case errors.Is(err, util.ErrPermissionDenied):
			return ctx.Locale.TrString("repo.pulls.delete_after_merge.head_branch.insufficient_branch")
		default:
			log.Error("DeleteBranchAfterMerge for pull request %d: %v", pr.ID, err)
			return ctx.Locale.TrString("repo.pulls.bulk_merge_delete_failed")
		}
	}
	if headRepo.IsBranchExist(pr.HeadBranch) {
		return ctx.Locale.TrString("repo.pulls.bulk_merge_branch_retained")
	}
	return ""
}
