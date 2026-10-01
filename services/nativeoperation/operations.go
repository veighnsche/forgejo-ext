// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"context"
	"errors"

	sdk "forgejo.org/extension-sdk"
	authmodel "forgejo.org/models/extensionauth"
	model "forgejo.org/models/nativeoperation"
)

// Submit authenticates the installation/principal (already verified by the
// dispatcher into decision), validates the complete intent and durably claims
// its operation ID before native execution. The same ID and identical intent
// return or reconcile the existing record, never start a second execution.
// Different content under the same ID is intent_conflict.
func (s *Service) Submit(ctx context.Context, decision authmodel.SubmissionDecision, installationID string, intent *ValidIntent) (sdk.OperationRecord, error) {
	if intent == nil || intent.Kind == "" {
		return sdk.OperationRecord{}, ErrInvalidIntent
	}
	switch intent.Kind {
	case model.KindMerge, model.KindRefPublish, model.KindPRCreate, model.KindReviewSubmit:
	default:
		// Admitted, but the kind's stage is not implemented yet. Report
		// the missing stage rather than inventing a receipt.
		return sdk.OperationRecord{}, ErrKindUnavailable
	}
	existing, err := model.LookupOperation(ctx, installationID, intent.OperationID)
	if err != nil {
		return sdk.OperationRecord{}, err
	}
	if existing != nil {
		return s.reconcileExisting(existing, intent)
	}
	if intent.Kind == model.KindRefPublish {
		return s.submitPublish(ctx, decision, installationID, intent)
	}
	if intent.Kind == model.KindPRCreate {
		return s.submitPRCreate(ctx, decision, installationID, intent)
	}
	if intent.Kind == model.KindReviewSubmit {
		return s.submitReviewSubmit(ctx, decision, installationID, intent)
	}
	return s.submitMerge(ctx, decision, installationID, intent)
}

func (s *Service) reconcileExisting(existing *model.Operation, intent *ValidIntent) (sdk.OperationRecord, error) {
	if !existing.Submitted {
		return ToRecord(existing), ErrCancelledBeforeSubmit
	}
	if existing.IntentDigest != intent.Digest {
		return sdk.OperationRecord{}, ErrIntentConflict
	}
	// Identical replay: return the recorded outcome without relaunching.
	return ToRecord(existing), nil
}

// Get returns the recorded intent, effect and native completion evidence for
// the owning installation. Absence is not_observed, not proof that an
// earlier request cannot still arrive or that a write never occurred. Lookup
// performs no new mutation.
func (s *Service) Get(ctx context.Context, installationID, operationID string) (sdk.OperationLookup, error) {
	if !validOperationID(operationID) {
		return sdk.OperationLookup{}, ErrInvalidIntent
	}
	op, err := model.LookupOperation(ctx, installationID, operationID)
	if err != nil {
		return sdk.OperationLookup{}, err
	}
	return ToLookup(installationID, operationID, op), nil
}

// Cancel invalidates the owning installation's operation even if the native
// actor has since lost permission. It persists cancellation even when
// submission has not arrived; a delayed submission under the same ID cannot
// recreate authority. Cancellation is pending until its ordering against any
// in-flight write is known.
func (s *Service) Cancel(ctx context.Context, installationID, operationID string) (sdk.OperationRecord, error) {
	if !validOperationID(operationID) {
		return sdk.OperationRecord{}, ErrInvalidIntent
	}
	op, err := model.LookupOperation(ctx, installationID, operationID)
	if err != nil {
		return sdk.OperationRecord{}, err
	}
	if op == nil {
		tombstone, err := model.InsertTombstone(ctx, installationID, operationID)
		if err != nil && !errors.Is(err, model.ErrDuplicateOperation) {
			return sdk.OperationRecord{}, err
		}
		if errors.Is(err, model.ErrDuplicateOperation) {
			op = tombstone
		} else {
			return ToRecord(tombstone), nil
		}
	}
	if !op.Submitted {
		return ToRecord(op), nil
	}
	revoked, err := model.RevokeOperation(ctx, installationID, operationID)
	if err != nil {
		return sdk.OperationRecord{}, err
	}
	if revoked == nil {
		return sdk.OperationRecord{}, errors.New("operation vanished during cancellation")
	}
	return ToRecord(revoked), nil
}
