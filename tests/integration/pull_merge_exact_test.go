// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"forgejo.org/models"
	auth_model "forgejo.org/models/auth"
	git_model "forgejo.org/models/git"
	issues_model "forgejo.org/models/issues"
	pull_model "forgejo.org/models/pull"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"
	api "forgejo.org/modules/structs"
	"forgejo.org/services/pull"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPullMergeExact exercises the direct exact fast-forward-only merge in the
// shipping merge owner: success binds the expected merge SHA, stale/missing
// head/base, no-op, divergence and cross-repo inputs refuse, native
// protection refusals re-evaluate across a ProtectionChange restart, and
// merge-conflict interference blocks through the machine's own mergeability
// check. The indirect scheduled auto-merge path stays untouched throughout.
func TestPullMergeExact(t *testing.T) {
	onApplicationRun(t, func(t *testing.T, giteaURL *url.URL) {
		session := loginUser(t, "user1")
		testRepoFork(t, session, "user2", "repo1", "user1", "repo1")
		token := getTokenForLoggedInUser(t, session, auth_model.AccessTokenScopeWriteRepository)

		apiCreatePR := func(t *testing.T, owner, repo, head, base, title string) {
			t.Helper()
			req := NewRequestWithJSON(t, http.MethodPost, fmt.Sprintf("/api/v1/repos/%s/%s/pulls", owner, repo), &api.CreatePullRequestOption{
				Head:  head,
				Base:  base,
				Title: title,
			}).AddTokenAuth(token)
			session.MakeRequest(t, req, http.StatusCreated)
		}

		loadRepo := func(t *testing.T, ownerID int64, name string) (*user_model.User, *repo_model.Repository) {
			t.Helper()
			user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: ownerID})
			repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{OwnerID: ownerID, Name: name})
			return user, repo
		}

		loadPR := func(t *testing.T, headRepoID, baseRepoID int64, head, base string) *issues_model.PullRequest {
			t.Helper()
			return unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{
				HeadRepoID: headRepoID,
				BaseRepoID: baseRepoID,
				HeadBranch: head,
				BaseBranch: base,
			})
		}

		// refreshMergeability reproduces the machine's own mergeability state
		// synchronously: compute with TestPatch, then persist the same
		// columns the patch-check queue persists. This removes the async
		// queue race without substituting a second computation.
		refreshMergeability := func(t *testing.T, pr *issues_model.PullRequest) *issues_model.PullRequest {
			t.Helper()
			require.NoError(t, pull.TestPatch(pr))
			require.NoError(t, pr.UpdateColsIfNotMerged(t.Context(), "merge_base", "status", "conflicted_files", "changed_protected_files"))
			return unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: pr.ID})
		}

		user1, repo1 := loadRepo(t, 1, "repo1")

		t.Run("ValidBindsExpectedMergeSHA", func(t *testing.T) {
			testEditFileToNewBranch(t, session, "user1", "repo1", "master", "exact-ok", "README.md", "Hello, World exact\n")
			apiCreatePR(t, "user1", "repo1", "exact-ok", "master", "exact merge success")

			pr := refreshMergeability(t, loadPR(t, repo1.ID, repo1.ID, "exact-ok", "master"))
			require.Equal(t, issues_model.PullRequestStatusMergeable, pr.Status)

			gitRepo, err := git.OpenRepository(git.DefaultContext, repo_model.RepoPath(user1.Name, repo1.Name))
			require.NoError(t, err)
			defer gitRepo.Close()

			headOID, err := gitRepo.GetRefCommitID("refs/heads/exact-ok")
			require.NoError(t, err)
			baseOID, err := gitRepo.GetRefCommitID("refs/heads/master")
			require.NoError(t, err)
			require.NotEqual(t, headOID, baseOID)

			message, _, err := pull.GetDefaultMergeMessage(t.Context(), gitRepo, pr, repo_model.MergeStyleFastForwardOnly)
			require.NoError(t, err)

			realized, err := pull.MergeExactFastForward(t.Context(), pr, user1, gitRepo,
				"refs/heads/exact-ok", "refs/heads/master", headOID, baseOID, message)
			require.NoError(t, err)
			require.Equal(t, strings.ToLower(headOID), strings.ToLower(realized))

			tip, err := gitRepo.GetRefCommitID("refs/heads/master")
			require.NoError(t, err)
			require.Equal(t, strings.ToLower(headOID), strings.ToLower(tip))

			merged := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: pr.ID})
			require.True(t, merged.HasMerged)
			require.Equal(t, strings.ToLower(headOID), strings.ToLower(merged.MergedCommitID))

			// Direct path only: no scheduled auto-merge row is created or
			// consumed. Manual marking is excluded structurally: this entry
			// never calls MergedManually, and the merge above committed
			// through the direct engine with post-receive bookkeeping.
			exists, _, err := pull_model.GetScheduledMergeByPullID(t.Context(), pr.ID)
			require.NoError(t, err)
			require.False(t, exists)
		})

		t.Run("StaleHeadRefuses", func(t *testing.T) {
			testEditFileToNewBranch(t, session, "user1", "repo1", "master", "exact-stale-head", "README.md", "Hello, World stale-head\n")
			apiCreatePR(t, "user1", "repo1", "exact-stale-head", "master", "exact merge stale head")

			pr := refreshMergeability(t, loadPR(t, repo1.ID, repo1.ID, "exact-stale-head", "master"))

			gitRepo, err := git.OpenRepository(git.DefaultContext, repo_model.RepoPath(user1.Name, repo1.Name))
			require.NoError(t, err)
			defer gitRepo.Close()

			headOID, err := gitRepo.GetRefCommitID("refs/heads/exact-stale-head")
			require.NoError(t, err)
			baseOID, err := gitRepo.GetRefCommitID("refs/heads/master")
			require.NoError(t, err)

			// Competing writer advances the head after the intent was bound.
			testEditFile(t, session, "user1", "repo1", "exact-stale-head", "README.md", "Hello, World stale-head moved\n")
			movedOID, err := gitRepo.GetRefCommitID("refs/heads/exact-stale-head")
			require.NoError(t, err)
			require.NotEqual(t, headOID, movedOID)

			_, err = pull.MergeExactFastForward(t.Context(), pr, user1, gitRepo,
				"refs/heads/exact-stale-head", "refs/heads/master", headOID, baseOID, "STALE-HEAD")
			require.Error(t, err)
			require.True(t, pull.IsErrExactMergeRefused(err))
			require.Equal(t, pull.ExactMergeRefusedStaleHead, err.(pull.ErrExactMergeRefused).Reason)

			tip, err := gitRepo.GetRefCommitID("refs/heads/master")
			require.NoError(t, err)
			require.Equal(t, strings.ToLower(baseOID), strings.ToLower(tip))
			unmerged := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: pr.ID})
			require.False(t, unmerged.HasMerged)
		})

		t.Run("StaleBaseRefuses", func(t *testing.T) {
			testEditFileToNewBranch(t, session, "user1", "repo1", "master", "exact-stale-base", "README.md", "Hello, World stale-base\n")
			apiCreatePR(t, "user1", "repo1", "exact-stale-base", "master", "exact merge stale base")

			pr := refreshMergeability(t, loadPR(t, repo1.ID, repo1.ID, "exact-stale-base", "master"))

			gitRepo, err := git.OpenRepository(git.DefaultContext, repo_model.RepoPath(user1.Name, repo1.Name))
			require.NoError(t, err)
			defer gitRepo.Close()

			headOID, err := gitRepo.GetRefCommitID("refs/heads/exact-stale-base")
			require.NoError(t, err)
			baseOID, err := gitRepo.GetRefCommitID("refs/heads/master")
			require.NoError(t, err)

			// Competing writer advances the base after the intent was bound.
			testEditFile(t, session, "user1", "repo1", "master", "README.md", "Hello, World stale-base moved\n")

			_, err = pull.MergeExactFastForward(t.Context(), pr, user1, gitRepo,
				"refs/heads/exact-stale-base", "refs/heads/master", headOID, baseOID, "STALE-BASE")
			require.Error(t, err)
			require.True(t, pull.IsErrExactMergeRefused(err))
			require.Equal(t, pull.ExactMergeRefusedStaleBase, err.(pull.ErrExactMergeRefused).Reason)

			unmerged := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: pr.ID})
			require.False(t, unmerged.HasMerged)
		})

		t.Run("NoOpRefuses", func(t *testing.T) {
			testEditFileToNewBranch(t, session, "user1", "repo1", "master", "exact-noop", "README.md", "Hello, World noop\n")
			apiCreatePR(t, "user1", "repo1", "exact-noop", "master", "exact merge no-op")

			pr := refreshMergeability(t, loadPR(t, repo1.ID, repo1.ID, "exact-noop", "master"))

			gitRepo, err := git.OpenRepository(git.DefaultContext, repo_model.RepoPath(user1.Name, repo1.Name))
			require.NoError(t, err)
			defer gitRepo.Close()

			headOID, err := gitRepo.GetRefCommitID("refs/heads/exact-noop")
			require.NoError(t, err)

			_, err = pull.MergeExactFastForward(t.Context(), pr, user1, gitRepo,
				"refs/heads/exact-noop", "refs/heads/master", headOID, headOID, "NOOP")
			require.Error(t, err)
			require.True(t, pull.IsErrExactMergeRefused(err))
			require.Equal(t, pull.ExactMergeRefusedNoOp, err.(pull.ErrExactMergeRefused).Reason)

			unmerged := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: pr.ID})
			require.False(t, unmerged.HasMerged)
		})

		t.Run("DivergingRefusesThroughNativeEngine", func(t *testing.T) {
			testEditFile(t, session, "user1", "repo1", "master", "README.md", "line1\nline2\nline3\nline4\nline5\nline6\n")
			testEditFileToNewBranch(t, session, "user1", "repo1", "master", "exact-diverging", "README.md", "line1\nLINE2\nline3\nline4\nline5\nline6\n")
			testEditFile(t, session, "user1", "repo1", "master", "README.md", "line1\nline2\nline3\nline4\nLINE5\nline6\n")
			apiCreatePR(t, "user1", "repo1", "exact-diverging", "master", "exact merge diverging")

			pr := refreshMergeability(t, loadPR(t, repo1.ID, repo1.ID, "exact-diverging", "master"))
			// Disjoint edits stay three-way mergeable; only the
			// fast-forward-only engine refuses the divergence.
			require.Equal(t, issues_model.PullRequestStatusMergeable, pr.Status)

			gitRepo, err := git.OpenRepository(git.DefaultContext, repo_model.RepoPath(user1.Name, repo1.Name))
			require.NoError(t, err)
			defer gitRepo.Close()

			headOID, err := gitRepo.GetRefCommitID("refs/heads/exact-diverging")
			require.NoError(t, err)
			baseOID, err := gitRepo.GetRefCommitID("refs/heads/master")
			require.NoError(t, err)

			_, err = pull.MergeExactFastForward(t.Context(), pr, user1, gitRepo,
				"refs/heads/exact-diverging", "refs/heads/master", headOID, baseOID, "DIVERGING")
			require.Error(t, err)
			assert.True(t, models.IsErrMergeDivergingFastForwardOnly(err), "expected native diverging error, got %v", err)
		})

		t.Run("ProtectionChangeRestartsMergeability", func(t *testing.T) {
			testEditFileToNewBranch(t, session, "user1", "repo1", "master", "exact-protected", "README.md", "Hello, World protected\n")
			apiCreatePR(t, "user1", "repo1", "exact-protected", "master", "exact merge protection")

			pr := refreshMergeability(t, loadPR(t, repo1.ID, repo1.ID, "exact-protected", "master"))

			gitRepo, err := git.OpenRepository(git.DefaultContext, repo_model.RepoPath(user1.Name, repo1.Name))
			require.NoError(t, err)
			defer gitRepo.Close()

			headOID, err := gitRepo.GetRefCommitID("refs/heads/exact-protected")
			require.NoError(t, err)
			baseOID, err := gitRepo.GetRefCommitID("refs/heads/master")
			require.NoError(t, err)

			// ProtectionChange: require one approval on the base branch.
			protectReq := NewRequestWithJSON(t, http.MethodPost, "/api/v1/repos/user1/repo1/branch_protections", &api.BranchProtection{
				RuleName:          "master",
				RequiredApprovals: 1,
				ApplyToAdmins:     true,
			}).AddTokenAuth(token)
			session.MakeRequest(t, protectReq, http.StatusCreated)

			rule, err := git_model.GetFirstMatchProtectedBranchRule(t.Context(), repo1.ID, "master")
			require.NoError(t, err)
			require.NotNil(t, rule)
			require.Equal(t, int64(1), rule.RequiredApprovals)

			_, err = pull.MergeExactFastForward(t.Context(), pr, user1, gitRepo,
				"refs/heads/exact-protected", "refs/heads/master", headOID, baseOID, "PROTECTED")
			require.Error(t, err)
			assert.True(t, models.IsErrDisallowedToMerge(err), "expected native protection refusal, got %v", err)

			// Restart after the protection is withdrawn: the same exact
			// intent re-evaluates against current policy and merges.
			deleteReq := NewRequestf(t, http.MethodDelete, "/api/v1/repos/user1/repo1/branch_protections/%s", "master").
				AddTokenAuth(token)
			session.MakeRequest(t, deleteReq, http.StatusNoContent)

			rule, err = git_model.GetFirstMatchProtectedBranchRule(t.Context(), repo1.ID, "master")
			require.NoError(t, err)
			require.Nil(t, rule)

			pr = refreshMergeability(t, loadPR(t, repo1.ID, repo1.ID, "exact-protected", "master"))
			headOID, err = gitRepo.GetRefCommitID("refs/heads/exact-protected")
			require.NoError(t, err)
			baseOID, err = gitRepo.GetRefCommitID("refs/heads/master")
			require.NoError(t, err)

			realized, err := pull.MergeExactFastForward(t.Context(), pr, user1, gitRepo,
				"refs/heads/exact-protected", "refs/heads/master", headOID, baseOID, "UNPROTECTED")
			require.NoError(t, err)
			require.Equal(t, strings.ToLower(headOID), strings.ToLower(realized))
		})

		t.Run("ConflictInterferenceBlocks", func(t *testing.T) {
			testEditFileToNewBranch(t, session, "user1", "repo1", "master", "exact-conflict-head", "README.md", "Hello, World conflict head\n")
			testEditFileToNewBranch(t, session, "user1", "repo1", "master", "exact-conflict-base", "README.md", "Hello, World conflict base\n")
			apiCreatePR(t, "user1", "repo1", "exact-conflict-head", "exact-conflict-base", "exact merge conflict")

			pr := refreshMergeability(t, loadPR(t, repo1.ID, repo1.ID, "exact-conflict-head", "exact-conflict-base"))
			require.Equal(t, issues_model.PullRequestStatusConflict, pr.Status)

			gitRepo, err := git.OpenRepository(git.DefaultContext, repo_model.RepoPath(user1.Name, repo1.Name))
			require.NoError(t, err)
			defer gitRepo.Close()

			headOID, err := gitRepo.GetRefCommitID("refs/heads/exact-conflict-head")
			require.NoError(t, err)
			baseOID, err := gitRepo.GetRefCommitID("refs/heads/exact-conflict-base")
			require.NoError(t, err)

			_, err = pull.MergeExactFastForward(t.Context(), pr, user1, gitRepo,
				"refs/heads/exact-conflict-head", "refs/heads/exact-conflict-base", headOID, baseOID, "CONFLICT")
			require.Error(t, err)
			assert.ErrorIs(t, err, pull.ErrNotMergeableState)
		})

		t.Run("CrossRepoRefuses", func(t *testing.T) {
			testEditFileToNewBranch(t, session, "user1", "repo1", "master", "exact-xrepo", "README.md", "Hello, World xrepo\n")
			apiCreatePR(t, "user2", "repo1", "user1:exact-xrepo", "master", "exact merge cross repo")

			user2, repo2 := loadRepo(t, 2, "repo1")
			pr := loadPR(t, repo1.ID, repo2.ID, "exact-xrepo", "master")
			require.NotEqual(t, pr.HeadRepoID, pr.BaseRepoID)

			gitRepo, err := git.OpenRepository(git.DefaultContext, repo_model.RepoPath(user2.Name, repo2.Name))
			require.NoError(t, err)
			defer gitRepo.Close()

			headRepo, err := git.OpenRepository(git.DefaultContext, repo_model.RepoPath(user1.Name, repo1.Name))
			require.NoError(t, err)
			defer headRepo.Close()

			headOID, err := headRepo.GetRefCommitID("refs/heads/exact-xrepo")
			require.NoError(t, err)
			baseOID, err := gitRepo.GetRefCommitID("refs/heads/master")
			require.NoError(t, err)

			_, err = pull.MergeExactFastForward(t.Context(), pr, user1, gitRepo,
				"refs/heads/exact-xrepo", "refs/heads/master", headOID, baseOID, "XREPO")
			require.Error(t, err)
			require.True(t, pull.IsErrExactMergeRefused(err))
			require.Equal(t, pull.ExactMergeRefusedCrossRepo, err.(pull.ErrExactMergeRefused).Reason)
		})
	})
}
