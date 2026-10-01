// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"context"

	"forgejo.org/models/db"
)

// CommitPRCreatePrimary atomically orders one pull_request.create primary
// commit against cancellation and expiry, runs insert, and records the
// committed receipt while the owner stays held for bounded completion. The
// caller verifies exact refs and native authority under the held reservation
// before calling; this transaction performs SQL only, never Git.
//
// The operation must still be pending, unrevoked, unadmitted and unexpired,
// and owner must still hold the reservation, or nothing is inserted. The
// final receipt update re-checks pending/unrevoked conditionally on the same
// operation row, so a concurrent cancellation either wins (nothing commits)
// or loses (the committed row reads too_late); it can never acknowledge
// prevention while this transaction commits. An insert failure rolls the
// whole transaction back: no partial PR rows survive and the operation row
// is untouched for the caller to refuse.
func CommitPRCreatePrimary(ctx context.Context, installationID, operationID, owner string, nowUnix int64, insert func(ctx context.Context) (receipt string, err error)) (*Operation, error) {
	var recorded *Operation
	err := db.WithTx(ctx, func(ctx context.Context) error {
		op, err := LookupOperation(ctx, installationID, operationID)
		if err != nil {
			return err
		}
		if op == nil || !op.Submitted || op.EffectState != EffectPending {
			return ErrAdmissionLost
		}
		if op.Revoked || op.Admitted {
			return ErrAdmissionLost
		}
		if op.NotAfter <= nowUnix {
			return ErrOperationExpired
		}
		reservation, err := ReadReservation(ctx)
		if err != nil {
			return err
		}
		if reservation.Owner != owner {
			return ErrWrongOwner
		}
		receipt, err := insert(ctx)
		if err != nil {
			return err
		}
		op.Admitted = true
		op.EffectState = EffectCommitted
		op.Cancellation = CancellationNone
		op.Completion = CompletionPending
		op.Receipt = receipt
		affected, err := db.GetEngine(ctx).Where(
			"installation_id=? AND operation_id=? AND effect_state=? AND revoked=?",
			installationID, operationID, EffectPending, false,
		).Cols("admitted", "effect_state", "cancellation", "completion", "receipt").Update(op)
		if err != nil {
			return err
		}
		if affected == 0 {
			return ErrAdmissionLost
		}
		recorded = op
		return nil
	})
	if err != nil {
		return nil, err
	}
	return recorded, nil
}

// SetCompletionAndRelease records the completion state of one committed
// conditional operation and releases its exact owner/generation in the same
// transaction. Only a committed operation whose completion is still pending
// (or unset) finalizes; anything else fails closed without touching the
// reservation. Offline recovery uses the same helper after establishing the
// known effect.
func SetCompletionAndRelease(ctx context.Context, installationID, operationID, owner string, generation int64, completion string) (*Operation, error) {
	if owner == "" || generation <= 0 {
		return nil, ErrWrongOwner
	}
	switch completion {
	case CompletionComplete, CompletionNeedsIntervention:
	default:
		return nil, ErrAdmissionLost
	}
	var recorded *Operation
	err := db.WithTx(ctx, func(ctx context.Context) error {
		op, err := LookupOperation(ctx, installationID, operationID)
		if err != nil {
			return err
		}
		if op == nil || op.EffectState != EffectCommitted {
			return ErrAdmissionLost
		}
		if op.Completion != CompletionPending && op.Completion != "" {
			return ErrAdmissionLost
		}
		op.Completion = completion
		if _, err := db.GetEngine(ctx).ID(op.ID).Cols("completion").Update(op); err != nil {
			return err
		}
		affected, err := db.GetEngine(ctx).Where("id=1 AND owner=? AND generation=?", owner, generation).Cols("owner", "owner_kind", "verifier", "scope").Update(&Reservation{})
		if err != nil {
			return err
		}
		if affected == 0 {
			return ErrWrongOwner
		}
		recorded = op
		return nil
	})
	if err != nil {
		return recorded, err
	}
	return recorded, nil
}
