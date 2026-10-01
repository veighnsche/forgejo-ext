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
	api "forgejo.org/modules/structs"
	operation_service "forgejo.org/services/nativeoperation"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPullReviewSubmit is the authoritative F-review proof: one conditional
// pull_request.review.submit drives an exact body-only native review through
// the shipping owners, and every divergence refuses without guessing.
// Subtests approve and request changes on native fixture PRs through a
// separately enrolled reviewer, replay the same ID without a second review,
// refuse stale/self/pending-draft/closed intents and a foreign-kind
// credential, race cancellation against the atomic primary commit, crash
// after the primary commit and recover offline, and record too_late
// withdrawal after commit. Similar reviews are never adopted: only the
// operation's own receipt proves submission.
//
// Disclosure: the cancel-race and crash subtests use the disclosed
// NATIVEOP_TEST_CRASH_POINT barrier instruments. The crash itself is a
// barrier timeout in-process, which retains the identical held-owner state a
// SIGKILL would leave (committed primary, unset completion, held owner,
// kept capability); offline recovery then runs for real under inhibition.
func TestPullReviewSubmit(t *testing.T) {
	onApplicationRun(t, func(t *testing.T, _ *url.URL) {
		session := loginUser(t, "user1")
		testRepoFork(t, session, "user2", "repo1", "user1", "repo1")
		authorToken := getTokenForLoggedInUser(t, session, auth_model.AccessTokenScopeWriteRepository, auth_model.AccessTokenScopeWriteIssue)

		user1 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
		user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
		repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{OwnerID: 1, Name: "repo1"})
		svc := operation_service.Default()
		ctx := t.Context()
		installation := "7c12e001-0000-4000-8000-000000000001"

		authorTokenRow, err := auth_model.GetAccessTokenBySHA(ctx, authorToken)
		require.NoError(t, err)
		authorDecision := extensionauth.SubmissionDecision{
			TokenID:               authorTokenRow.ID,
			ActorID:               user1.ID,
			CredentialFingerprint: extensionauth.CredentialFingerprint(authorTokenRow.TokenHash, authorTokenRow.TokenSalt),
		}
		_, err = extensionauth.EnrollBinding(ctx, installation, authorTokenRow.ID, user1.ID, repo.ID, extensionauth.KindPRCreate)
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

		validate := func(t *testing.T, opID string, actorID int64, kind, payload string) operation_service.ValidIntent {
			t.Helper()
			revision := waitIdle(t)
			now := time.Now().Unix()
			intent, err := operation_service.ValidateIntent(opID, actorID, repo.ID, kind,
				"auth-rev-1", revision, now+600, []byte(payload), now)
			require.NoError(t, err)
			return *intent
		}
		// Fixture PRs come from the ordinary native API, so this proof
		// depends on no other conditional implementation.
		createPR := func(t *testing.T, headBranch, title string) (number, issueID int64) {
			t.Helper()
			session.MakeRequest(t, NewRequestWithJSON(t, http.MethodPost,
				fmt.Sprintf("/api/v1/repos/%s/%s/pulls", repo.OwnerName, repo.Name),
				&api.CreatePullRequestOption{
					Head:  headBranch,
					Base:  "master",
					Title: title,
				}).AddTokenAuth(authorToken), http.StatusCreated)
			pr := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{
				HeadRepoID: repo.ID, BaseRepoID: repo.ID, HeadBranch: headBranch, BaseBranch: "master",
			})
			return pr.Index, pr.IssueID
		}

		numberA, issueA := createPR(t, "branch2", "ft13 PR A")
		numberB, issueB := createPR(t, "home-md-img-check", "ft13 PR B")
		numberC, issueC := createPR(t, "pr-to-update", "ft13 PR C")

		// Fresh reviewer configuration, enrolled after the PRs exist: a
		// distinct native actor with a new token and a review-only
		// operation binding. The reviewer holds code write on the fork
		// as an ordinary collaborator, which the submission authority
		// check requires; the binding still grants no other kind.
		reviewerSession := loginUser(t, "user2")
		writePermission := "write"
		session.MakeRequest(t, NewRequestWithJSON(t, http.MethodPut,
			fmt.Sprintf("/api/v1/repos/%s/%s/collaborators/%s", repo.OwnerName, repo.Name, user2.Name),
			&api.AddCollaboratorOption{Permission: &writePermission}).AddTokenAuth(authorToken), http.StatusNoContent)
		reviewerToken := getTokenForLoggedInUser(t, reviewerSession, auth_model.AccessTokenScopeWriteRepository)
		reviewerTokenRow, err := auth_model.GetAccessTokenBySHA(ctx, reviewerToken)
		require.NoError(t, err)
		reviewerDecision := extensionauth.SubmissionDecision{
			TokenID:               reviewerTokenRow.ID,
			ActorID:               user2.ID,
			CredentialFingerprint: extensionauth.CredentialFingerprint(reviewerTokenRow.TokenHash, reviewerTokenRow.TokenSalt),
		}
		_, err = extensionauth.EnrollBinding(ctx, installation, reviewerTokenRow.ID, user2.ID, repo.ID, extensionauth.KindReviewSubmit)
		require.NoError(t, err)

		reviewPayload := func(number, author int64, headRef, baseRef, headOID, baseOID, event, body string) string {
			return fmt.Sprintf(`{"pull_request_number":%d,"pr_author_id":%d,`+
				`"head_repository_id":%d,`+
				`"head_ref":%q,"base_ref":%q,`+
				`"expected_head_oid":%q,"expected_base_oid":%q,`+
				`"commit_id":%q,"event":%q,"body":%q}`,
				number, author, repo.ID, headRef, baseRef, headOID, baseOID, headOID, event, body)
		}
		payloadA := func(event, body string) string {
			return reviewPayload(numberA, user1.ID, "refs/heads/branch2", "refs/heads/master", branch2OID, masterOID, event, body)
		}
		payloadB := func(event, body string) string {
			return reviewPayload(numberB, user1.ID, "refs/heads/home-md-img-check", "refs/heads/master", homeOID, masterOID, event, body)
		}
		payloadC := func(event, body string) string {
			return reviewPayload(numberC, user1.ID, "refs/heads/pr-to-update", "refs/heads/master", updateOID, masterOID, event, body)
		}
		reviewCount := func(t *testing.T, issueID int64) int {
			t.Helper()
			return unittest.GetCount(t, &issues_model.Review{IssueID: issueID})
		}
		decodeReceipt := func(t *testing.T, raw json.RawMessage) operation_service.ReviewSubmitReceipt {
			t.Helper()
			var receipt operation_service.ReviewSubmitReceipt
			require.NoError(t, json.Unmarshal(raw, &receipt))
			return receipt
		}
		loadIssue := func(t *testing.T, issueID int64) *issues_model.Issue {
			t.Helper()
			issue := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: issueID})
			require.NoError(t, issue.LoadPullRequest(ctx))
			return issue
		}

		var successOpID string
		var successReviewID int64
		var successIntent operation_service.ValidIntent

		t.Run("success_approve", func(t *testing.T) {
			successOpID = "ft13-approve"
			intent := validate(t, successOpID, user2.ID, nativeop_model.KindReviewSubmit, payloadA("APPROVED", "ship it @user1"))
			successIntent = intent
			record, err := svc.Submit(ctx, reviewerDecision, installation, &intent)
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectCommitted, record.Outcome)
			require.Equal(t, nativeop_model.CompletionComplete, record.CompletionState)
			receipt := decodeReceipt(t, record.Receipt)
			require.Equal(t, user2.ID, receipt.ReviewerID)
			require.Equal(t, issueA, receipt.IssueID)
			require.Equal(t, numberA, receipt.PRNumber)
			require.Equal(t, branch2OID, receipt.CommitID)
			require.Equal(t, "APPROVED", receipt.Event)
			require.Equal(t, branch2OID, receipt.HeadOID)
			require.Equal(t, masterOID, receipt.BaseOID)
			successReviewID = receipt.ReviewID

			// The native review rows carry the exact candidate: final
			// type, bound commit, no stale mark, native official
			// computation and the bundled review comment.
			review, err := issues_model.GetReviewByID(ctx, receipt.ReviewID)
			require.NoError(t, err)
			require.Equal(t, issues_model.ReviewTypeApprove, review.Type)
			require.Equal(t, user2.ID, review.ReviewerID)
			require.Equal(t, issueA, review.IssueID)
			require.Equal(t, branch2OID, review.CommitID)
			require.False(t, review.Stale)
			require.False(t, review.Dismissed)
			expectedOfficial, err := issues_model.IsOfficialReviewer(ctx, loadIssue(t, issueA), user2)
			require.NoError(t, err)
			require.Equal(t, expectedOfficial, review.Official)
			comment := unittest.AssertExistsAndLoadBean(t, &issues_model.Comment{ID: receipt.CommentID})
			require.Equal(t, issues_model.CommentTypeReview, comment.Type)
			require.Equal(t, receipt.ReviewID, comment.ReviewID)
			require.Equal(t, "ship it @user1", comment.Content)

			// Bounded completion recorded the mention through the
			// shared ordinary completion.
			mentioned := unittest.AssertExistsAndLoadBean(t, &issues_model.IssueUser{IssueID: issueA, UID: user1.ID})
			require.True(t, mentioned.IsMentioned)

			// A lost reply reconciles through Get: same digest and
			// same receipt, never a replacement submit.
			lookup, err := svc.Get(ctx, installation, successOpID)
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectCommitted, lookup.Status)
			require.NotNil(t, lookup.Record)
			require.Equal(t, record.IntentDigest, lookup.Record.IntentDigest)
			require.JSONEq(t, string(record.Receipt), string(lookup.Record.Receipt))
		})

		t.Run("request_changes", func(t *testing.T) {
			intent := validate(t, "ft13-reject", user2.ID, nativeop_model.KindReviewSubmit, payloadB("REQUEST_CHANGES", "please add tests"))
			record, err := svc.Submit(ctx, reviewerDecision, installation, &intent)
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectCommitted, record.Outcome)
			require.Equal(t, nativeop_model.CompletionComplete, record.CompletionState)
			receipt := decodeReceipt(t, record.Receipt)
			require.Equal(t, "REQUEST_CHANGES", receipt.Event)
			review, err := issues_model.GetReviewByID(ctx, receipt.ReviewID)
			require.NoError(t, err)
			require.Equal(t, issues_model.ReviewTypeReject, review.Type)
			require.Equal(t, homeOID, review.CommitID)
		})

		t.Run("replay_same_id", func(t *testing.T) {
			before := reviewCount(t, issueA)
			// An identical replay resubmits the same validated
			// intent, not a re-derived one: a fresh revision binds
			// a different authorization and correctly conflicts.
			record, err := svc.Submit(ctx, reviewerDecision, installation, &successIntent)
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectCommitted, record.Outcome)
			require.Equal(t, before, reviewCount(t, issueA), "replay must not create a second review")

			lookup, err := svc.Get(ctx, installation, successOpID)
			require.NoError(t, err)
			require.JSONEq(t, string(record.Receipt), string(lookup.Record.Receipt))
		})

		t.Run("intent_conflict", func(t *testing.T) {
			intent := validate(t, successOpID, user2.ID, nativeop_model.KindReviewSubmit, payloadA("APPROVED", "changed body"))
			_, err = svc.Submit(ctx, reviewerDecision, installation, &intent)
			require.ErrorIs(t, err, operation_service.ErrIntentConflict)
		})

		t.Run("stale_head", func(t *testing.T) {
			intent := validate(t, "ft13-stale-head", user2.ID, nativeop_model.KindReviewSubmit,
				reviewPayload(numberA, user1.ID, "refs/heads/branch2", "refs/heads/master", homeOID, masterOID, "APPROVED", "stale"))
			record, err := svc.Submit(ctx, reviewerDecision, installation, &intent)
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectNotCommitted, record.Outcome)
			require.Equal(t, nativeop_model.ReasonStaleHead, record.ReasonCode)
		})

		t.Run("stale_base", func(t *testing.T) {
			intent := validate(t, "ft13-stale-base", user2.ID, nativeop_model.KindReviewSubmit,
				reviewPayload(numberA, user1.ID, "refs/heads/branch2", "refs/heads/master", branch2OID, homeOID, "APPROVED", "stale"))
			record, err := svc.Submit(ctx, reviewerDecision, installation, &intent)
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectNotCommitted, record.Outcome)
			require.Equal(t, nativeop_model.ReasonStaleBaseOrResult, record.ReasonCode)
		})

		t.Run("self_review", func(t *testing.T) {
			before := reviewCount(t, issueA)
			intent := validate(t, "ft13-self", user1.ID, nativeop_model.KindReviewSubmit, payloadA("APPROVED", "self approval"))
			record, err := svc.Submit(ctx, authorDecision, installation, &intent)
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectNotCommitted, record.Outcome)
			require.Equal(t, nativeop_model.ReasonAuthorityLost, record.ReasonCode)
			require.Equal(t, before, reviewCount(t, issueA), "self review must not commit")
		})

		t.Run("binding_kind_mismatch", func(t *testing.T) {
			// Role separation at the operation boundary: the
			// author's PR-create credential carries no review
			// binding, so the review submission refuses even
			// though the credential is otherwise valid.
			before := reviewCount(t, issueA)
			forged := extensionauth.SubmissionDecision{
				TokenID:               authorTokenRow.ID,
				ActorID:               user1.ID,
				CredentialFingerprint: extensionauth.CredentialFingerprint(authorTokenRow.TokenHash, authorTokenRow.TokenSalt),
			}
			intent := validate(t, "ft13-kind-mismatch", user1.ID, nativeop_model.KindReviewSubmit, payloadA("APPROVED", "wrong kind"))
			record, err := svc.Submit(ctx, forged, installation, &intent)
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectNotCommitted, record.Outcome)
			require.Equal(t, nativeop_model.ReasonAuthorityLost, record.ReasonCode)
			require.Equal(t, before, reviewCount(t, issueA))
		})

		t.Run("pending_draft", func(t *testing.T) {
			before := reviewCount(t, issueC)
			// An ordinary pending draft through the native API: an
			// empty event stays pending with a body.
			reviewerSession.MakeRequest(t, NewRequestWithJSON(t, http.MethodPost,
				fmt.Sprintf("/api/v1/repos/%s/%s/pulls/%d/reviews", repo.OwnerName, repo.Name, numberC),
				&api.CreatePullReviewOptions{Body: "draft notes"}).AddTokenAuth(reviewerToken), http.StatusOK)
			issue := loadIssue(t, issueC)
			pending, err := issues_model.GetCurrentReview(ctx, user2, issue)
			require.NoError(t, err)
			require.Equal(t, issues_model.ReviewTypePending, pending.Type)

			intent := validate(t, "ft13-pending", user2.ID, nativeop_model.KindReviewSubmit, payloadC("APPROVED", "no adoption"))
			record, err := svc.Submit(ctx, reviewerDecision, installation, &intent)
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectNotCommitted, record.Outcome)
			require.Equal(t, nativeop_model.ReasonPendingReviewExists, record.ReasonCode)
			require.Equal(t, before+1, reviewCount(t, issueC), "only the ordinary draft exists")

			// The draft stays untouched: still pending, never
			// adopted or deleted.
			pending, err = issues_model.GetCurrentReview(ctx, user2, loadIssue(t, issueC))
			require.NoError(t, err)
			require.Equal(t, issues_model.ReviewTypePending, pending.Type)
			require.Equal(t, "draft notes", pending.Content)
		})

		t.Run("cancel_before_submit", func(t *testing.T) {
			before := reviewCount(t, issueA)
			cancelled, err := svc.Cancel(ctx, installation, "ft13-never-submitted")
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectNotCommitted, cancelled.Outcome)

			intent := validate(t, "ft13-never-submitted", user2.ID, nativeop_model.KindReviewSubmit, payloadA("APPROVED", "never"))
			_, err = svc.Submit(ctx, reviewerDecision, installation, &intent)
			require.ErrorIs(t, err, operation_service.ErrCancelledBeforeSubmit)
			require.Equal(t, before, reviewCount(t, issueA))
		})

		t.Run("cancel_race", func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("NATIVEOP_TEST_CRASH_POINT", operation_service.CrashPointReviewSubmitBeforePrimary)
			t.Setenv("NATIVEOP_TEST_CRASH_BARRIER_DIR", dir)
			before := reviewCount(t, issueB)

			intent := validate(t, "ft13-cancel-race", user2.ID, nativeop_model.KindReviewSubmit, payloadB("APPROVED", "raced"))
			type result struct {
				out    string
				reason string
				cancel string
				err    error
			}
			done := make(chan result, 1)
			go func() {
				record, err := svc.Submit(ctx, reviewerDecision, installation, &intent)
				done <- result{out: record.Outcome, reason: record.ReasonCode, cancel: record.CancellationStatus, err: err}
			}()
			entered := filepath.Join(dir, operation_service.CrashPointReviewSubmitBeforePrimary+".entered")
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
			_, err = svc.Cancel(ctx, installation, "ft13-cancel-race")
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, operation_service.CrashPointReviewSubmitBeforePrimary+".release"), []byte("go\n"), 0o600))

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
			require.Equal(t, before, reviewCount(t, issueB), "winning cancellation must prevent the primary effect")
		})

		t.Run("crash_after_primary_recovery", func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("NATIVEOP_TEST_CRASH_POINT", operation_service.CrashPointReviewSubmitAfterPrimary)
			t.Setenv("NATIVEOP_TEST_CRASH_BARRIER_DIR", dir)
			t.Setenv("NATIVEOP_TEST_CRASH_TIMEOUT_SECONDS", "5")

			intent := validate(t, "ft13-crash", user2.ID, nativeop_model.KindReviewSubmit, payloadB("APPROVED", "crashed"))
			_, err = svc.Submit(ctx, reviewerDecision, installation, &intent)
			require.Error(t, err, "barrier timeout must fail the submit closed")
			require.FileExists(t, filepath.Join(dir, operation_service.CrashPointReviewSubmitAfterPrimary+".entered"))

			// The crash-equivalent state: primary committed with its
			// receipt, completion unset, owner still held.
			observation, err := svc.ReadNativeRevision(ctx)
			require.NoError(t, err)
			require.False(t, observation.Idle)
			op, err := nativeop_model.LookupOperation(ctx, installation, "ft13-crash")
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectCommitted, op.EffectState)
			require.NotEmpty(t, op.Receipt)
			require.Equal(t, nativeop_model.CompletionPending, op.Completion)

			// Offline recovery under inhibition attributes the
			// recorded review and finalizes completion as uncertain.
			require.NoError(t, os.WriteFile(nativeop_model.OfflineMarkerPath(), []byte("recovery\n"), 0o600))
			t.Cleanup(func() { _ = os.Remove(nativeop_model.OfflineMarkerPath()) })
			reservation, err := nativeop_model.ReadReservation(ctx)
			require.NoError(t, err)
			assessment, err := svc.Recover(ctx, reservation.Owner, reservation.Generation)
			require.NoError(t, err)
			require.Equal(t, operation_service.RecoveryReleased, assessment.Verdict)
			require.Equal(t, nativeop_model.EffectCommitted, assessment.Effect)
			require.Equal(t, "conditional-review", assessment.Family)

			recovered, err := nativeop_model.LookupOperation(ctx, installation, "ft13-crash")
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectCommitted, recovered.EffectState)
			require.Equal(t, nativeop_model.CompletionNeedsIntervention, recovered.Completion)
			receipt := decodeReceipt(t, json.RawMessage(recovered.Receipt))
			review, err := issues_model.GetReviewByID(ctx, receipt.ReviewID)
			require.NoError(t, err)
			require.Equal(t, user2.ID, review.ReviewerID)
			require.Equal(t, issues_model.ReviewTypeApprove, review.Type)

			observation, err = svc.ReadNativeRevision(ctx)
			require.NoError(t, err)
			require.True(t, observation.Idle)
		})

		t.Run("closed_pr", func(t *testing.T) {
			before := reviewCount(t, issueC)
			closed := "closed"
			session.MakeRequest(t, NewRequestWithJSON(t, http.MethodPatch,
				fmt.Sprintf("/api/v1/repos/%s/%s/pulls/%d", repo.OwnerName, repo.Name, numberC),
				&api.EditPullRequestOption{State: &closed}).AddTokenAuth(authorToken), http.StatusCreated)

			intent := validate(t, "ft13-closed", user2.ID, nativeop_model.KindReviewSubmit, payloadC("APPROVED", "too late"))
			record, err := svc.Submit(ctx, reviewerDecision, installation, &intent)
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectNotCommitted, record.Outcome)
			require.Equal(t, nativeop_model.ReasonPRMismatch, record.ReasonCode)
			require.Equal(t, before, reviewCount(t, issueC), "closed PR must refuse without new rows")
		})

		t.Run("withdrawal_after_commit", func(t *testing.T) {
			record, err := svc.Cancel(ctx, installation, successOpID)
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectCommitted, record.Outcome)
			require.Equal(t, nativeop_model.CancellationTooLate, record.CancellationStatus)

			review, err := issues_model.GetReviewByID(ctx, successReviewID)
			require.NoError(t, err)
			require.Equal(t, issues_model.ReviewTypeApprove, review.Type)
			assert.Equal(t, branch2OID, review.CommitID)
		})
	})
}
