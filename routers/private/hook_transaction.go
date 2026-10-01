// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package private

import (
	"net/http"

	"forgejo.org/modules/log"
	"forgejo.org/modules/private"
	"forgejo.org/modules/web"
	"forgejo.org/services/context"
	nativeoperation "forgejo.org/services/nativeoperation"
)

// HookReferenceTransaction enforces the native-operation reservation at
// Git's reference-transaction checkpoint. The channel's internal token
// authenticates the hook transport; the execution proof additionally
// identifies the permitted owner. Refusals fail the ref update; errors fail
// closed without exposing secrets.
func HookReferenceTransaction(ctx *context.PrivateContext) {
	opts := web.GetForm(ctx).(*private.TransactionOptions)
	ownerName := ctx.Params(":owner")
	repoName := ctx.Params(":repo")
	if len(opts.OldCommitIDs) != len(opts.NewCommitIDs) || len(opts.OldCommitIDs) != len(opts.RefFullNames) {
		ctx.JSON(http.StatusBadRequest, private.Response{Err: "mismatched transaction inputs"})
		return
	}
	lines := make([]nativeoperation.RefLine, 0, len(opts.OldCommitIDs))
	for i := range opts.OldCommitIDs {
		lines = append(lines, nativeoperation.RefLine{Old: opts.OldCommitIDs[i], New: opts.NewCommitIDs[i], Ref: string(opts.RefFullNames[i])})
	}
	decision, err := nativeoperation.Default().ClassifyTransaction(ctx, nativeoperation.TransactionRequest{
		OwnerName: ownerName,
		RepoName:  repoName,
		Phase:     opts.Phase,
		Lines:     lines,
		Proof:     opts.ExecProof,
	})
	if err != nil {
		log.Error("Reference transaction check failed for %s/%s: %v", ownerName, repoName, err)
		ctx.JSON(http.StatusInternalServerError, private.Response{Err: "transaction check unavailable"})
		return
	}
	if !decision.Allowed {
		ctx.JSON(http.StatusForbidden, private.Response{UserMsg: decision.Reason})
		return
	}
	ctx.PlainText(http.StatusOK, "ok")
}

// checkCompletionBinding binds synchronous post-receive bookkeeping to the
// held reservation owner. It returns false after writing the refusal. Idle
// pushes skip the binding without loading anything new.
func checkCompletionBinding(ctx *context.PrivateContext, opts *private.HookOptions, ownerName, repoName string) bool {
	if len(opts.RefFullNames) == 0 {
		return true
	}
	idle, err := nativeoperation.Default().GateState(ctx)
	if err != nil {
		log.Error("Completion binding check failed for %s/%s: %v", ownerName, repoName, err)
		ctx.JSON(http.StatusInternalServerError, private.HookPostReceiveResult{
			Err: "completion binding unavailable",
		})
		return false
	}
	if idle {
		return true
	}
	repo := loadRepository(ctx, ownerName, repoName)
	if ctx.Written() {
		return false
	}
	refNames := make([]string, 0, len(opts.RefFullNames))
	for _, ref := range opts.RefFullNames {
		refNames = append(refNames, string(ref))
	}
	decision, err := nativeoperation.Default().ClassifyCompletion(ctx, repo.ID, refNames, opts.ExecProof, !opts.GetGitPushOptions().Empty())
	if err != nil {
		log.Error("Completion binding check failed for %s/%s: %v", ownerName, repoName, err)
		ctx.JSON(http.StatusInternalServerError, private.HookPostReceiveResult{
			Err: "completion binding unavailable",
		})
		return false
	}
	if !decision.Allowed {
		ctx.JSON(http.StatusForbidden, private.HookPostReceiveResult{
			Err: decision.Reason,
		})
		return false
	}
	return true
}
