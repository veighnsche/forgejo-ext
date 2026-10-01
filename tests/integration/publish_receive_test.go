// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdk "forgejo.org/extension-sdk"
	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/extensionauth"
	issues_model "forgejo.org/models/issues"
	nativeop_model "forgejo.org/models/nativeoperation"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"
	api "forgejo.org/modules/structs"
	"forgejo.org/services/extensions"
	operation_service "forgejo.org/services/nativeoperation"

	gouuid "github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestPublishReceive is the authoritative F-publish proof: a registered
// git.ref.publish intent drives exactly one bound smart-HTTP receive-pack
// through the shipping owners, and every divergence refuses without
// guessing. Subtests push genuine new objects the server has never seen,
// bind them to installation/actor/operation, and reconcile receipts with
// refs+SHAs. Cancellation orderings, lost-reply replay, restart with fresh
// admission, protection effectiveness, PAT-only non-consumption and
// kind-specific offline recovery are all exercised on the live receive.
func TestPublishReceive(t *testing.T) {
	onApplicationRun(t, func(t *testing.T, giteaURL *url.URL) {
		session := loginUser(t, "user1")
		testRepoFork(t, session, "user2", "repo1", "user1", "repo1")
		token := getTokenForLoggedInUser(t, session, auth_model.AccessTokenScopeWriteRepository, auth_model.AccessTokenScopeWriteIssue)

		user1 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
		repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{OwnerID: 1, Name: "repo1"})
		svc := operation_service.Default()
		ctx := t.Context()

		tokenRow, err := auth_model.GetAccessTokenBySHA(ctx, token)
		require.NoError(t, err)
		decision := extensionauth.SubmissionDecision{
			TokenID:               tokenRow.ID,
			ActorID:               user1.ID,
			CredentialFingerprint: extensionauth.CredentialFingerprint(tokenRow.TokenHash, tokenRow.TokenSalt),
		}
		// The integration server starts no extension manager; the proof
		// installs a real admission registry (unstarted manager: no
		// extension processes) as the installation-admission authority
		// the receive route verifies against.
		manager := extensions.NewManager(t.TempDir())
		extensions.SetDefault(manager)
		t.Cleanup(func() { extensions.SetDefault(nil) })

		serverRepo, err := git.OpenRepository(git.DefaultContext, repo_model.RepoPath(user1.Name, repo.Name))
		require.NoError(t, err)
		defer serverRepo.Close()

		zeroOID := git.Sha1ObjectFormat.EmptyObjectID().String()

		// waitIdle polls for writer quiescence: an idle reservation with
		// a revision stable across two observations. Async push
		// completion advances the revision after the push returns, so a
		// single read cannot anchor an exact-revision assertion.
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

		enroll := func(t *testing.T, installation string) {
			t.Helper()
			_, err := extensionauth.EnrollBinding(ctx, installation, tokenRow.ID, user1.ID, repo.ID, extensionauth.KindRefPublish)
			require.NoError(t, err)
		}

		admit := func(t *testing.T, installation string) string {
			t.Helper()
			admission, err := manager.IssueRuntimeAdmission(installation, "ft11-proof", []string{sdk.CapabilityBackgroundOperations})
			require.NoError(t, err)
			return admission
		}

		submit := func(t *testing.T, installation, opID, payload string) {
			t.Helper()
			revision := waitIdle(t)
			now := time.Now().Unix()
			intent, err := operation_service.ValidateIntent(opID, user1.ID, repo.ID, nativeop_model.KindRefPublish,
				"auth-rev-1", revision, now+600, []byte(payload), now)
			require.NoError(t, err)
			record, err := svc.Submit(ctx, decision, installation, intent)
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectPending, record.Outcome)
		}

		get := func(t *testing.T, installation, opID string) nativeop_model.Operation {
			t.Helper()
			found, err := nativeop_model.LookupOperation(ctx, installation, opID)
			require.NoError(t, err)
			require.NotNil(t, found)
			return *found
		}

		clone := func(t *testing.T) string {
			t.Helper()
			dir := t.TempDir()
			u := *giteaURL
			u.Path = "user1/repo1.git"
			u.User = url.UserPassword("user1", token)
			doGitClone(dir, &u)(t)
			return dir
		}

		// commitFile commits one new file on a new local branch and
		// returns the new commit ID. The commit is local-only: the
		// server has never seen its objects.
		commitFile := func(t *testing.T, dir, branch, filename, content string) string {
			t.Helper()
			doGitCreateBranch(dir, branch)(t)
			require.NoError(t, os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644))
			require.NoError(t, git.AddChanges(dir, true))
			signature := git.Signature{Email: "soda-tester@example.test", Name: "soda-tester"}
			require.NoError(t, git.CommitChanges(dir, git.CommitChangesOptions{
				Committer: &signature, Author: &signature, Message: "publish candidate " + branch,
			}))
			local, err := git.OpenRepository(git.DefaultContext, dir)
			require.NoError(t, err)
			defer local.Close()
			oid, err := local.GetBranchCommitID(branch)
			require.NoError(t, err)
			return oid
		}

		push := func(t *testing.T, dir string, env []string, args ...string) (string, error) {
			t.Helper()
			fullEnv := append(os.Environ(), env...)
			stdout, stderr, err := git.NewCommand(git.DefaultContext, "push").AddArguments(git.ToTrustedCmdArgs(args)...).RunStdString(&git.RunOpts{Dir: dir, Env: fullEnv})
			return stdout + stderr, err
		}

		pushBound := func(t *testing.T, dir, opID, admission string, args ...string) (string, error) {
			t.Helper()
			env, err := sdk.PublishPushEnv(opID, admission)
			require.NoError(t, err)
			return push(t, dir, env, args...)
		}

		serverTip := func(t *testing.T, ref string) (string, bool) {
			t.Helper()
			oid, err := serverRepo.GetRefCommitID(ref)
			if err != nil {
				require.True(t, git.IsErrNotExist(err), "unexpected tip error: %v", err)
				return "", false
			}
			return oid, true
		}

		payload := func(ref, old, new, baseRef, base, correction string) string {
			body := fmt.Sprintf(`{"ref":%q,"expected_old":%q,"new_oid":%q,`+
				`"comparison_ref":%q,"expected_comparison_oid":%q`, ref, old, new, baseRef, base)
			if correction != "" {
				body += `,"pull_request":` + correction
			}
			return body + `}`
		}

		master := func(t *testing.T) string {
			t.Helper()
			oid, ok := serverTip(t, "refs/heads/master")
			require.True(t, ok)
			return oid
		}

		requireIdle := func(t *testing.T) {
			t.Helper()
			observation, err := svc.ReadNativeRevision(ctx)
			require.NoError(t, err)
			require.True(t, observation.Idle)
		}

		waitIdle(t)

		t.Run("CreationCommitsNewObjects", func(t *testing.T) {
			installation := gouuid.NewString()
			enroll(t, installation)
			admission := admit(t, installation)
			dir := clone(t)
			newOID := commitFile(t, dir, "pub-rx-new", "pub-rx-new.txt", "candidate\n")
			opID := "op-pub-rx-create"
			submit(t, installation, opID, payload("refs/heads/pub-rx-new", "absent", newOID, "refs/heads/master", master(t), ""))

			out, err := pushBound(t, dir, opID, admission, "origin", "pub-rx-new:refs/heads/pub-rx-new")
			require.NoError(t, err, "bound push failed: %s", out)

			op := get(t, installation, opID)
			require.Equal(t, nativeop_model.EffectCommitted, op.EffectState)
			require.Equal(t, nativeop_model.CompletionComplete, op.Completion)
			var receipt operation_service.PublishReceipt
			require.NoError(t, json.Unmarshal([]byte(op.Receipt), &receipt))
			require.Equal(t, "refs/heads/pub-rx-new", receipt.Ref)
			require.Equal(t, zeroOID, receipt.OldOID)
			require.Equal(t, strings.ToLower(newOID), receipt.NewOID)
			require.Equal(t, "refs/heads/master", receipt.ComparisonRef)
			require.Equal(t, user1.ID, receipt.ActorID)
			require.Zero(t, receipt.PRID)

			tip, ok := serverTip(t, "refs/heads/pub-rx-new")
			require.True(t, ok)
			require.Equal(t, strings.ToLower(newOID), strings.ToLower(tip))
			waitIdle(t)
			requireIdle(t)
		})

		t.Run("CorrectionUpdateCommits", func(t *testing.T) {
			installation := gouuid.NewString()
			enroll(t, installation)
			admission := admit(t, installation)
			dir := clone(t)
			// The recorded prior candidate rides an ordinary branch; the
			// correction advances it with objects the server never saw.
			baseOID := commitFile(t, dir, "pub-rx-corr", "pub-rx-corr.txt", "prior\n")
			_, err := push(t, dir, nil, "origin", "pub-rx-corr:refs/heads/pub-rx-corr")
			require.NoError(t, err)
			waitIdle(t)

			prReq := NewRequestWithJSON(t, "POST", "/api/v1/repos/user1/repo1/pulls", &api.CreatePullRequestOption{
				Head: "pub-rx-corr", Base: "master", Title: "correction candidate",
			}).AddTokenAuth(token)
			prResp := session.MakeRequest(t, prReq, 201)
			var pr api.PullRequest
			DecodeJSON(t, prResp, &pr)
			require.False(t, pr.HasMerged)

			// The correction branches from the published prior tip, so it
			// fast-forwards; its objects are local-only until the push.
			doGitCheckoutBranch(dir, "pub-rx-corr")(t)
			nextOID := commitFile(t, dir, "pub-rx-corr-next", "pub-rx-corr.txt", "corrected\n")
			_ = baseOID
			tip, ok := serverTip(t, "refs/heads/pub-rx-corr")
			require.True(t, ok)
			opID := "op-pub-rx-corr"
			submit(t, installation, opID, payload("refs/heads/pub-rx-corr", tip, nextOID, "refs/heads/master", master(t),
				fmt.Sprintf(`{"number":%d,"expected_author_id":%d}`, pr.Index, user1.ID)))

			out, err := pushBound(t, dir, opID, admission, "origin", "pub-rx-corr-next:refs/heads/pub-rx-corr")
			require.NoError(t, err, "bound correction push failed: %s", out)

			op := get(t, installation, opID)
			require.Equal(t, nativeop_model.EffectCommitted, op.EffectState)
			var receipt operation_service.PublishReceipt
			require.NoError(t, json.Unmarshal([]byte(op.Receipt), &receipt))
			prRow, err := issues_model.GetPullRequestByIndex(ctx, repo.ID, pr.Index)
			require.NoError(t, err)
			require.Equal(t, prRow.ID, receipt.PRID)
			require.Equal(t, prRow.IssueID, receipt.IssueID)
			require.Equal(t, strings.ToLower(tip), receipt.OldOID)
			waitIdle(t)
			requireIdle(t)
		})

		t.Run("ExtraRefRefused", func(t *testing.T) {
			installation := gouuid.NewString()
			enroll(t, installation)
			admission := admit(t, installation)
			dir := clone(t)
			newOID := commitFile(t, dir, "pub-rx-extra", "pub-rx-extra.txt", "candidate\n")
			otherOID := commitFile(t, dir, "pub-rx-extra-other", "pub-rx-extra-other.txt", "other\n")
			_ = otherOID
			opID := "op-pub-rx-extra"
			submit(t, installation, opID, payload("refs/heads/pub-rx-extra", "absent", newOID, "refs/heads/master", master(t), ""))

			out, err := pushBound(t, dir, opID, admission, "origin",
				"pub-rx-extra:refs/heads/pub-rx-extra", "pub-rx-extra-other:refs/heads/pub-rx-extra-other")
			require.Error(t, err, "multi-ref bound push must fail: %s", out)

			op := get(t, installation, opID)
			require.Equal(t, nativeop_model.EffectNotCommitted, op.EffectState)
			require.Equal(t, nativeop_model.ReasonUnexpectedRefEffects, op.Reason)
			_, ok := serverTip(t, "refs/heads/pub-rx-extra")
			require.False(t, ok)
			_, ok = serverTip(t, "refs/heads/pub-rx-extra-other")
			require.False(t, ok)
			waitIdle(t)
			requireIdle(t)
		})

		t.Run("StaleOldRefused", func(t *testing.T) {
			installation := gouuid.NewString()
			enroll(t, installation)
			admission := admit(t, installation)
			dir := clone(t)
			commitFile(t, dir, "pub-rx-stale", "pub-rx-stale.txt", "prior\n")
			_, err := push(t, dir, nil, "origin", "pub-rx-stale:refs/heads/pub-rx-stale")
			require.NoError(t, err)
			waitIdle(t)
			staleTip, ok := serverTip(t, "refs/heads/pub-rx-stale")
			require.True(t, ok)

			doGitCheckoutBranch(dir, "pub-rx-stale")(t)
			candidateOID := commitFile(t, dir, "pub-rx-stale-next", "pub-rx-stale.txt", "candidate\n")
			opID := "op-pub-rx-stale"
			submit(t, installation, opID, payload("refs/heads/pub-rx-stale", staleTip, candidateOID, "refs/heads/master", master(t), ""))

			// An ordinary push wins the tip first; the bound push must
			// refuse against the changed tip without claiming.
			doGitCheckoutBranch(dir, "pub-rx-stale")(t)
			commitFile(t, dir, "pub-rx-stale-winner", "pub-rx-stale.txt", "winner\n")
			_, err = push(t, dir, nil, "origin", "pub-rx-stale-winner:refs/heads/pub-rx-stale")
			require.NoError(t, err)
			waitIdle(t)
			winnerTip, ok := serverTip(t, "refs/heads/pub-rx-stale")
			require.True(t, ok)
			require.NotEqual(t, strings.ToLower(staleTip), strings.ToLower(winnerTip))

			// --force carries the RPC to the server gate: without it the
			// git client short-circuits the divergent push after the
			// advertisement and the server never decides.
			revBefore := waitIdle(t)
			out, err := pushBound(t, dir, opID, admission, "--force", "origin", "pub-rx-stale-next:refs/heads/pub-rx-stale")
			require.Error(t, err, "stale bound push must fail: %s", out)
			op := get(t, installation, opID)
			require.Equal(t, nativeop_model.EffectNotCommitted, op.EffectState)
			require.Equal(t, nativeop_model.ReasonStaleHead, op.Reason)
			tip, ok := serverTip(t, "refs/heads/pub-rx-stale")
			require.True(t, ok)
			require.Equal(t, strings.ToLower(winnerTip), strings.ToLower(tip))
			revAfter := waitIdle(t)
			require.Equal(t, revBefore, revAfter, "refused pre-launch push must not claim")
			requireIdle(t)
		})

		t.Run("NonFastForwardRefused", func(t *testing.T) {
			installation := gouuid.NewString()
			enroll(t, installation)
			admission := admit(t, installation)
			dir := clone(t)
			commitFile(t, dir, "pub-rx-ff", "pub-rx-ff.txt", "prior\n")
			_, err := push(t, dir, nil, "origin", "pub-rx-ff:refs/heads/pub-rx-ff")
			require.NoError(t, err)
			waitIdle(t)
			tip, ok := serverTip(t, "refs/heads/pub-rx-ff")
			require.True(t, ok)

			// A diverged candidate from another base: forced or not, the
			// publish gate refuses it as non-fast-forward.
			doGitCheckoutBranch(dir, "master")(t)
			divergedOID := commitFile(t, dir, "pub-rx-ff-diverged", "pub-rx-ff.txt", "diverged\n")
			opID := "op-pub-rx-ff"
			submit(t, installation, opID, payload("refs/heads/pub-rx-ff", tip, divergedOID, "refs/heads/master", master(t), ""))

			out, err := pushBound(t, dir, opID, admission, "--force", "origin", "pub-rx-ff-diverged:refs/heads/pub-rx-ff")
			require.Error(t, err, "forced non-FF bound push must fail: %s", out)
			op := get(t, installation, opID)
			require.Equal(t, nativeop_model.EffectNotCommitted, op.EffectState)
			require.Equal(t, nativeop_model.ReasonNotFastForward, op.Reason)
			kept, ok := serverTip(t, "refs/heads/pub-rx-ff")
			require.True(t, ok)
			require.Equal(t, strings.ToLower(tip), strings.ToLower(kept))
			waitIdle(t)
			requireIdle(t)
		})

		t.Run("ProtectionEffective", func(t *testing.T) {
			installation := gouuid.NewString()
			enroll(t, installation)
			admission := admit(t, installation)

			ruleReq := NewRequestWithJSON(t, "POST", "/api/v1/repos/user1/repo1/branch_protections", &api.BranchProtection{
				RuleName: "pub-rx-prot",
			}).AddTokenAuth(token)
			session.MakeRequest(t, ruleReq, 201)
			waitIdle(t)

			dir := clone(t)
			newOID := commitFile(t, dir, "pub-rx-prot", "pub-rx-prot.txt", "candidate\n")
			opID := "op-pub-rx-prot"
			submit(t, installation, opID, payload("refs/heads/pub-rx-prot", "absent", newOID, "refs/heads/master", master(t), ""))

			out, err := pushBound(t, dir, opID, admission, "origin", "pub-rx-prot:refs/heads/pub-rx-prot")
			require.Error(t, err, "protected bound push must fail: %s", out)
			op := get(t, installation, opID)
			require.Equal(t, nativeop_model.EffectNotCommitted, op.EffectState)
			require.Equal(t, nativeop_model.ReasonNativeRefused, op.Reason)
			_, ok := serverTip(t, "refs/heads/pub-rx-prot")
			require.False(t, ok)
			waitIdle(t)
			requireIdle(t)
		})

		t.Run("PATOnlyCannotConsume", func(t *testing.T) {
			installation := gouuid.NewString()
			enroll(t, installation)
			admission := admit(t, installation)
			dir := clone(t)
			newOID := commitFile(t, dir, "pub-rx-pat", "pub-rx-pat.txt", "candidate\n")
			opID := "op-pub-rx-pat"
			submit(t, installation, opID, payload("refs/heads/pub-rx-pat", "absent", newOID, "refs/heads/master", master(t), ""))

			// An ordinary PAT-only push succeeds as an ordinary native
			// operation and leaves the registration waiting.
			commitFile(t, dir, "pub-rx-pat-other", "pub-rx-pat-other.txt", "ordinary\n")
			out, err := push(t, dir, nil, "origin", "pub-rx-pat-other:refs/heads/pub-rx-pat-other")
			require.NoError(t, err, "ordinary push failed: %s", out)
			waitIdle(t)
			op := get(t, installation, opID)
			require.Equal(t, nativeop_model.EffectPending, op.EffectState)

			// The ordinary push advanced the shared revision, so the
			// bound push now refuses stale rather than adopting it.
			out, err = pushBound(t, dir, opID, admission, "origin", "pub-rx-pat:refs/heads/pub-rx-pat")
			require.Error(t, err, "stale bound push must fail: %s", out)
			op = get(t, installation, opID)
			require.Equal(t, nativeop_model.EffectNotCommitted, op.EffectState)
			require.Equal(t, nativeop_model.ReasonStaleNativeRevision, op.Reason)
			waitIdle(t)
			requireIdle(t)
		})

		t.Run("CancelBeforeReceive", func(t *testing.T) {
			installation := gouuid.NewString()
			enroll(t, installation)
			admission := admit(t, installation)
			dir := clone(t)
			newOID := commitFile(t, dir, "pub-rx-cancel", "pub-rx-cancel.txt", "candidate\n")
			opID := "op-pub-rx-cancel"
			submit(t, installation, opID, payload("refs/heads/pub-rx-cancel", "absent", newOID, "refs/heads/master", master(t), ""))

			cancelled, err := svc.Cancel(ctx, installation, opID)
			require.NoError(t, err)
			require.Equal(t, nativeop_model.EffectPending, cancelled.Outcome)

			revBefore := waitIdle(t)
			out, err := pushBound(t, dir, opID, admission, "origin", "pub-rx-cancel:refs/heads/pub-rx-cancel")
			require.Error(t, err, "cancelled bound push must fail: %s", out)
			op := get(t, installation, opID)
			require.Equal(t, nativeop_model.EffectNotCommitted, op.EffectState)
			require.Equal(t, nativeop_model.ReasonCancelledBeforeAdmission, op.Reason)
			_, ok := serverTip(t, "refs/heads/pub-rx-cancel")
			require.False(t, ok)
			require.Equal(t, revBefore, waitIdle(t))
			requireIdle(t)
		})

		t.Run("CancelDuringPrepared", func(t *testing.T) {
			installation := gouuid.NewString()
			enroll(t, installation)
			admission := admit(t, installation)
			dir := clone(t)
			newOID := commitFile(t, dir, "pub-rx-race", "pub-rx-race.txt", "candidate\n")
			opID := "op-pub-rx-race"
			submit(t, installation, opID, payload("refs/heads/pub-rx-race", "absent", newOID, "refs/heads/master", master(t), ""))

			// The disclosed admission barrier pauses the atomic
			// prepared decision so cancellation wins the ordering.
			barrier := t.TempDir()
			t.Setenv("NATIVEOP_TEST_ADMISSION_BARRIER", barrier)
			type pushResult struct {
				out string
				err error
			}
			done := make(chan pushResult, 1)
			go func() {
				out, err := pushBound(t, dir, opID, admission, "origin", "pub-rx-race:refs/heads/pub-rx-race")
				done <- pushResult{out: out, err: err}
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
			_, err := svc.Cancel(ctx, installation, opID)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(barrier, "admission.release"), []byte("go\n"), 0o600))
			var result pushResult
			select {
			case result = <-done:
			case <-time.After(30 * time.Second):
				t.Fatal("bound push never returned after release")
			}
			require.Error(t, result.err, "cancelled race push must fail: %s", result.out)
			op := get(t, installation, opID)
			require.Equal(t, nativeop_model.EffectNotCommitted, op.EffectState)
			require.Equal(t, nativeop_model.ReasonCancelledBeforeAdmission, op.Reason)
			_, ok := serverTip(t, "refs/heads/pub-rx-race")
			require.False(t, ok)
			waitIdle(t)
			requireIdle(t)
		})

		t.Run("LostReplyNoDuplicateReceiver", func(t *testing.T) {
			installation := gouuid.NewString()
			enroll(t, installation)
			admission := admit(t, installation)
			dir := clone(t)
			newOID := commitFile(t, dir, "pub-rx-replay", "pub-rx-replay.txt", "candidate\n")
			opID := "op-pub-rx-replay"
			submit(t, installation, opID, payload("refs/heads/pub-rx-replay", "absent", newOID, "refs/heads/master", master(t), ""))

			out, err := pushBound(t, dir, opID, admission, "origin", "pub-rx-replay:refs/heads/pub-rx-replay")
			require.NoError(t, err, "bound push failed: %s", out)
			revBefore := waitIdle(t)
			first := get(t, installation, opID)
			require.Equal(t, nativeop_model.EffectCommitted, first.EffectState)

			// A replay of the operation launches no second receiver: it
			// refuses while the earlier receipt stays the answer. The
			// replay carries a different tip so git issues the RPC
			// instead of reporting up-to-date without contacting the
			// route; the server refuses on the recorded outcome.
			doGitCheckoutBranch(dir, "pub-rx-replay")(t)
			commitFile(t, dir, "pub-rx-replay-again", "pub-rx-replay.txt", "replay\n")
			out, err = pushBound(t, dir, opID, admission, "origin", "pub-rx-replay-again:refs/heads/pub-rx-replay")
			require.Error(t, err, "replay push must fail: %s", out)
			second := get(t, installation, opID)
			require.Equal(t, nativeop_model.EffectCommitted, second.EffectState)
			require.Equal(t, first.Receipt, second.Receipt)
			require.Equal(t, revBefore, waitIdle(t), "replay must not claim")
			tip, ok := serverTip(t, "refs/heads/pub-rx-replay")
			require.True(t, ok)
			require.Equal(t, strings.ToLower(newOID), strings.ToLower(tip))
			requireIdle(t)
		})

		t.Run("RestartFreshAdmission", func(t *testing.T) {
			installation := gouuid.NewString()
			enroll(t, installation)
			_ = admit(t, installation)
			dir := clone(t)
			newOID := commitFile(t, dir, "pub-rx-restart", "pub-rx-restart.txt", "candidate\n")
			opID := "op-pub-rx-restart"
			submit(t, installation, opID, payload("refs/heads/pub-rx-restart", "absent", newOID, "refs/heads/master", master(t), ""))

			// A caller restart bootstraps fresh installation admission
			// and attaches to the still-waiting intent.
			fresh, err := manager.IssueRuntimeAdmission(installation, "ft11-proof-restart", []string{sdk.CapabilityBackgroundOperations})
			require.NoError(t, err)
			out, err := pushBound(t, dir, opID, fresh, "origin", "pub-rx-restart:refs/heads/pub-rx-restart")
			require.NoError(t, err, "restarted bound push failed: %s", out)
			op := get(t, installation, opID)
			require.Equal(t, nativeop_model.EffectCommitted, op.EffectState)
			waitIdle(t)
			requireIdle(t)
		})

		t.Run("HalfBindingRefuses", func(t *testing.T) {
			installation := gouuid.NewString()
			enroll(t, installation)
			_ = admit(t, installation)
			dir := clone(t)
			newOID := commitFile(t, dir, "pub-rx-half", "pub-rx-half.txt", "candidate\n")
			opID := "op-pub-rx-half"
			submit(t, installation, opID, payload("refs/heads/pub-rx-half", "absent", newOID, "refs/heads/master", master(t), ""))

			env := []string{
				"GIT_CONFIG_COUNT=1",
				"GIT_CONFIG_KEY_0=http.extraHeader",
				"GIT_CONFIG_VALUE_0=X-Forgejo-Operation: " + opID,
			}
			out, err := push(t, dir, env, "origin", "pub-rx-half:refs/heads/pub-rx-half")
			require.Error(t, err, "half-bound push must fail: %s", out)
			op := get(t, installation, opID)
			require.Equal(t, nativeop_model.EffectPending, op.EffectState)
			_, ok := serverTip(t, "refs/heads/pub-rx-half")
			require.False(t, ok)
			waitIdle(t)
			requireIdle(t)
		})

		t.Run("RecoveryReleasesCommitted", func(t *testing.T) {
			installation := gouuid.NewString()
			enroll(t, installation)
			opID := "op-pub-rx-recov-commit"
			submit(t, installation, opID, payload("refs/heads/pub-rx-recov", "absent", master(t), "refs/heads/master", master(t), ""))

			prepared, outcome, err := svc.PreparePublishReceive(ctx, operation_service.PublishReceiveRequest{
				InstallationID: installation, OperationID: opID,
				ActorID: user1.ID, TokenSecret: token, RepositoryID: repo.ID,
			})
			require.NoError(t, err)
			require.Nil(t, outcome)
			require.NotNil(t, prepared)

			// Crash between admission and reconciliation: the prepared
			// decision committed through the atomic primitive, the ref
			// moved without hooks, and reconcile never ran.
			_, admitted, err := nativeop_model.RecordAdmissionAttempt(ctx, installation, opID, prepared.Execution.Owner, true, "")
			require.NoError(t, err)
			require.True(t, admitted)
			hookless := append(os.Environ(),
				"GIT_CONFIG_COUNT=1",
				"GIT_CONFIG_KEY_0=core.hooksPath",
				"GIT_CONFIG_VALUE_0=/dev/null",
			)
			_, _, err = git.NewCommand(ctx, "update-ref").AddDynamicArguments("refs/heads/pub-rx-recov", master(t), zeroOID).
				RunStdString(&git.RunOpts{Dir: repo.RepoPath(), Env: hookless})
			require.NoError(t, err)

			marker := nativeop_model.OfflineMarkerPath()
			require.NoError(t, os.WriteFile(marker, []byte("recovery\n"), 0o600))
			defer os.Remove(marker)
			assessment, err := svc.Recover(ctx, prepared.Execution.Owner, prepared.Execution.Generation)
			require.NoError(t, err)
			require.Equal(t, operation_service.RecoveryReleased, assessment.Verdict)
			require.Equal(t, nativeop_model.EffectCommitted, assessment.Effect)
			op := get(t, installation, opID)
			require.Equal(t, nativeop_model.EffectCommitted, op.EffectState)
			var receipt operation_service.PublishReceipt
			require.NoError(t, json.Unmarshal([]byte(op.Receipt), &receipt))
			require.Equal(t, "refs/heads/pub-rx-recov", receipt.Ref)
			prepared.RetireCapability()
			require.NoError(t, os.Remove(marker))
			requireIdle(t)
		})

		t.Run("RecoveryReleasesRefused", func(t *testing.T) {
			installation := gouuid.NewString()
			enroll(t, installation)
			opID := "op-pub-rx-recov-refused"
			submit(t, installation, opID, payload("refs/heads/pub-rx-recov-absent", "absent", master(t), "refs/heads/master", master(t), ""))

			prepared, outcome, err := svc.PreparePublishReceive(ctx, operation_service.PublishReceiveRequest{
				InstallationID: installation, OperationID: opID,
				ActorID: user1.ID, TokenSecret: token, RepositoryID: repo.ID,
			})
			require.NoError(t, err)
			require.Nil(t, outcome)
			require.NotNil(t, prepared)

			// Crash before any ref effect: unadmitted with the branch
			// still absent reconciles as not_committed.
			marker := nativeop_model.OfflineMarkerPath()
			require.NoError(t, os.WriteFile(marker, []byte("recovery\n"), 0o600))
			defer os.Remove(marker)
			assessment, err := svc.Recover(ctx, prepared.Execution.Owner, prepared.Execution.Generation)
			require.NoError(t, err)
			require.Equal(t, operation_service.RecoveryReleased, assessment.Verdict)
			require.Equal(t, nativeop_model.EffectNotCommitted, assessment.Effect)
			op := get(t, installation, opID)
			require.Equal(t, nativeop_model.EffectNotCommitted, op.EffectState)
			require.Equal(t, nativeop_model.ReasonRecoveredNoEffect, op.Reason)
			prepared.RetireCapability()
			require.NoError(t, os.Remove(marker))
			requireIdle(t)
		})
	})
}
