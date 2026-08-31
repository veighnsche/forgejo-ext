// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package pull

import (
	"context"

	issues_model "forgejo.org/models/issues"
	access_model "forgejo.org/models/perm/access"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/services/authz"
)

// ListMergeInfo pins the source revision shown when selecting a pull request.
type ListMergeInfo struct {
	HeadCommitID string
}

// GetListMergeInfo returns selectable open pull requests, using merge permissions
// rather than issue editing permissions. Authorization is checked again on merge.
func GetListMergeInfo(ctx context.Context, issues issues_model.IssueList, doer *user_model.User, reducer authz.AuthorizationReducer) (map[int64]*ListMergeInfo, error) {
	info := make(map[int64]*ListMergeInfo)
	if doer == nil {
		return info, nil
	}

	permissions := make(map[int64]access_model.Permission)
	repositories := make(map[int64]*git.Repository)
	defer func() {
		for _, repository := range repositories {
			repository.Close()
		}
	}()

	for _, issue := range issues {
		if !issue.IsPull || issue.IsClosed || issue.PullRequest == nil || issue.PullRequest.HasMerged {
			continue
		}
		pr := issue.PullRequest
		if err := pr.LoadBaseRepo(ctx); err != nil {
			return nil, err
		}
		if pr.BaseRepo.IsArchived || pr.BaseRepo.IsMirror {
			continue
		}
		permission, ok := permissions[pr.BaseRepoID]
		if !ok {
			var err error
			if reducer == nil {
				permission, err = access_model.GetUserRepoPermission(ctx, pr.BaseRepo, doer)
			} else {
				permission, err = access_model.GetUserRepoPermissionWithReducer(ctx, pr.BaseRepo, doer, reducer)
			}
			if err != nil {
				return nil, err
			}
			permissions[pr.BaseRepoID] = permission
		}
		if !permission.CanRead(unit.TypeCode) || !permission.CanRead(unit.TypePullRequests) {
			continue
		}
		allowed, err := IsUserAllowedToMerge(ctx, pr, permission, doer)
		if err != nil {
			return nil, err
		}
		if !allowed {
			continue
		}

		gitRepo := repositories[pr.BaseRepoID]
		if gitRepo == nil {
			gitRepo, err = gitrepo.OpenRepository(ctx, pr.BaseRepo)
			if err != nil {
				return nil, err
			}
			repositories[pr.BaseRepoID] = gitRepo
		}
		headCommitID, err := gitRepo.GetRefCommitID(pr.GetGitRefName())
		if git.IsErrNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		info[issue.ID] = &ListMergeInfo{HeadCommitID: headCommitID}
	}
	return info, nil
}
