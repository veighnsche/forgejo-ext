// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	unit_model "forgejo.org/models/unit"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/git"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/test"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type listMergeResponse struct {
	Merged  bool   `json:"merged"`
	Message string `json:"message"`
	Warning string `json:"warning"`
}

func listMergePR(t *testing.T, session *TestSession, branch, title string) *issues_model.PullRequest {
	t.Helper()
	testRepoFork(t, session, "user2", "repo1", "user1", "repo1")
	testEditFileToNewBranch(t, session, "user1", "repo1", "master", branch, "README.md", "change for "+branch+"\n")
	created := testPullCreate(t, session, "user1", "repo1", false, "master", branch, title)

	redirect := strings.Trim(test.RedirectURL(created), "/")
	parts := strings.Split(redirect, "/")
	require.GreaterOrEqual(t, len(parts), 4, "unexpected pull request redirect: %s", redirect)
	index, err := strconv.ParseInt(parts[len(parts)-1], 10, 64)
	require.NoError(t, err)

	baseRepo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{OwnerName: "user2", Name: "repo1"})
	pr := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{BaseRepoID: baseRepo.ID, Index: index})
	return pr
}

func listMergeHeadSHA(t *testing.T, pr *issues_model.PullRequest) string {
	t.Helper()
	pr.Issue = unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: pr.IssueID})
	pr.Issue.Repo = unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: pr.BaseRepoID})
	require.NoError(t, pr.LoadHeadRepo(t.Context()))
	repo, err := gitrepo.OpenRepository(db.DefaultContext, pr.HeadRepo)
	require.NoError(t, err)
	defer repo.Close()

	sha, err := repo.GetBranchCommitID(pr.HeadBranch)
	require.NoError(t, err)
	return sha
}

func postListMerge(t *testing.T, session *TestSession, owner, repo string, index int64, values map[string]string, expectedStatus int) (listMergeResponse, *httptest.ResponseRecorder) {
	t.Helper()
	request := NewRequestWithValues(t, http.MethodPost,
		fmt.Sprintf("/%s/%s/pulls/%d/merge-from-list", owner, repo, index), values)
	response := session.MakeRequest(t, request, expectedStatus)
	result := listMergeResponse{}
	if expectedStatus != http.StatusNotFound {
		DecodeJSON(t, response, &result)
	}
	return result, response
}

func listMergeValues(pr *issues_model.PullRequest, sha string, style repo_model.MergeStyle) map[string]string {
	return map[string]string{
		"do":             string(style),
		"head_commit_id": sha,
		"base_branch":    pr.BaseBranch,
	}
}

func allowListMergeStyle(t *testing.T, style repo_model.MergeStyle) {
	t.Helper()
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{OwnerName: "user2", Name: "repo1"})
	pullUnit := unittest.AssertExistsAndLoadBean(t, &repo_model.RepoUnit{RepoID: repo.ID, Type: unit_model.TypePullRequests})
	config := pullUnit.PullRequestsConfig()
	switch style {
	case repo_model.MergeStyleFastForwardOnly:
		config.AllowFastForwardOnly = true
	default:
		t.Fatalf("unsupported test merge-style override: %s", style)
	}
	pullUnit.Config = config
	require.NoError(t, repo_model.UpdateRepoUnit(t.Context(), pullUnit))
}

func TestPullListMergeSuccessAndDeleteBranch(t *testing.T) {
	onApplicationRun(t, func(t *testing.T, _ *url.URL) {
		session := loginUser(t, "user1")
		pr := listMergePR(t, session, "list-merge-delete", "list merge delete")
		sha := listMergeHeadSHA(t, pr)

		values := listMergeValues(pr, sha, repo_model.MergeStyleMerge)
		values["delete_branch_after_merge"] = "on"
		result, _ := postListMerge(t, session, "user2", "repo1", pr.Index, values, http.StatusOK)
		assert.True(t, result.Merged)
		assert.NotEmpty(t, result.Message)

		merged := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: pr.ID})
		assert.True(t, merged.HasMerged)
		fork := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{OwnerName: "user1", Name: "repo1"})
		gitRepo, err := gitrepo.OpenRepository(db.DefaultContext, fork)
		require.NoError(t, err)
		defer gitRepo.Close()
		_, err = gitRepo.GetBranch(pr.HeadBranch)
		require.Error(t, err)
		assert.True(t, git.IsErrBranchNotExist(err))
	})
}

