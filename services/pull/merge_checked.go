// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package pull

import (
	"context"
	"errors"
	"fmt"

	"forgejo.org/models"
	issues_model "forgejo.org/models/issues"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"
	"forgejo.org/modules/util"
	"forgejo.org/services/authz"
)

var (
	// ErrPullRequestBaseBranchChanged indicates that the pull request target
	// branch is no longer the target branch selected by the caller.
	ErrPullRequestBaseBranchChanged = errors.New("pull request base branch changed")
	// ErrPullRequestBaseCommitChanged indicates that the base branch advanced
	// after the pull request's mergeability checks completed.
	ErrPullRequestBaseCommitChanged = errors.New("pull request base commit changed")
)

// MergeWithChecks refreshes and checks a pull request immediately before
// merging it. It is intended for callers that must not rely on the cached
// mergeability state shown in a pull request list.
func MergeWithChecks(ctx context.Context, pr *issues_model.PullRequest, doer *user_model.User, baseGitRepo *git.Repository, mergeStyle repo_model.MergeStyle, expectedHeadCommitID, expectedBaseBranch string, reducer authz.AuthorizationReducer) error {
	defer pullWorkingPool.Lock(fmt.Sprint(pr.ID))()

	pr, err := issues_model.GetPullRequestByID(ctx, pr.ID)
	if err != nil {
		return fmt.Errorf("get pull request: %w", err)
	}
	if err := pr.LoadBaseRepo(ctx); err != nil {
		return fmt.Errorf("load base repository: %w", err)
	}
	if pr.BaseBranch != expectedBaseBranch {
		return ErrPullRequestBaseBranchChanged
	}
	if pr.HasMerged {
		return ErrHasMerged
	}
	if err := pr.LoadIssue(ctx); err != nil {
		return fmt.Errorf("load issue: %w", err)
	}
	if pr.Issue.IsClosed {
		return ErrIsClosed
	}

	var perm access_model.Permission
	if reducer == nil {
		perm, err = access_model.GetUserRepoPermission(ctx, pr.BaseRepo, doer)
	} else {
		perm, err = access_model.GetUserRepoPermissionWithReducer(ctx, pr.BaseRepo, doer, reducer)
	}
	if err != nil {
		return fmt.Errorf("get user repository permission: %w", err)
	}
	allowed, err := IsUserAllowedToMerge(ctx, pr, perm, doer)
	if err != nil {
		return fmt.Errorf("check user merge permission: %w", err)
	}
	if !allowed {
		return ErrUserNotAllowedToMerge
	}
	if pr.IsWorkInProgress(ctx) {
		return ErrIsWorkInProgress
	}
	if err := pr.LoadHeadRepo(ctx); err != nil {
		return fmt.Errorf("load head repository: %w", err)
	}
	if pr.HeadRepo == nil {
		return fmt.Errorf("head repository: %w", util.ErrNotExist)
	}

	expectedBaseCommitID, err := refreshMergeState(ctx, pr, expectedHeadCommitID)
	if err != nil {
		return err
	}
	if pr.HeadCommitID != expectedHeadCommitID {
		return models.ErrSHADoesNotMatch{
			Path:       pr.HeadBranch,
			GivenSHA:   expectedHeadCommitID,
			CurrentSHA: pr.HeadCommitID,
		}
	}

	if err := CheckPullMergeable(ctx, doer, &perm, pr, MergeCheckTypeGeneral, false, mergeStyle); err != nil {
		return err
	}
	if statusPass, err := isPullCommitStatusPassForSHA(ctx, pr, expectedHeadCommitID); err != nil {
		return err
	} else if !statusPass {
		return models.ErrDisallowedToMerge{Reason: "Not all required status checks successful"}
	}
	if _, err := isSignedIfRequiredAtHeadCommit(ctx, pr, doer, mergeStyle, expectedHeadCommitID); err != nil {
		return err
	}

	message, body, err := GetDefaultMergeMessage(ctx, baseGitRepo, pr, mergeStyle)
	if err != nil {
		return fmt.Errorf("get default merge message: %w", err)
	}
	if body != "" {
		if message != "" {
			message += "\n\n"
		}
		message += body
	}

	return merge(ctx, pr, doer, baseGitRepo, mergeStyle, expectedHeadCommitID, expectedBaseBranch, expectedBaseCommitID, message, false)
}

