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
	"forgejo.org/models/extensionauth"
	issues_model "forgejo.org/models/issues"
	nativeop_model "forgejo.org/models/nativeoperation"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"
	operation_service "forgejo.org/services/nativeoperation"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPullPRCreate is the authoritative F-prcreate proof: one conditional
// pull_request.create submit drives the exact native PR through the shipping
// owners, and every divergence refuses without guessing. Subtests create a
// real PR from native fixture branches, replay the same ID without a second
// PR, refuse duplicates/stale intents/cancelled IDs, race cancellation
// against the atomic primary commit, crash after the primary commit and
// recover offline, keep an ordinary committed branch intact when PR creation
// fails, and record too_late withdrawal after commit. Similar PRs are never
// adopted: only the operation's own receipt proves creation.
//
// Disclosure: the cancel-race and crash subtests use the disclosed
// NATIVEOP_TEST_CRASH_POINT barrier instruments. The crash itself is a
// barrier timeout in-process, which retains the identical held-owner state a
// SIGKILL would leave (committed primary, unset completion, held owner,
// kept capability); offline recovery then runs for real under inhibition.
func TestPullPRCreate(t *testing.T) {
	onApplicationRun(t, func(t *testing.T, _ *url.URL) {
		session := loginUser(t, "user1")
		testRepoFork(t, session, "user2", "repo1", "user1", "repo1")
		token := getTokenForLoggedInUser(t, session, auth_model.AccessTokenScopeWriteRepository, auth_model.AccessTokenScopeWriteIssue)

		user1 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
		repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{OwnerID: 1, Name: "repo1"})
		svc := operation_service.Default()
		ctx := t.Context()
		installation := "7c12e001-0000-4000-8000-000000000000"

		tokenRow, err := auth_model.GetAccessTokenBySHA(ctx, token)
		require.NoError(t, err)
		decision := extensionauth.SubmissionDecision{
			TokenID:               tokenRow.ID,
			ActorID:               user1.ID,
			CredentialFingerprint: extensionauth.CredentialFingerprint(tokenRow.TokenHash, tokenRow.TokenSalt),
		}
		_, err = extensionauth.EnrollBinding(ctx, installation, tokenRow.ID, user1.ID, repo.ID, extensionauth.KindPRCreate)
		require.NoError(t, err)

		repoPath := repo_model.RepoPath(user1.Name, repo.Name)
		tip := func(t *testing.T, ref string) string {
			t.Helper()
			oid, err := git.GetFullCommitID(ctx, repoPath, ref)
			require.NoError(t, err, "fixture ref %s must resolve", ref)
			require.Len(t, oid, 40)
			return strings.ToLower(oid)
		}
		masterOID := tip(t, "refs/heads/master")
		branch2OID := tip(t, "refs/heads/branch2")
		homeOID := tip(t, "refs/heads/home-md-img-check")
		updateOID := tip(t, "refs/heads/pr-to-update")
		for name, oid := range map[string]string{"branch2": branch2OID, "home": homeOID, "update": updateOID} {
			require.NotEqual(t, masterOID, oid, "fixture branch %s must differ from master", name)
		}

		// waitIdle polls for writer quiescence: an idle reservation with
		// a revision stable across two observations. Async completion
		// advances the revision after Submit returns, so a single read
		// cannot anchor an exact-revision assertion.
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

		submit := func(t *testing.T, opID, payload string) (operation_service.ValidIntent, error) {
			t.Helper()
			revision := waitIdle(t)
			now := time.Now().Unix()
			intent, err := operation_service.ValidateIntent(opID, user1.ID, repo.ID, nativeop_model.KindPRCreate,
				"auth-rev-1", revision, now+600, []byte(payload), now)
			require.NoError(t, err)
			return *intent, nil
		}
		payload := func(headRef, baseRef, headOID, baseOID, title string) string {
			return fmt.Sprintf(`{"head_repository_id":%d,`+
				`"head_ref":%q,"base_ref":%q,`+
				`"expected_head_oid":%q,"expected_base_oid":%q,`+
				`"title":%q,"body":"ft12 proof body",`+
				`"allow_maintainer_edit":false}`,
				repo.ID, headRef, baseRef, headOID, baseOID, title)
		}
		prCount := func(t *testing.T) int {
			t.Helper()
			return unittest.GetCount(t, &issues_model.Issue{RepoID: repo.ID, IsPull: true})
		}
		decodeReceipt := func(t *testing.T, raw json.RawMessage) operation_service.PRCreateReceipt {
			t.Helper()
			var receipt operation_service.PRCreateReceipt
			require.NoError(t, json.Unmarshal(raw, &receipt))
			return receipt
		}

		var successOpID string
		var successNumber int64
		var successIntent operation_service.ValidIntent

		t.Run("success_exact", func(t *testing.T) {
			successOpID = "ft12-success"
			intent, err := submit(t, successOpID, payload("refs/heads/branch2", "refs/heads/master", branch2OID, masterOID, "ft12 exact PR"))
			require.NoError(t, err)
			successIntent = intent
			record, err := svc.Submit(ctx, decision, installation, &intent)
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectCommitted, record.Outcome)
			require.Equal(t, nativeop_model.CompletionComplete, record.CompletionState)
			receipt := decodeReceipt(t, record.Receipt)
			require.Equal(t, user1.ID, receipt.AuthorID)
			require.Equal(t, repo.ID, receipt.RepositoryID)
			require.Equal(t, "refs/heads/branch2", receipt.HeadRef)
			require.Equal(t, "refs/heads/master", receipt.BaseRef)
			require.Equal(t, branch2OID, receipt.HeadOID)
			require.Equal(t, masterOID, receipt.BaseOID)
			successNumber = receipt.PRNumber

			// The native PR rows carry the shared native preparation:
			// index, branches, author, merge base, head commit and
			// divergence.
			pr, err := issues_model.GetPullRequestByIndex(ctx, repo.ID, receipt.PRNumber)
			require.NoError(t, err)
			require.Equal(t, receipt.PRID, pr.ID)
			require.Equal(t, receipt.IssueID, pr.IssueID)
			require.Equal(t, "branch2", pr.HeadBranch)
			require.Equal(t, "master", pr.BaseBranch)
			require.NotEmpty(t, pr.MergeBase)
			require.GreaterOrEqual(t, pr.CommitsAhead+pr.CommitsBehind, 1)
			// HeadCommitID is transient by native design (xorm:"-"):
			// native callers recompute it via testPatch on load.
			require.NoError(t, pr.LoadIssue(ctx))
			require.Equal(t, user1.ID, pr.Issue.PosterID)
			require.Equal(t, "ft12 exact PR", pr.Issue.Title)

			// Bounded completion established the exact internal ref
			// and the push-history comment.
			internalTip, err := git.GetFullCommitID(ctx, repoPath, fmt.Sprintf("refs/pull/%d/head", receipt.PRNumber))
			require.NoError(t, err)
			require.Equal(t, branch2OID, strings.ToLower(internalTip))
			comments, err := issues_model.FindComments(ctx, &issues_model.FindCommentsOptions{
				IssueID: receipt.IssueID,
				Type:    issues_model.CommentTypePullRequestPush,
			})
			require.NoError(t, err)
			require.NotEmpty(t, comments)

			// A lost reply reconciles through Get: same digest and
			// same receipt, never a replacement create.
			lookup, err := svc.Get(ctx, installation, successOpID)
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectCommitted, lookup.Status)
			require.NotNil(t, lookup.Record)
			require.Equal(t, record.IntentDigest, lookup.Record.IntentDigest)
			require.JSONEq(t, string(record.Receipt), string(lookup.Record.Receipt))
		})

		t.Run("replay_same_id", func(t *testing.T) {
			before := prCount(t)
			// An identical replay resubmits the same validated
			// intent, not a re-derived one: a fresh revision binds
			// a different authorization and correctly conflicts.
			record, err := svc.Submit(ctx, decision, installation, &successIntent)
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectCommitted, record.Outcome)
			require.Equal(t, before, prCount(t), "replay must not create a second PR")

			lookup, err := svc.Get(ctx, installation, successOpID)
			require.NoError(t, err)
			require.JSONEq(t, string(record.Receipt), string(lookup.Record.Receipt))
		})

		t.Run("intent_conflict", func(t *testing.T) {
			intent, err := submit(t, successOpID, payload("refs/heads/branch2", "refs/heads/master", branch2OID, masterOID, "changed title"))
			require.NoError(t, err)
			_, err = svc.Submit(ctx, decision, installation, &intent)
			require.ErrorIs(t, err, operation_service.ErrIntentConflict)
		})

		t.Run("duplicate_pr", func(t *testing.T) {
			before := prCount(t)
			intent, err := submit(t, "ft12-duplicate", payload("refs/heads/branch2", "refs/heads/master", branch2OID, masterOID, "ft12 exact PR"))
			require.NoError(t, err)
			record, err := svc.Submit(ctx, decision, installation, &intent)
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectNotCommitted, record.Outcome)
			require.Equal(t, nativeop_model.ReasonDuplicatePullRequest, record.ReasonCode)
			require.Equal(t, before, prCount(t), "duplicate refusal must not adopt the existing PR")
		})

		t.Run("stale_head", func(t *testing.T) {
			intent, err := submit(t, "ft12-stale-head", payload("refs/heads/branch2", "refs/heads/master", homeOID, masterOID, "stale"))
			require.NoError(t, err)
			record, err := svc.Submit(ctx, decision, installation, &intent)
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectNotCommitted, record.Outcome)
			require.Equal(t, nativeop_model.ReasonStaleHead, record.ReasonCode)
		})

		t.Run("cancel_before_submit", func(t *testing.T) {
			before := prCount(t)
			cancelled, err := svc.Cancel(ctx, installation, "ft12-never-submitted")
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectNotCommitted, cancelled.Outcome)

			intent, err := submit(t, "ft12-never-submitted", payload("refs/heads/home-md-img-check", "refs/heads/master", homeOID, masterOID, "never"))
			require.NoError(t, err)
			_, err = svc.Submit(ctx, decision, installation, &intent)
			require.ErrorIs(t, err, operation_service.ErrCancelledBeforeSubmit)
			require.Equal(t, before, prCount(t))
		})

		t.Run("cancel_race", func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("NATIVEOP_TEST_CRASH_POINT", operation_service.CrashPointPRCreateBeforePrimary)
			t.Setenv("NATIVEOP_TEST_CRASH_BARRIER_DIR", dir)
			before := prCount(t)

			intent, err := submit(t, "ft12-cancel-race", payload("refs/heads/home-md-img-check", "refs/heads/master", homeOID, masterOID, "raced"))
			require.NoError(t, err)
			type result struct {
				out    string
				reason string
				cancel string
				err    error
			}
			done := make(chan result, 1)
			go func() {
				record, err := svc.Submit(ctx, decision, installation, &intent)
				done <- result{out: record.Outcome, reason: record.ReasonCode, cancel: record.CancellationStatus, err: err}
			}()
			entered := filepath.Join(dir, operation_service.CrashPointPRCreateBeforePrimary+".entered")
			deadline := time.Now().Add(30 * time.Second)
			for {
				if _, err := os.Stat(entered); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("submit did not reach the pre-primary barrier")
				}
				time.Sleep(10 * time.Millisecond)
			}
			// The claim is held and the primary has not committed:
			// revoke now so the atomic commit must observe it.
			_, err = svc.Cancel(ctx, installation, "ft12-cancel-race")
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, operation_service.CrashPointPRCreateBeforePrimary+".release"), []byte("go\n"), 0o600))

			var res result
			select {
			case res = <-done:
			case <-time.After(60 * time.Second):
				t.Fatal("cancelled submit did not return")
			}
			require.NoError(t, res.err)
			require.Equal(t, nativeop_model.EffectNotCommitted, res.out)
			require.Equal(t, nativeop_model.ReasonCancelledBeforeAdmission, res.reason)
			require.Equal(t, nativeop_model.CancellationCancelled, res.cancel)
			require.Equal(t, before, prCount(t), "winning cancellation must prevent the primary effect")
			_, err = issues_model.GetUnmergedPullRequest(ctx, repo.ID, repo.ID, "home-md-img-check", "master", issues_model.PullRequestFlowGithub)
			require.True(t, issues_model.IsErrPullRequestNotExist(err))
		})

		t.Run("crash_after_primary_recovery", func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("NATIVEOP_TEST_CRASH_POINT", operation_service.CrashPointPRCreateAfterPrimary)
			t.Setenv("NATIVEOP_TEST_CRASH_BARRIER_DIR", dir)
			t.Setenv("NATIVEOP_TEST_CRASH_TIMEOUT_SECONDS", "5")

			intent, err := submit(t, "ft12-crash", payload("refs/heads/pr-to-update", "refs/heads/master", updateOID, masterOID, "crashed"))
			require.NoError(t, err)
			_, err = svc.Submit(ctx, decision, installation, &intent)
			require.Error(t, err, "barrier timeout must fail the submit closed")
			require.FileExists(t, filepath.Join(dir, operation_service.CrashPointPRCreateAfterPrimary+".entered"))

			// The crash-equivalent state: primary committed with its
			// receipt, completion unset, owner still held.
			observation, err := svc.ReadNativeRevision(ctx)
			require.NoError(t, err)
			require.False(t, observation.Idle)
			op, err := nativeop_model.LookupOperation(ctx, installation, "ft12-crash")
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectCommitted, op.EffectState)
			require.NotEmpty(t, op.Receipt)
			require.Equal(t, nativeop_model.CompletionPending, op.Completion)

			// Offline recovery under inhibition attributes the
			// recorded PR and finalizes completion as uncertain.
			require.NoError(t, os.WriteFile(nativeop_model.OfflineMarkerPath(), []byte("recovery\n"), 0o600))
			t.Cleanup(func() { _ = os.Remove(nativeop_model.OfflineMarkerPath()) })
			reservation, err := nativeop_model.ReadReservation(ctx)
			require.NoError(t, err)
			assessment, err := svc.Recover(ctx, reservation.Owner, reservation.Generation)
			require.NoError(t, err)
			require.Equal(t, operation_service.RecoveryReleased, assessment.Verdict)
			require.Equal(t, nativeop_model.EffectCommitted, assessment.Effect)
			require.Equal(t, "conditional-prcreate", assessment.Family)

			recovered, err := nativeop_model.LookupOperation(ctx, installation, "ft12-crash")
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectCommitted, recovered.EffectState)
			require.Equal(t, nativeop_model.CompletionNeedsIntervention, recovered.Completion)
			receipt := decodeReceipt(t, json.RawMessage(recovered.Receipt))
			pr, err := issues_model.GetPullRequestByIndex(ctx, repo.ID, receipt.PRNumber)
			require.NoError(t, err)
			require.Equal(t, receipt.PRID, pr.ID)

			observation, err = svc.ReadNativeRevision(ctx)
			require.NoError(t, err)
			require.True(t, observation.Idle)
		})

		t.Run("branch_committed_pr_failed", func(t *testing.T) {
			// An ordinary committed branch persists independently of
			// a refused conditional PR creation over it.
			testAPICreateBranch(t, session, "user1", "repo1", "branch2", "ft12-ordinary", http.StatusCreated)
			ordinaryOID := tip(t, "refs/heads/ft12-ordinary")
			require.Equal(t, branch2OID, ordinaryOID)
			before := prCount(t)

			intent, err := submit(t, "ft12-branch-survives", payload("refs/heads/ft12-ordinary", "refs/heads/master", ordinaryOID, homeOID, "wrong base"))
			require.NoError(t, err)
			record, err := svc.Submit(ctx, decision, installation, &intent)
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectNotCommitted, record.Outcome)
			require.Equal(t, nativeop_model.ReasonStaleBaseOrResult, record.ReasonCode)
			require.Equal(t, before, prCount(t))
			require.Equal(t, ordinaryOID, tip(t, "refs/heads/ft12-ordinary"), "refused PR creation must not move the branch")
		})

		t.Run("withdrawal_after_commit", func(t *testing.T) {
			record, err := svc.Cancel(ctx, installation, successOpID)
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectCommitted, record.Outcome)
			require.Equal(t, nativeop_model.CancellationTooLate, record.CancellationStatus)

			pr, err := issues_model.GetPullRequestByIndex(ctx, repo.ID, successNumber)
			require.NoError(t, err)
			require.False(t, pr.HasMerged)
			assert.Equal(t, "branch2", pr.HeadBranch)
		})
	})
}