func TestPullListMergeAllowedStyles(t *testing.T) {
	for _, style := range []repo_model.MergeStyle{
		repo_model.MergeStyleMerge,
		repo_model.MergeStyleRebase,
		repo_model.MergeStyleRebaseMerge,
		repo_model.MergeStyleSquash,
		repo_model.MergeStyleFastForwardOnly,
	} {
		t.Run(string(style), func(t *testing.T) {
			onApplicationRun(t, func(t *testing.T, _ *url.URL) {
				if style == repo_model.MergeStyleFastForwardOnly {
					allowListMergeStyle(t, style)
				}
				session := loginUser(t, "user1")
				branch := "list-merge-" + string(style)
				pr := listMergePR(t, session, branch, "list merge "+string(style))
				sha := listMergeHeadSHA(t, pr)
				result, _ := postListMerge(t, session, "user2", "repo1", pr.Index,
					listMergeValues(pr, sha, style), http.StatusOK)
				assert.True(t, result.Merged)
			})
		})
	}
}

func TestPullListMergeDefaultStyle(t *testing.T) {
	onApplicationRun(t, func(t *testing.T, _ *url.URL) {
		session := loginUser(t, "user1")
		pr := listMergePR(t, session, "list-merge-default", "list merge default")
		values := listMergeValues(pr, listMergeHeadSHA(t, pr), repo_model.MergeStyleMerge)
		values["do"] = "default"
		result, _ := postListMerge(t, session, "user2", "repo1", pr.Index, values, http.StatusOK)
		assert.True(t, result.Merged)
	})
}

func TestPullListMergeValidationAndAuthorization(t *testing.T) {
	onApplicationRun(t, func(t *testing.T, _ *url.URL) {
		session := loginUser(t, "user1")
		pr := listMergePR(t, session, "list-merge-validation", "list merge validation")
		sha := listMergeHeadSHA(t, pr)

		postListMerge(t, session, "user2", "repo1", pr.Index, map[string]string{
			"do": string(repo_model.MergeStyleMerge),
		}, http.StatusBadRequest)
		manualValues := listMergeValues(pr, sha, repo_model.MergeStyleManuallyMerged)
		manualResult, _ := postListMerge(t, session, "user2", "repo1", pr.Index, manualValues, http.StatusBadRequest)
		assert.False(t, manualResult.Merged)
		postListMerge(t, session, "user2", "repo1", 999999, listMergeValues(pr, sha, repo_model.MergeStyleMerge), http.StatusNotFound)

		unauthorized := loginUser(t, "user4")
		postListMerge(t, unauthorized, "user2", "repo1", pr.Index, listMergeValues(pr, sha, repo_model.MergeStyleMerge), http.StatusForbidden)

		stale := sha[:len(sha)-1] + "0"
		if stale == sha {
			stale = sha[:len(sha)-1] + "1"
		}
		staleResult, _ := postListMerge(t, session, "user2", "repo1", pr.Index, listMergeValues(pr, stale, repo_model.MergeStyleMerge), http.StatusConflict)
		assert.False(t, staleResult.Merged)
		retargetValues := listMergeValues(pr, sha, repo_model.MergeStyleMerge)
		retargetValues["base_branch"] = "retargeted"
		retargetResult, _ := postListMerge(t, session, "user2", "repo1", pr.Index, retargetValues, http.StatusConflict)
		assert.False(t, retargetResult.Merged)
		unchanged := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: pr.ID})
		assert.False(t, unchanged.HasMerged)
	})
}