// refreshMergeState calculates the values that merge checks depend on from a
// fresh temporary repository. The values are deliberately kept in-memory:
// the normal background patch checker remains responsible for persisting its
// list-view cache.
func refreshMergeState(ctx context.Context, pr *issues_model.PullRequest, expectedHeadCommitID string) (string, error) {
	testPatchCtx, err := getTestPatchCtx(ctx, pr, git.SupportGitMergeTree)
	if err != nil {
		testPatchCtx.close()
		return "", fmt.Errorf("get test patch context: %w", err)
	}
	defer testPatchCtx.close()

	sourceCommitID, err := getTestPatchHeadCommitID(ctx, testPatchCtx)
	if err != nil {
		return "", err
	}
	if sourceCommitID != expectedHeadCommitID {
		return "", models.ErrSHADoesNotMatch{
			Path:       pr.HeadBranch,
			GivenSHA:   expectedHeadCommitID,
			CurrentSHA: sourceCommitID,
		}
	}
	expectedBaseCommitID, err := testPatchCtx.gitRepo.GetRefCommitID(testPatchCtx.baseRev)
	if err != nil {
		return "", fmt.Errorf("get base commit ID before mergeability check: %w", err)
	}

	if err := testPatchWithContext(ctx, pr, testPatchCtx); err != nil {
		return "", err
	}
	currentSourceCommitID, err := getTestPatchHeadCommitID(ctx, testPatchCtx)
	if err != nil {
		return "", err
	}
	if currentSourceCommitID != sourceCommitID {
		return "", models.ErrSHADoesNotMatch{
			Path:       pr.HeadBranch,
			GivenSHA:   sourceCommitID,
			CurrentSHA: currentSourceCommitID,
		}
	}
	currentBaseCommitID, err := testPatchCtx.gitRepo.GetRefCommitID(testPatchCtx.baseRev)
	if err != nil {
		return "", fmt.Errorf("get base commit ID after mergeability check: %w", err)
	}
	if currentBaseCommitID != expectedBaseCommitID {
		return "", ErrPullRequestBaseCommitChanged
	}

	divergence, err := git.GetDivergingCommits(ctx, testPatchCtx.gitRepo.Path, testPatchCtx.baseRev, testPatchCtx.headRev, testPatchCtx.env)
	if err != nil {
		return "", fmt.Errorf("get pull request divergence: %w", err)
	}
	pr.CommitsAhead = divergence.Ahead
	pr.CommitsBehind = divergence.Behind

	currentSourceCommitID, err = getTestPatchHeadCommitID(ctx, testPatchCtx)
	if err != nil {
		return "", err
	}
	if currentSourceCommitID != sourceCommitID {
		return "", models.ErrSHADoesNotMatch{
			Path:       pr.HeadBranch,
			GivenSHA:   sourceCommitID,
			CurrentSHA: currentSourceCommitID,
		}
	}
	currentBaseCommitID, err = testPatchCtx.gitRepo.GetRefCommitID(testPatchCtx.baseRev)
	if err != nil {
		return "", fmt.Errorf("get base commit ID after divergence check: %w", err)
	}
	if currentBaseCommitID != expectedBaseCommitID {
		return "", ErrPullRequestBaseCommitChanged
	}
	pr.HeadCommitID = sourceCommitID
	return expectedBaseCommitID, nil
}

func getTestPatchHeadCommitID(ctx context.Context, testPatchCtx *testPatchContext) (string, error) {
	if testPatchCtx.headIsCommitID {
		return testPatchCtx.headRev, nil
	}
	headCommitID, err := testPatchCtx.gitRepo.GetRefCommitID(testPatchCtx.headRev)
	if err != nil {
		return "", fmt.Errorf("get head commit ID: %w", err)
	}
	return headCommitID, nil
}
