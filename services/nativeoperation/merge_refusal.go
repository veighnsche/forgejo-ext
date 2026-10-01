// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"errors"

	"forgejo.org/models"
	model "forgejo.org/models/nativeoperation"
	"forgejo.org/modules/git"
	pull_service "forgejo.org/services/pull"
)

// mapExactMergeRefusal maps a deterministic exact-merge refusal to its bounded
// operation reason. Changed or missing candidates refuse as stale; identity,
// source, target and lifecycle mismatches refuse as PR mismatches; native
// policy, mergeability and engine outcomes refuse as native refusals. The
// caller only applies this to pre-write refusals: the result-mismatch reason
// is unattributable and never maps here.
func mapExactMergeRefusal(err error) string {
	var refused pull_service.ErrExactMergeRefused
	if !errors.As(err, &refused) {
		return model.ReasonNativeRefused
	}
	switch refused.Reason {
	case pull_service.ExactMergeRefusedStaleHead,
		pull_service.ExactMergeRefusedMissingHead:
		return model.ReasonStaleHead
	case pull_service.ExactMergeRefusedStaleBase,
		pull_service.ExactMergeRefusedMissingBase,
		pull_service.ExactMergeRefusedNoOp:
		return model.ReasonStaleBaseOrResult
	case pull_service.ExactMergeRefusedCrossRepo,
		pull_service.ExactMergeRefusedPRMismatch,
		pull_service.ExactMergeRefusedClosedOrMerged:
		return model.ReasonPRMismatch
	default:
		// Malformed refs/OIDs are unreachable from a validated
		// intent, a disallowed method is current native policy, and
		// any future reason fails closed: all are native refusals.
		return model.ReasonNativeRefused
	}
}

// mapMergeEngineError maps a merge-engine or mergeability outcome to the hint
// reason reconciliation reports when the authoritative tip evidence shows no
// effect. Deterministic pre-push outcomes classify precisely: divergence and
// a raced base are not_fast_forward, a head that moved under the engine is
// stale_head. Everything else — protection, mergeable state, permission and
// infrastructure — is a native refusal. The hint never overrides tip
// evidence: a committed or moved tip reconciles from admission and the tip,
// never from this classification.
func mapMergeEngineError(err error) string {
	switch {
	case models.IsErrMergeDivergingFastForwardOnly(err), git.IsErrPushOutOfDate(err):
		return model.ReasonNotFastForward
	case models.IsErrSHADoesNotMatch(err):
		return model.ReasonStaleHead
	default:
		return model.ReasonNativeRefused
	}
}