func TestPullListMergeAlreadyMerged(t *testing.T) {
	onApplicationRun(t, func(t *testing.T, _ *url.URL) {
		session := loginUser(t, "user1")
		pr := listMergePR(t, session, "list-merge-already", "list merge already")
		sha := listMergeHeadSHA(t, pr)
		values := listMergeValues(pr, sha, repo_model.MergeStyleMerge)
		result, _ := postListMerge(t, session, "user2", "repo1", pr.Index, values, http.StatusOK)
		assert.True(t, result.Merged)

		result, _ = postListMerge(t, session, "user2", "repo1", pr.Index, values, http.StatusConflict)
		assert.False(t, result.Merged)
		merged := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: pr.ID})
		assert.True(t, merged.HasMerged)
	})
}

func TestPullListMergeRequiredStatusCheck(t *testing.T) {
	onApplicationRun(t, func(t *testing.T, _ *url.URL) {
		baseContext := NewAPITestContext(t, "user2", "repo1", auth_model.AccessTokenScopeWriteRepository, auth_model.AccessTokenScopeWriteUser)
		doProtectBranch(baseContext, "master", parameterProtectBranch{
			"enable_push":           "true",
			"enable_status_check":   "true",
			"status_check_contexts": "list-merge-required",
		})(t)

		session := loginUser(t, "user1")
		pr := listMergePR(t, session, "list-merge-required-status", "list merge required status")
		values := listMergeValues(pr, listMergeHeadSHA(t, pr), repo_model.MergeStyleMerge)
		values["force_merge"] = "true"
		values["merge_when_checks_succeed"] = "true"
		result, _ := postListMerge(t, session, "user2", "repo1", pr.Index, values, http.StatusConflict)
		assert.False(t, result.Merged)
		unchanged := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: pr.ID})
		assert.False(t, unchanged.HasMerged)
	})
}

func TestPullListMergeRequiredApproval(t *testing.T) {
	onApplicationRun(t, func(t *testing.T, _ *url.URL) {
		baseContext := NewAPITestContext(t, "user2", "repo1", auth_model.AccessTokenScopeWriteRepository, auth_model.AccessTokenScopeWriteUser)
		doProtectBranch(baseContext, "master", parameterProtectBranch{
			"enable_push":        "true",
			"required_approvals": "1",
		})(t)

		session := loginUser(t, "user1")
		pr := listMergePR(t, session, "list-merge-required-approval", "list merge required approval")
		values := listMergeValues(pr, listMergeHeadSHA(t, pr), repo_model.MergeStyleMerge)
		values["force_merge"] = "true"
		values["merge_when_checks_succeed"] = "true"
		result, _ := postListMerge(t, session, "user2", "repo1", pr.Index, values, http.StatusConflict)
		assert.False(t, result.Merged)
		unchanged := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: pr.ID})
		assert.False(t, unchanged.HasMerged)
	})
}

func TestPullListMergeBlockOutdatedBranch(t *testing.T) {
	onApplicationRun(t, func(t *testing.T, _ *url.URL) {
		baseContext := NewAPITestContext(t, "user2", "repo1", auth_model.AccessTokenScopeWriteRepository, auth_model.AccessTokenScopeWriteUser)
		doProtectBranch(baseContext, "master", parameterProtectBranch{
			"enable_push":              "true",
			"block_on_outdated_branch": "true",
		})(t)

		session := loginUser(t, "user1")
		testRepoFork(t, session, "user2", "repo1", "user1", "repo1")
		testNewFileToNewBranch(t, session, "user1", "repo1", "master", "list-merge-outdated-first", "first.txt", "first\n")
		testNewFileToNewBranch(t, session, "user1", "repo1", "master", "list-merge-outdated-second", "second.txt", "second\n")
		testPullCreate(t, session, "user1", "repo1", false, "master", "list-merge-outdated-first", "first non-conflicting merge")
		testPullCreate(t, session, "user1", "repo1", false, "master", "list-merge-outdated-second", "second non-conflicting merge")

		baseRepo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{OwnerName: "user2", Name: "repo1"})
		first := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{BaseRepoID: baseRepo.ID, HeadBranch: "list-merge-outdated-first"})
		second := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{BaseRepoID: baseRepo.ID, HeadBranch: "list-merge-outdated-second"})
		firstResult, _ := postListMerge(t, session, "user2", "repo1", first.Index, listMergeValues(first, listMergeHeadSHA(t, first), repo_model.MergeStyleMerge), http.StatusOK)
		assert.True(t, firstResult.Merged)
		secondResult, _ := postListMerge(t, session, "user2", "repo1", second.Index, listMergeValues(second, listMergeHeadSHA(t, second), repo_model.MergeStyleMerge), http.StatusConflict)
		assert.False(t, secondResult.Merged)
		unchanged := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: second.ID})
		assert.False(t, unchanged.HasMerged)
	})
}

