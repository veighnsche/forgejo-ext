// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	"forgejo.org/models/extensionauth"
	issues_model "forgejo.org/models/issues"
	nativeop_model "forgejo.org/models/nativeoperation"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"
	api "forgejo.org/modules/structs"
	operation_service "forgejo.org/services/nativeoperation"
	pull_service "forgejo.org/services/pull"

	"github.com/stretchr/testify/require"
)

// TestPullMergeSubmit is the authoritative F-merge proof: one conditional
// pull_request.merge drives an exact fast-forward-only native merge through
// the shipping owners, and every divergence refuses without guessing. Only
// the documented fast-forward-only method is exposed: diverging candidates
// refuse as not_fast_forward, and a repository policy that disallows the
// method refuses as a native refusal. Subtests merge one exact candidate
// (receipt + native rows + lost-reply Get), replay the same ID without a
// second effect, refuse stale/no-op/foreign intents, a diverged candidate, an
// active branch protection (then restart after withdrawal), a disallowed
// method, an already-merged PR and a foreign-kind credential, race
// cancellation against prepared admission, crash before and after the native
// merge and recover offline (clean, committed, and committed-but-incomplete),
// and record too_late withdrawal after commit.
//
// Disclosure: the cancel subtest uses the disclosed
// NATIVEOP_TEST_ADMISSION_BARRIER instrument. The crash subtests use the
// disclosed NATIVEOP_TEST_CRASH_POINT barrier instruments with a timeout:
// the timeout fails the submit in-process, which retains the identical
// held-owner state a SIGKILL would leave (no effect before the native merge;
// moved ref plus merged PR after it; held owner and kept capability in both
// cases); offline recovery then runs for real under inhibition. The
// incomplete-bookkeeping variant additionally resets the merged PR flags
// after the crash to simulate the real interleaving where the ref moved but
// the PR row never recorded it.
func TestPullMergeSubmit(t *testing.T) {
	onApplicationRun(t, func(t *testing.T, _ *url.URL) {
		session := loginUser(t, "user1")
		testRepoFork(t, session, "user2", "repo1", "user1", "repo1")
		token := getTokenForLoggedInUser(t, session, auth_model.AccessTokenScopeWriteRepository, auth_model.AccessTokenScopeWriteIssue)
		otherToken := getTokenForLoggedInUser(t, session, auth_model.AccessTokenScopeWriteRepository)

		user1 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
		repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{OwnerID: 1, Name: "repo1"})
		svc := operation_service.Default()
		ctx := t.Context()
		installation := "7c12e001-0000-4000-8000-000000000014"

		tokenRow, err := auth_model.GetAccessTokenBySHA(ctx, token)
		require.NoError(t, err)
		decision := extensionauth.SubmissionDecision{
			TokenID:               tokenRow.ID,
			ActorID:               user1.ID,
			CredentialFingerprint: extensionauth.CredentialFingerprint(tokenRow.TokenHash, tokenRow.TokenSalt),
		}
		_, err = extensionauth.EnrollBinding(ctx, installation, tokenRow.ID, user1.ID, repo.ID, extensionauth.KindMerge)
		require.NoError(t, err)

		otherTokenRow, err := auth_model.GetAccessTokenBySHA(ctx, otherToken)
		require.NoError(t, err)
		otherDecision := extensionauth.SubmissionDecision{
			TokenID:               otherTokenRow.ID,
			ActorID:               user1.ID,
			CredentialFingerprint: extensionauth.CredentialFingerprint(otherTokenRow.TokenHash, otherTokenRow.TokenSalt),
		}
		_, err = extensionauth.EnrollBinding(ctx, installation, otherTokenRow.ID, user1.ID, repo.ID, extensionauth.KindReviewSubmit)
		require.NoError(t, err)

		repoPath := repo_model.RepoPath(user1.Name, repo.Name)
		tip := func(t *testing.T, ref string) string {
			t.Helper()
			oid, err := git.GetFullCommitID(ctx, repoPath, ref)
			require.NoError(t, err, "fixture ref %s must resolve", ref)
			require.Len(t, oid, 40)
			return strings.ToLower(oid)
		}
		master := func(t *testing.T) string {
			t.Helper()
			return tip(t, "refs/heads/master")
		}

		// waitIdle polls for writer quiescence: an idle reservation with
		// a revision stable across two observations. Fixture setup and
		// submits both drain first: leftover async work fences setup
		// edits and invalidates bound revisions.
		waitIdle := func(t *testing.T) int64 {
			t.Helper()
			deadline := time.Now().Add(60 * time.Second)
			var last int64 = -1
			for time.Now().Before(deadline) {
				observation, err := svc.ReadNativeRevision(ctx)
				require.NoError(t, err)
				if observation.Idle && observation.Revision == last {
					return observation.Revision
				}
				last = observation.Revision
				time.Sleep(200 * time.Millisecond)
			}
			t.Fatal("native writer domain did not reach quiescence")
			return 0
		}

		validate := func(t *testing.T, opID, kind, payload string) operation_service.ValidIntent {
			t.Helper()
			revision := waitIdle(t)
			now := time.Now().Unix()
			intent, err := operation_service.ValidateIntent(opID, user1.ID, repo.ID, kind,
				"auth-rev-1", revision, now+600, []byte(payload), now)
			require.NoError(t, err)
			return *intent
		}

		mergePayload := func(number int64, headRef, baseRef, headOID, baseOID string) string {
			return fmt.Sprintf(`{"pull_request_number":%d,"head_repository_id":%d,`+
				`"head_ref":%q,"base_ref":%q,`+
				`"expected_head_oid":%q,"expected_base_oid":%q,`+
				`"method":"fast-forward-only"}`,
				number, repo.ID, headRef, baseRef, headOID, baseOID)
		}

		newBranch := func(t *testing.T, branch, content string) {
			t.Helper()
			waitIdle(t)
			testEditFileToNewBranch(t, session, "user1", "repo1", "master", branch, "README.md", content)
		}
		editBranch := func(t *testing.T, branch, content string) {
			t.Helper()
			waitIdle(t)
			testEditFile(t, session, "user1", "repo1", branch, "README.md", content)
		}

		// Fixture PRs come from the ordinary native API, so this proof
		// depends on no other conditional implementation.
		apiCreatePR := func(t *testing.T, head, base, title string) (number, prID int64) {
			t.Helper()
			waitIdle(t)
			session.MakeRequest(t, NewRequestWithJSON(t, http.MethodPost,
				fmt.Sprintf("/api/v1/repos/%s/%s/pulls", repo.OwnerName, repo.Name),
				&api.CreatePullRequestOption{
					Head:  head,
					Base:  base,
					Title: title,
				}).AddTokenAuth(token), http.StatusCreated)
			pr := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{
				HeadRepoID: repo.ID, BaseRepoID: repo.ID, HeadBranch: head, BaseBranch: base,
			})
			return pr.Index, pr.ID
		}
		loadPR := func(t *testing.T, prID int64) *issues_model.PullRequest {
			t.Helper()
			return unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: prID})
		}

		// refreshMergeability reproduces the machine's own mergeability
		// state synchronously: compute with TestPatch, then persist
		// the same columns the patch-check queue persists. This
		// removes the async queue race without substituting a second
		// computation.
		refreshMergeability := func(t *testing.T, pr *issues_model.PullRequest) *issues_model.PullRequest {
			t.Helper()
			require.NoError(t, pull_service.TestPatch(pr))
			require.NoError(t, pr.UpdateColsIfNotMerged(ctx, "merge_base", "status", "conflicted_files", "changed_protected_files"))
			return unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: pr.ID})
		}

		prepareCandidate := func(t *testing.T, branch, title string) (number, prID int64) {
			t.Helper()
			newBranch(t, branch, "Hello, World "+branch+"\n")
			number, prID = apiCreatePR(t, branch, "master", title)
			pr := refreshMergeability(t, loadPR(t, prID))
			require.Equal(t, issues_model.PullRequestStatusMergeable, pr.Status)
			return number, prID
		}

		decodeReceipt := func(t *testing.T, raw json.RawMessage) operation_service.MergeReceipt {
			t.Helper()
			var receipt operation_service.MergeReceipt
			require.NoError(t, json.Unmarshal(raw, &receipt))
			return receipt
		}

		var successOpID string
		var successNumber, successPRID int64
		var successHeadOID string
		var successIntent operation_service.ValidIntent

		t.Run("success_exact", func(t *testing.T) {
			successOpID = "ft14-success"
			number, prID := prepareCandidate(t, "ft14-ok", "ft14 merge success")
			successNumber, successPRID = number, prID
			headOID := tip(t, "refs/heads/ft14-ok")
			baseOID := master(t)
			require.NotEqual(t, headOID, baseOID)
			successHeadOID = headOID

			intent := validate(t, successOpID, nativeop_model.KindMerge,
				mergePayload(number, "refs/heads/ft14-ok", "refs/heads/master", headOID, baseOID))
			successIntent = intent
			record, err := svc.Submit(ctx, decision, installation, &intent)
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectCommitted, record.Outcome)
			require.Equal(t, nativeop_model.CompletionComplete, record.CompletionState)
			receipt := decodeReceipt(t, record.Receipt)
			require.Equal(t, baseOID, receipt.OldOID)
			require.Equal(t, headOID, receipt.NewOID)
			require.Equal(t, user1.ID, receipt.ActorID)
			require.Equal(t, repo.ID, receipt.RepositoryID)
			require.Equal(t, number, receipt.PRNumber)
			require.Equal(t, prID, receipt.PRID)
			require.Equal(t, "refs/heads/ft14-ok", receipt.HeadRef)
			require.Equal(t, "refs/heads/master", receipt.BaseRef)
			require.Equal(t, "fast-forward-only", receipt.Method)

			require.Equal(t, headOID, tip(t, "refs/heads/master"))
			merged := loadPR(t, prID)
			require.True(t, merged.HasMerged)
			require.Equal(t, headOID, strings.ToLower(merged.MergedCommitID))

			// A lost reply reconciles through Get: same digest and
			// same receipt, never a replacement submit.
			lookup, err := svc.Get(ctx, installation, successOpID)
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectCommitted, lookup.Status)
			require.NotNil(t, lookup.Record)
			require.Equal(t, record.IntentDigest, lookup.Record.IntentDigest)
			require.JSONEq(t, string(record.Receipt), string(lookup.Record.Receipt))
		})

		t.Run("replay_same_id", func(t *testing.T) {
			revBefore := waitIdle(t)
			// An identical replay resubmits the same validated
			// intent, not a re-derived one: a fresh revision binds
			// a different authorization and correctly conflicts.
			record, err := svc.Submit(ctx, decision, installation, &successIntent)
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectCommitted, record.Outcome)
			require.Equal(t, revBefore, waitIdle(t), "replay must not claim")

			lookup, err := svc.Get(ctx, installation, successOpID)
			require.NoError(t, err)
			require.JSONEq(t, string(record.Receipt), string(lookup.Record.Receipt))
		})

		t.Run("intent_conflict", func(t *testing.T) {
			// A valid payload with a different base cannot match
			// the recorded digest.
			intent := validate(t, successOpID, nativeop_model.KindMerge,
				mergePayload(successNumber, "refs/heads/ft14-ok", "refs/heads/master", successHeadOID, strings.Repeat("a", 40)))
			_, err := svc.Submit(ctx, decision, installation, &intent)
			require.ErrorIs(t, err, operation_service.ErrIntentConflict)
		})

		t.Run("stale_head", func(t *testing.T) {
			number, prID := prepareCandidate(t, "ft14-stale-head", "ft14 merge stale head")
			headOID := tip(t, "refs/heads/ft14-stale-head")
			baseOID := master(t)

			// A competing writer advances the head after binding.
			editBranch(t, "ft14-stale-head", "Hello, World stale-head moved\n")
			require.NotEqual(t, headOID, tip(t, "refs/heads/ft14-stale-head"))

			intent := validate(t, "ft14-stale-head", nativeop_model.KindMerge,
				mergePayload(number, "refs/heads/ft14-stale-head", "refs/heads/master", headOID, baseOID))
			record, err := svc.Submit(ctx, decision, installation, &intent)
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectNotCommitted, record.Outcome)
			require.Equal(t, nativeop_model.ReasonStaleHead, record.ReasonCode)
			require.Equal(t, baseOID, tip(t, "refs/heads/master"), "refused merge must not move the base")
			require.False(t, loadPR(t, prID).HasMerged)
		})

		t.Run("stale_base", func(t *testing.T) {
			number, prID := prepareCandidate(t, "ft14-stale-base", "ft14 merge stale base")
			headOID := tip(t, "refs/heads/ft14-stale-base")
			baseOID := master(t)

			// A competing writer advances the base after binding.
			editBranch(t, "master", "Hello, World stale-base moved\n")
			require.NotEqual(t, baseOID, master(t))

			intent := validate(t, "ft14-stale-base", nativeop_model.KindMerge,
				mergePayload(number, "refs/heads/ft14-stale-base", "refs/heads/master", headOID, baseOID))
			record, err := svc.Submit(ctx, decision, installation, &intent)
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectNotCommitted, record.Outcome)
			require.Equal(t, nativeop_model.ReasonStaleBaseOrResult, record.ReasonCode)
			require.Equal(t, headOID, tip(t, "refs/heads/ft14-stale-base"), "refused merge must not move the head")
			require.False(t, loadPR(t, prID).HasMerged)
		})

		t.Run("diverging_non_ff", func(t *testing.T) {
			editBranch(t, "master", "line1\nline2\nline3\nline4\nline5\nline6\n")
			newBranch(t, "ft14-diverging", "line1\nLINE2\nline3\nline4\nline5\nline6\n")
			editBranch(t, "master", "line1\nline2\nline3\nline4\nLINE5\nline6\n")
			number, prID := apiCreatePR(t, "ft14-diverging", "master", "ft14 merge diverging")
			pr := refreshMergeability(t, loadPR(t, prID))
			// Disjoint edits stay three-way mergeable; only the
			// fast-forward-only engine refuses the divergence.
			require.Equal(t, issues_model.PullRequestStatusMergeable, pr.Status)

			headOID := tip(t, "refs/heads/ft14-diverging")
			baseOID := master(t)
			intent := validate(t, "ft14-diverging", nativeop_model.KindMerge,
				mergePayload(number, "refs/heads/ft14-diverging", "refs/heads/master", headOID, baseOID))
			record, err := svc.Submit(ctx, decision, installation, &intent)
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectNotCommitted, record.Outcome)
			require.Equal(t, nativeop_model.ReasonNotFastForward, record.ReasonCode)
			require.Equal(t, baseOID, tip(t, "refs/heads/master"), "diverged merge must not move the base")
			require.False(t, loadPR(t, prID).HasMerged)
		})

		t.Run("protection_refuses", func(t *testing.T) {
			number, prID := prepareCandidate(t, "ft14-protected", "ft14 merge protection")
			headOID := tip(t, "refs/heads/ft14-protected")
			baseOID := master(t)

			// ProtectionChange: require one approval on the base branch.
			waitIdle(t)
			session.MakeRequest(t, NewRequestWithJSON(t, http.MethodPost,
				"/api/v1/repos/user1/repo1/branch_protections",
				&api.BranchProtection{
					RuleName:          "master",
					RequiredApprovals: 1,
					ApplyToAdmins:     true,
				}).AddTokenAuth(token), http.StatusCreated)

			intent := validate(t, "ft14-protected", nativeop_model.KindMerge,
				mergePayload(number, "refs/heads/ft14-protected", "refs/heads/master", headOID, baseOID))
			record, err := svc.Submit(ctx, decision, installation, &intent)
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectNotCommitted, record.Outcome)
			require.Equal(t, nativeop_model.ReasonNativeRefused, record.ReasonCode)
			require.Equal(t, baseOID, tip(t, "refs/heads/master"), "protected merge must not move the base")
			require.False(t, loadPR(t, prID).HasMerged)

			// Restart after the protection is withdrawn: the same
			// exact candidate re-evaluates against current policy
			// under a fresh operation and merges.
			waitIdle(t)
			session.MakeRequest(t, NewRequestf(t, http.MethodDelete,
				"/api/v1/repos/user1/repo1/branch_protections/%s", "master").AddTokenAuth(token), http.StatusNoContent)

			restart := validate(t, "ft14-protected-restart", nativeop_model.KindMerge,
				mergePayload(number, "refs/heads/ft14-protected", "refs/heads/master", headOID, baseOID))
			restarted, err := svc.Submit(ctx, decision, installation, &restart)
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectCommitted, restarted.Outcome)
			require.Equal(t, nativeop_model.CompletionComplete, restarted.CompletionState)
			require.Equal(t, headOID, tip(t, "refs/heads/master"))
			require.True(t, loadPR(t, prID).HasMerged)
		})

		t.Run("method_not_allowed", func(t *testing.T) {
			number, prID := prepareCandidate(t, "ft14-method", "ft14 merge method policy")
			headOID := tip(t, "refs/heads/ft14-method")
			baseOID := master(t)

			// Current repository policy disallows the only
			// documented method: the candidate refuses even though
			// its refs are exact. A silently ignored policy flip
			// would commit below and fail the assertion.
			allow := false
			waitIdle(t)
			session.MakeRequest(t, NewRequestWithJSON(t, http.MethodPatch,
				"/api/v1/repos/user1/repo1",
				&api.EditRepoOption{AllowFastForwardOnly: &allow}).AddTokenAuth(token), http.StatusOK)

			intent := validate(t, "ft14-method", nativeop_model.KindMerge,
				mergePayload(number, "refs/heads/ft14-method", "refs/heads/master", headOID, baseOID))
			record, err := svc.Submit(ctx, decision, installation, &intent)
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectNotCommitted, record.Outcome)
			require.Equal(t, nativeop_model.ReasonNativeRefused, record.ReasonCode)
			require.Equal(t, baseOID, tip(t, "refs/heads/master"), "disallowed method must not move the base")
			require.False(t, loadPR(t, prID).HasMerged)

			allow = true
			waitIdle(t)
			session.MakeRequest(t, NewRequestWithJSON(t, http.MethodPatch,
				"/api/v1/repos/user1/repo1",
				&api.EditRepoOption{AllowFastForwardOnly: &allow}).AddTokenAuth(token), http.StatusOK)
		})

		t.Run("already_merged", func(t *testing.T) {
			intent := validate(t, "ft14-already-merged", nativeop_model.KindMerge,
				mergePayload(successNumber, "refs/heads/ft14-ok", "refs/heads/master", successHeadOID, master(t)))
			record, err := svc.Submit(ctx, decision, installation, &intent)
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectNotCommitted, record.Outcome)
			require.Equal(t, nativeop_model.ReasonPRMismatch, record.ReasonCode)
		})

		t.Run("binding_kind_mismatch", func(t *testing.T) {
			// Kind isolation at the operation boundary: the
			// review-bound credential carries no merge binding, so
			// the merge refuses even though the credential is
			// otherwise valid.
			intent := validate(t, "ft14-kind-mismatch", nativeop_model.KindMerge,
				mergePayload(successNumber, "refs/heads/ft14-ok", "refs/heads/master", successHeadOID, master(t)))
			record, err := svc.Submit(ctx, otherDecision, installation, &intent)
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectNotCommitted, record.Outcome)
			require.Equal(t, nativeop_model.ReasonAuthorityLost, record.ReasonCode)
		})

		t.Run("cancel_before_submit", func(t *testing.T) {
			before := master(t)
			cancelled, err := svc.Cancel(ctx, installation, "ft14-never-submitted")
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectNotCommitted, cancelled.Outcome)

			intent := validate(t, "ft14-never-submitted", nativeop_model.KindMerge,
				mergePayload(successNumber, "refs/heads/ft14-ok", "refs/heads/master", successHeadOID, before))
			_, err = svc.Submit(ctx, decision, installation, &intent)
			require.ErrorIs(t, err, operation_service.ErrCancelledBeforeSubmit)
			require.Equal(t, before, master(t))
		})

		t.Run("cancel_during_prepared", func(t *testing.T) {
			number, prID := prepareCandidate(t, "ft14-race", "ft14 merge cancel race")
			headOID := tip(t, "refs/heads/ft14-race")
			baseOID := master(t)
			intent := validate(t, "ft14-race", nativeop_model.KindMerge,
				mergePayload(number, "refs/heads/ft14-race", "refs/heads/master", headOID, baseOID))

			// The disclosed admission barrier pauses the atomic
			// prepared decision so cancellation wins the ordering.
			barrier := t.TempDir()
			t.Setenv("NATIVEOP_TEST_ADMISSION_BARRIER", barrier)
			type outcome struct {
				out    string
				reason string
				cancel string
				err    error
			}
			done := make(chan outcome, 1)
			go func() {
				record, err := svc.Submit(ctx, decision, installation, &intent)
				done <- outcome{out: record.Outcome, reason: record.ReasonCode, cancel: record.CancellationStatus, err: err}
			}()
			entered := filepath.Join(barrier, "admission.entered")
			deadline := time.Now().Add(20 * time.Second)
			for {
				if _, err := os.Stat(entered); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("prepared admission never reached the barrier")
				}
				time.Sleep(50 * time.Millisecond)
			}
			_, err := svc.Cancel(ctx, installation, "ft14-race")
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(barrier, "admission.release"), []byte("go\n"), 0o600))
			var res outcome
			select {
			case res = <-done:
			case <-time.After(60 * time.Second):
				t.Fatal("cancelled submit did not return")
			}
			require.NoError(t, res.err)
			require.Equal(t, nativeop_model.EffectNotCommitted, res.out)
			require.Equal(t, nativeop_model.ReasonCancelledBeforeAdmission, res.reason)
			require.Equal(t, nativeop_model.CancellationCancelled, res.cancel)
			require.Equal(t, baseOID, tip(t, "refs/heads/master"), "winning cancellation must prevent the merge")
			require.False(t, loadPR(t, prID).HasMerged)
		})

		t.Run("crash_before_native_recovery", func(t *testing.T) {
			number, prID := prepareCandidate(t, "ft14-crash-before", "ft14 merge crash before")
			headOID := tip(t, "refs/heads/ft14-crash-before")
			baseOID := master(t)
			intent := validate(t, "ft14-crash-before", nativeop_model.KindMerge,
				mergePayload(number, "refs/heads/ft14-crash-before", "refs/heads/master", headOID, baseOID))

			dir := t.TempDir()
			t.Setenv("NATIVEOP_TEST_CRASH_POINT", operation_service.CrashPointMergeBeforeNative)
			t.Setenv("NATIVEOP_TEST_CRASH_BARRIER_DIR", dir)
			t.Setenv("NATIVEOP_TEST_CRASH_TIMEOUT_SECONDS", "5")

			_, err := svc.Submit(ctx, decision, installation, &intent)
			require.Error(t, err, "barrier timeout must fail the submit closed")
			require.FileExists(t, filepath.Join(dir, operation_service.CrashPointMergeBeforeNative+".entered"))

			// The crash-equivalent state: claim held, no admission,
			// no ref effect, owner still held.
			observation, err := svc.ReadNativeRevision(ctx)
			require.NoError(t, err)
			require.False(t, observation.Idle)
			op, err := nativeop_model.LookupOperation(ctx, installation, "ft14-crash-before")
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectPending, op.EffectState)
			require.False(t, op.Admitted)
			require.Empty(t, op.Receipt)
			require.Equal(t, baseOID, tip(t, "refs/heads/master"))
			require.False(t, loadPR(t, prID).HasMerged)

			// Offline recovery under inhibition releases the
			// effect-free claim as not_committed.
			require.NoError(t, os.WriteFile(nativeop_model.OfflineMarkerPath(), []byte("recovery\n"), 0o600))
			t.Cleanup(func() { _ = os.Remove(nativeop_model.OfflineMarkerPath()) })
			reservation, err := nativeop_model.ReadReservation(ctx)
			require.NoError(t, err)
			assessment, err := svc.Recover(ctx, reservation.Owner, reservation.Generation)
			require.NoError(t, err)
			require.Equal(t, operation_service.RecoveryReleased, assessment.Verdict)
			require.Equal(t, nativeop_model.EffectNotCommitted, assessment.Effect)
			require.Equal(t, "conditional-merge", assessment.Family)

			recovered, err := nativeop_model.LookupOperation(ctx, installation, "ft14-crash-before")
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectNotCommitted, recovered.EffectState)
			require.Equal(t, nativeop_model.ReasonRecoveredNoEffect, recovered.Reason)

			observation, err = svc.ReadNativeRevision(ctx)
			require.NoError(t, err)
			require.True(t, observation.Idle)
		})

		t.Run("crash_after_native_recovery", func(t *testing.T) {
			number, prID := prepareCandidate(t, "ft14-crash-after", "ft14 merge crash after")
			headOID := tip(t, "refs/heads/ft14-crash-after")
			baseOID := master(t)
			intent := validate(t, "ft14-crash-after", nativeop_model.KindMerge,
				mergePayload(number, "refs/heads/ft14-crash-after", "refs/heads/master", headOID, baseOID))

			dir := t.TempDir()
			t.Setenv("NATIVEOP_TEST_CRASH_POINT", operation_service.CrashPointMergeAfterNative)
			t.Setenv("NATIVEOP_TEST_CRASH_BARRIER_DIR", dir)
			t.Setenv("NATIVEOP_TEST_CRASH_TIMEOUT_SECONDS", "5")

			_, err := svc.Submit(ctx, decision, installation, &intent)
			require.Error(t, err, "barrier timeout must fail the submit closed")
			require.FileExists(t, filepath.Join(dir, operation_service.CrashPointMergeAfterNative+".entered"))

			// The crash-equivalent state: the native merge ran to
			// completion (ref moved, PR merged) but reconciliation
			// never ran; the owner stays held.
			observation, err := svc.ReadNativeRevision(ctx)
			require.NoError(t, err)
			require.False(t, observation.Idle)
			require.Equal(t, headOID, tip(t, "refs/heads/master"))
			require.True(t, loadPR(t, prID).HasMerged)

			// Offline recovery under inhibition attributes the
			// admitted ref effect plus the merged PR and finalizes
			// the receipt as complete.
			require.NoError(t, os.WriteFile(nativeop_model.OfflineMarkerPath(), []byte("recovery\n"), 0o600))
			t.Cleanup(func() { _ = os.Remove(nativeop_model.OfflineMarkerPath()) })
			reservation, err := nativeop_model.ReadReservation(ctx)
			require.NoError(t, err)
			assessment, err := svc.Recover(ctx, reservation.Owner, reservation.Generation)
			require.NoError(t, err)
			require.Equal(t, operation_service.RecoveryReleased, assessment.Verdict)
			require.Equal(t, nativeop_model.EffectCommitted, assessment.Effect)
			require.Equal(t, "conditional-merge", assessment.Family)

			recovered, err := nativeop_model.LookupOperation(ctx, installation, "ft14-crash-after")
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectCommitted, recovered.EffectState)
			require.Equal(t, nativeop_model.CompletionComplete, recovered.Completion)
			receipt := decodeReceipt(t, json.RawMessage(recovered.Receipt))
			require.Equal(t, baseOID, receipt.OldOID)
			require.Equal(t, headOID, receipt.NewOID)
			require.Equal(t, number, receipt.PRNumber)
			require.Equal(t, prID, receipt.PRID)
			require.Equal(t, "fast-forward-only", receipt.Method)

			observation, err = svc.ReadNativeRevision(ctx)
			require.NoError(t, err)
			require.True(t, observation.Idle)
		})

		t.Run("crash_after_native_incomplete_recovery", func(t *testing.T) {
			number, prID := prepareCandidate(t, "ft14-crash-partial", "ft14 merge crash partial")
			headOID := tip(t, "refs/heads/ft14-crash-partial")
			baseOID := master(t)
			intent := validate(t, "ft14-crash-partial", nativeop_model.KindMerge,
				mergePayload(number, "refs/heads/ft14-crash-partial", "refs/heads/master", headOID, baseOID))

			dir := t.TempDir()
			t.Setenv("NATIVEOP_TEST_CRASH_POINT", operation_service.CrashPointMergeAfterNative)
			t.Setenv("NATIVEOP_TEST_CRASH_BARRIER_DIR", dir)
			t.Setenv("NATIVEOP_TEST_CRASH_TIMEOUT_SECONDS", "5")

			_, err := svc.Submit(ctx, decision, installation, &intent)
			require.Error(t, err, "barrier timeout must fail the submit closed")

			// Simulate the interleaving where the ref moved but the
			// PR row never recorded the merge: the admitted effect
			// is known, its bookkeeping is not. A raw storage
			// write erases the bookkeeping traces: model writers
			// stay fenced while the crashed owner is held, and
			// this simulates storage-level loss, not a native
			// write.
			pr := loadPR(t, prID)
			require.True(t, pr.HasMerged)
			pr.HasMerged = false
			pr.MergedCommitID = ""
			_, err = db.GetEngine(ctx).ID(pr.ID).Cols("has_merged", "merged_commit_id").Update(pr)
			require.NoError(t, err)

			require.NoError(t, os.WriteFile(nativeop_model.OfflineMarkerPath(), []byte("recovery\n"), 0o600))
			t.Cleanup(func() { _ = os.Remove(nativeop_model.OfflineMarkerPath()) })
			reservation, err := nativeop_model.ReadReservation(ctx)
			require.NoError(t, err)
			assessment, err := svc.Recover(ctx, reservation.Owner, reservation.Generation)
			require.NoError(t, err)
			require.Equal(t, operation_service.RecoveryReleased, assessment.Verdict)
			require.Equal(t, nativeop_model.EffectCommitted, assessment.Effect)
			require.Equal(t, "conditional-merge", assessment.Family)

			recovered, err := nativeop_model.LookupOperation(ctx, installation, "ft14-crash-partial")
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectCommitted, recovered.EffectState)
			require.Equal(t, nativeop_model.CompletionNeedsIntervention, recovered.Completion)
			receipt := decodeReceipt(t, json.RawMessage(recovered.Receipt))
			require.Equal(t, baseOID, receipt.OldOID)
			require.Equal(t, headOID, receipt.NewOID)
			require.Equal(t, number, receipt.PRNumber)

			observation, err := svc.ReadNativeRevision(ctx)
			require.NoError(t, err)
			require.True(t, observation.Idle)
		})

		t.Run("withdrawal_after_commit", func(t *testing.T) {
			record, err := svc.Cancel(ctx, installation, successOpID)
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectCommitted, record.Outcome)
			require.Equal(t, nativeop_model.CancellationTooLate, record.CancellationStatus)

			merged := loadPR(t, successPRID)
			require.True(t, merged.HasMerged)
			require.Equal(t, successHeadOID, strings.ToLower(merged.MergedCommitID))
		})
	})
}
