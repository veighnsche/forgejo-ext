// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	auth_model "forgejo.org/models/auth"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"
	api "forgejo.org/modules/structs"
	operation_service "forgejo.org/services/nativeoperation"

	"github.com/stretchr/testify/require"
)

// TestPublishExact exercises the exact candidate-ref verification in the
// shipping repository owner: creation and update intents verify against live
// tips, fast-forward order and the comparison base, and every divergence
// (existing/missing branch, stale old tip, missing commit, non-fast-forward,
// missing/changed comparison, correction PR mismatch or closure) refuses with
// its stable reason. Nothing here performs a receive or holds a reservation.
func TestPublishExact(t *testing.T) {
	onApplicationRun(t, func(t *testing.T, giteaURL *url.URL) {
		session := loginUser(t, "user1")
		testRepoFork(t, session, "user2", "repo1", "user1", "repo1")
		token := getTokenForLoggedInUser(t, session, auth_model.AccessTokenScopeWriteRepository, auth_model.AccessTokenScopeWriteIssue)

		user1 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
		repo1 := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{OwnerID: 1, Name: "repo1"})

		gitRepo, err := git.OpenRepository(git.DefaultContext, repo_model.RepoPath(user1.Name, repo1.Name))
		require.NoError(t, err)
		defer gitRepo.Close()

		tip := func(t *testing.T, ref string) string {
			t.Helper()
			oid, err := gitRepo.GetRefCommitID(ref)
			require.NoError(t, err)
			return oid
		}

		verify := func(t *testing.T, payload string) (*operation_service.PublishTarget, error) {
			t.Helper()
			intent, err := operation_service.ParsePublishPayload([]byte(payload))
			require.NoError(t, err)
			return operation_service.VerifyExactPublishTarget(t.Context(), repo1, gitRepo, intent)
		}

		refuse := func(t *testing.T, payload, reason string) {
			t.Helper()
			_, err := verify(t, payload)
			require.Error(t, err)
			require.True(t, operation_service.IsErrExactPublishRefused(err), "got %v", err)
			require.Equal(t, reason, err.(operation_service.ErrExactPublishRefused).Reason)
		}

		updatePayload := func(ref, old, new, baseRef, base, correction string) string {
			payload := fmt.Sprintf(`{"ref":%q,"expected_old":%q,"new_oid":%q,`+
				`"comparison_ref":%q,"expected_comparison_oid":%q`, ref, old, new, baseRef, base)
			if correction != "" {
				payload += `,"pull_request":` + correction
			}
			return payload + `}`
		}

		// stageUpdate creates a target branch with one commit plus a staging
		// branch holding that commit's child. The child models a candidate
		// commit that exists but is not on the target yet, so an update
		// intent from the target tip to the child is a genuine advance.
		stageUpdate := func(t *testing.T, name string) (tipOID, nextOID string) {
			t.Helper()
			testEditFileToNewBranch(t, session, "user1", "repo1", "master", name, "README.md", "Hello, World "+name+"\n")
			tipOID = tip(t, "refs/heads/"+name)
			testEditFileToNewBranch(t, session, "user1", "repo1", name, name+"-next", "README.md", "Hello, World "+name+" next\n")
			nextOID = tip(t, "refs/heads/"+name+"-next")
			require.NotEqual(t, tipOID, nextOID)
			return tipOID, nextOID
		}

		master := func(t *testing.T) string {
			t.Helper()
			return tip(t, "refs/heads/master")
		}

		t.Run("ValidCreation", func(t *testing.T) {
			_, next := stageUpdate(t, "pub-seed")
			target, err := verify(t, updatePayload("refs/heads/pub-fresh", "absent", next, "refs/heads/master", master(t), ""))
			require.NoError(t, err)
			require.Equal(t, "refs/heads/pub-fresh", target.Ref)
			require.Equal(t, strings.Repeat("0", 40), target.OldOID)
			require.Equal(t, strings.ToLower(next), target.NewOID)
			require.Zero(t, target.PRID)
		})

		t.Run("ValidUpdate", func(t *testing.T) {
			old, next := stageUpdate(t, "pub-upd")
			target, err := verify(t, updatePayload("refs/heads/pub-upd", old, next, "refs/heads/master", master(t), ""))
			require.NoError(t, err)
			require.Equal(t, strings.ToLower(old), target.OldOID)
			require.Equal(t, strings.ToLower(next), target.NewOID)
		})

		t.Run("BranchExistsRefusesCreation", func(t *testing.T) {
			_, next := stageUpdate(t, "pub-exists")
			refuse(t, updatePayload("refs/heads/pub-exists", "absent", next, "refs/heads/master", master(t), ""),
				operation_service.ExactPublishRefusedBranchExists)
		})

		t.Run("MissingBranchRefusesUpdate", func(t *testing.T) {
			old, next := stageUpdate(t, "pub-src")
			refuse(t, updatePayload("refs/heads/pub-ghost", old, next, "refs/heads/master", master(t), ""),
				operation_service.ExactPublishRefusedMissingBranch)
		})

		t.Run("StaleOldRefuses", func(t *testing.T) {
			old, next := stageUpdate(t, "pub-stale")
			// A competing writer advances the branch after the intent bound
			// its old tip.
			testEditFile(t, session, "user1", "repo1", "pub-stale", "README.md", "Hello, World pub-stale moved\n")
			refuse(t, updatePayload("refs/heads/pub-stale", old, next, "refs/heads/master", master(t), ""),
				operation_service.ExactPublishRefusedStaleOld)
		})

		t.Run("NotFastForwardRefuses", func(t *testing.T) {
			old, _ := stageUpdate(t, "pub-ff")
			// The master tip is an ancestor of the branch tip, so pushing it
			// over the tip is a force push, never a fast-forward update.
			refuse(t, updatePayload("refs/heads/pub-ff", old, master(t), "refs/heads/master", master(t), ""),
				operation_service.ExactPublishRefusedNotFastForward)
		})

		t.Run("MissingCommitRefuses", func(t *testing.T) {
			old, _ := stageUpdate(t, "pub-miss")
			refuse(t, updatePayload("refs/heads/pub-miss", old, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", "refs/heads/master", master(t), ""),
				operation_service.ExactPublishRefusedMissingCommit)
		})

		t.Run("MissingComparisonRefuses", func(t *testing.T) {
			old, next := stageUpdate(t, "pub-nocomp")
			refuse(t, updatePayload("refs/heads/pub-nocomp", old, next, "refs/heads/pub-ghost", master(t), ""),
				operation_service.ExactPublishRefusedMissingCompare)
		})

		t.Run("StaleComparisonRefuses", func(t *testing.T) {
			old, next := stageUpdate(t, "pub-stalebase")
			bound := master(t)
			testEditFile(t, session, "user1", "repo1", "master", "README.md", "Hello, World master moved\n")
			require.NotEqual(t, bound, master(t))
			refuse(t, updatePayload("refs/heads/pub-stalebase", old, next, "refs/heads/master", bound, ""),
				operation_service.ExactPublishRefusedStaleCompare)
		})

		t.Run("Correction", func(t *testing.T) {
			old, next := stageUpdate(t, "pub-corr")
			base := master(t)
			createReq := NewRequestWithJSON(t, http.MethodPost, "/api/v1/repos/user1/repo1/pulls", &api.CreatePullRequestOption{
				Head:  "pub-corr",
				Base:  "master",
				Title: "publish correction",
			}).AddTokenAuth(token)
			session.MakeRequest(t, createReq, http.StatusCreated)
			pr := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{
				HeadRepoID: repo1.ID, BaseRepoID: repo1.ID, HeadBranch: "pub-corr", BaseBranch: "master",
			})

			binding := fmt.Sprintf(`{"number":%d,"expected_author_id":%d}`, pr.Index, user1.ID)
			target, err := verify(t, updatePayload("refs/heads/pub-corr", old, next, "refs/heads/master", base, binding))
			require.NoError(t, err)
			require.Equal(t, pr.ID, target.PRID)
			require.Equal(t, pr.IssueID, target.IssueID)

			wrongAuthor := fmt.Sprintf(`{"number":%d,"expected_author_id":%d}`, pr.Index, 2)
			refuse(t, updatePayload("refs/heads/pub-corr", old, next, "refs/heads/master", base, wrongAuthor),
				operation_service.ExactPublishRefusedPRMismatch)

			unknown := `{"number":9999,"expected_author_id":1}`
			refuse(t, updatePayload("refs/heads/pub-corr", old, next, "refs/heads/master", base, unknown),
				operation_service.ExactPublishRefusedMissingPR)

			closed := "closed"
			closeReq := NewRequestWithJSON(t, http.MethodPatch,
				fmt.Sprintf("/api/v1/repos/user1/repo1/issues/%d", pr.Index),
				&api.EditIssueOption{State: &closed}).AddTokenAuth(token)
			session.MakeRequest(t, closeReq, http.StatusCreated)
			refuse(t, updatePayload("refs/heads/pub-corr", old, next, "refs/heads/master", base, binding),
				operation_service.ExactPublishRefusedClosedPR)
		})
	})
}