func TestPullListMergeFastForwardOnlyAfterBaseAdvance(t *testing.T) {
	onApplicationRun(t, func(t *testing.T, _ *url.URL) {
		allowListMergeStyle(t, repo_model.MergeStyleFastForwardOnly)
		session := loginUser(t, "user1")
		testRepoFork(t, session, "user2", "repo1", "user1", "repo1")
		testNewFileToNewBranch(t, session, "user1", "repo1", "master", "list-merge-ff-first", "first.txt", "first\n")
		testNewFileToNewBranch(t, session, "user1", "repo1", "master", "list-merge-ff-second", "second.txt", "second\n")
		testPullCreate(t, session, "user1", "repo1", false, "master", "list-merge-ff-first", "first fast-forward merge")
		testPullCreate(t, session, "user1", "repo1", false, "master", "list-merge-ff-second", "second fast-forward merge")

		baseRepo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{OwnerName: "user2", Name: "repo1"})
		first := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{BaseRepoID: baseRepo.ID, HeadBranch: "list-merge-ff-first"})
		second := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{BaseRepoID: baseRepo.ID, HeadBranch: "list-merge-ff-second"})
		firstResult, _ := postListMerge(t, session, "user2", "repo1", first.Index, listMergeValues(first, listMergeHeadSHA(t, first), repo_model.MergeStyleFastForwardOnly), http.StatusOK)
		assert.True(t, firstResult.Merged)
		secondResult, _ := postListMerge(t, session, "user2", "repo1", second.Index, listMergeValues(second, listMergeHeadSHA(t, second), repo_model.MergeStyleFastForwardOnly), http.StatusConflict)
		assert.False(t, secondResult.Merged)
		unchanged := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: second.ID})
		assert.False(t, unchanged.HasMerged)
	})
}

func TestPullListMergeDraft(t *testing.T) {
	onApplicationRun(t, func(t *testing.T, _ *url.URL) {
		session := loginUser(t, "user1")
		draft := listMergePR(t, session, "list-merge-draft", "[WIP] list merge draft")
		draftSHA := listMergeHeadSHA(t, draft)
		result, _ := postListMerge(t, session, "user2", "repo1", draft.Index, listMergeValues(draft, draftSHA, repo_model.MergeStyleMerge), http.StatusConflict)
		assert.False(t, result.Merged)
		unchanged := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: draft.ID})
		assert.False(t, unchanged.HasMerged)
	})
}

func TestPullListMergeClosed(t *testing.T) {
	onApplicationRun(t, func(t *testing.T, _ *url.URL) {
		session := loginUser(t, "user1")
		closed := listMergePR(t, session, "list-merge-closed", "list merge closed")
		closedSHA := listMergeHeadSHA(t, closed)
		_, err := db.GetEngine(db.DefaultContext).ID(closed.IssueID).Cols("is_closed").Update(&issues_model.Issue{IsClosed: true})
		require.NoError(t, err)
		result, _ := postListMerge(t, session, "user2", "repo1", closed.Index, listMergeValues(closed, closedSHA, repo_model.MergeStyleMerge), http.StatusConflict)
		assert.False(t, result.Merged)
		unchanged := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: closed.ID})
		assert.False(t, unchanged.HasMerged)
	})
}

func TestPullListMergeSequentialConflict(t *testing.T) {
	onApplicationRun(t, func(t *testing.T, _ *url.URL) {
		session := loginUser(t, "user1")
		testRepoFork(t, session, "user2", "repo1", "user1", "repo1")
		testEditFileToNewBranch(t, session, "user1", "repo1", "master", "list-merge-first", "README.md", "first change\n")
		testEditFileToNewBranch(t, session, "user1", "repo1", "master", "list-merge-second", "README.md", "second change\n")
		testPullCreate(t, session, "user1", "repo1", false, "master", "list-merge-first", "first list merge")
		testPullCreate(t, session, "user1", "repo1", false, "master", "list-merge-second", "second list merge")

		baseRepo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{OwnerName: "user2", Name: "repo1"})
		firstPR := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{BaseRepoID: baseRepo.ID, HeadBranch: "list-merge-first"})
		secondPR := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{BaseRepoID: baseRepo.ID, HeadBranch: "list-merge-second"})
		firstSHA := listMergeHeadSHA(t, firstPR)
		secondSHA := listMergeHeadSHA(t, secondPR)
		firstResult, _ := postListMerge(t, session, "user2", "repo1", firstPR.Index, listMergeValues(firstPR, firstSHA, repo_model.MergeStyleMerge), http.StatusOK)
		assert.True(t, firstResult.Merged)
		secondResult, _ := postListMerge(t, session, "user2", "repo1", secondPR.Index, listMergeValues(secondPR, secondSHA, repo_model.MergeStyleMerge), http.StatusConflict)
		assert.False(t, secondResult.Merged)
		unchanged := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: secondPR.ID})
		assert.False(t, unchanged.HasMerged)
	})
}

func TestPullListMergeDeletionWarning(t *testing.T) {
	onApplicationRun(t, func(t *testing.T, _ *url.URL) {
		baseSession := loginUser(t, "user2")
		mergeSession := loginUser(t, "user4")
		testRepoFork(t, mergeSession, "user2", "repo1", "user4", "repo1")
		testEditFileToNewBranch(t, baseSession, "user2", "repo1", "master", "list-merge-no-delete", "README.md", "no delete change\n")
		created := testPullCreateDirectly(t, mergeSession, "user4", "repo1", "master", "user2", "repo1", "list-merge-no-delete", "list merge no delete")
		redirect := strings.Trim(test.RedirectURL(created), "/")
		parts := strings.Split(redirect, "/")
		index, err := strconv.ParseInt(parts[len(parts)-1], 10, 64)
		require.NoError(t, err)
		baseRepo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{OwnerName: "user4", Name: "repo1"})
		pr := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{BaseRepoID: baseRepo.ID, Index: index})
		sha := listMergeHeadSHA(t, pr)

		values := listMergeValues(pr, sha, repo_model.MergeStyleMerge)
		values["delete_branch_after_merge"] = "on"
		result, _ := postListMerge(t, mergeSession, "user4", "repo1", pr.Index, values, http.StatusOK)
		assert.True(t, result.Merged)
		assert.NotEmpty(t, result.Warning)

		headRepo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{OwnerName: "user2", Name: "repo1"})
		gitRepo, err := gitrepo.OpenRepository(db.DefaultContext, headRepo)
		require.NoError(t, err)
		defer gitRepo.Close()
		_, err = gitRepo.GetBranch(pr.HeadBranch)
		require.NoError(t, err)
	})
}

func TestPullListMergeSelectionMarkup(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	session := loginUser(t, "user2")

	response := session.MakeRequest(t, NewRequest(t, http.MethodGet, "/user2/repo1/pulls"), http.StatusOK)
	html := NewHTMLParser(t, response.Body)
	assert.NotEmpty(t, html.doc.Find(".issue-checkbox[data-pull-merge-url]").Length())

	response = session.MakeRequest(t, NewRequest(t, http.MethodGet, "/user2/repo1/issues"), http.StatusOK)
	html = NewHTMLParser(t, response.Body)
	assert.Zero(t, html.doc.Find(".issue-checkbox[data-pull-merge-url]").Length())

	response = session.MakeRequest(t, NewRequest(t, http.MethodGet, "/pulls"), http.StatusOK)
	html = NewHTMLParser(t, response.Body)
	assert.NotEmpty(t, html.doc.Find(".issue-checkbox[data-pull-merge-url]").Length())
}
