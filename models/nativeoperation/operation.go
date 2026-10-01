// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package nativeoperation stores the generic conditional-operation ledger:
// durable operation identity with pre-submit cancellation tombstones, and the
// single exclusive native-mutation reservation with its native-state revision.
// It performs no factory policy.
package nativeoperation

import (
	"forgejo.org/models/db"
	"forgejo.org/modules/timeutil"
)

// Operation kinds mirror the SDK background kinds. Authorization for one kind
// never grants another. FT03 executes merge; other kinds report their stage
// as unavailable until their owning task lands.
const (
	KindRefPublish   = "git.ref.publish"
	KindPRCreate     = "pull_request.create"
	KindReviewSubmit = "pull_request.review.submit"
	KindMerge        = "pull_request.merge"
)

// Effect states for an operation's primary native effect.
const (
	EffectPending       = "pending"
	EffectNotCommitted  = "not_committed"
	EffectCommitted     = "committed"
	EffectIndeterminate = "indeterminate"
)

// Cancellation outcomes reported alongside the effect state.
const (
	CancellationNone          = "none"
	CancellationPending       = "pending"
	CancellationCancelled     = "cancelled"
	CancellationTooLate       = "too_late"
	CancellationIndeterminate = "indeterminate"
)

// Completion states for a committed effect's native bookkeeping.
const (
	CompletionPending           = "pending"
	CompletionComplete          = "complete"
	CompletionNeedsIntervention = "needs_intervention"
)

// Bounded refusal/receipt reasons recorded on operations.
const (
	ReasonCancelledBeforeSubmit    = "cancelled_before_submit"
	ReasonCancelledBeforeAdmission = "cancelled_before_admission"
	ReasonDuplicateAdmission       = "duplicate_admission"
	ReasonStaleNativeRevision      = "stale_native_revision"
	ReasonStaleHead                = "stale_head"
	ReasonStaleBaseOrResult        = "stale_base_or_result"
	ReasonExpiredBeforeAdmission   = "expired_before_admission"
	ReasonWrongOwner               = "wrong_owner"
	ReasonUnexpectedRefEffects     = "unexpected_ref_effects"
	ReasonNativeRefused            = "native_refused"
	ReasonAuthorityLost            = "authority_lost"
	ReasonIntentConflict           = "intent_conflict"
	ReasonPRMismatch               = "pr_mismatch"
	ReasonRecoveredNoEffect        = "recovered_no_effect"
)

// Operation is one immutable conditional-operation authorization identified
// by (installation_id, operation_id), or a pre-submit cancellation tombstone
// when Submitted is false. CredentialFingerprint is private: it detects
// regeneration of the bound token row and must never appear in receipts.
type Operation struct {
	ID                     int64              `xorm:"pk autoincr"`
	InstallationID         string             `xorm:"VARCHAR(36) NOT NULL index unique(op)"`
	OperationID            string             `xorm:"VARCHAR(128) NOT NULL index unique(op)"`
	Kind                   string             `xorm:"VARCHAR(64) NOT NULL DEFAULT ''"`
	ActorID                int64              `xorm:"NOT NULL DEFAULT 0"`
	RepositoryID           int64              `xorm:"NOT NULL DEFAULT 0"`
	TokenID                int64              `xorm:"NOT NULL DEFAULT 0"`
	CredentialFingerprint  string             `xorm:"VARCHAR(64) NOT NULL DEFAULT ''"`
	AuthRevision           string             `xorm:"TEXT NOT NULL"`
	ExpectedNativeRevision int64              `xorm:"NOT NULL DEFAULT 0"`
	NotAfter               int64              `xorm:"NOT NULL DEFAULT 0"`
	IntentDigest           string             `xorm:"VARCHAR(64) NOT NULL DEFAULT ''"`
	Intent                 string             `xorm:"TEXT NOT NULL"`
	Submitted              bool               `xorm:"NOT NULL DEFAULT false"`
	Revoked                bool               `xorm:"NOT NULL DEFAULT false"`
	Admitted               bool               `xorm:"NOT NULL DEFAULT false"`
	EffectState            string             `xorm:"VARCHAR(16) NOT NULL DEFAULT 'pending'"`
	Reason                 string             `xorm:"VARCHAR(64) NOT NULL DEFAULT ''"`
	Cancellation           string             `xorm:"VARCHAR(16) NOT NULL DEFAULT 'none'"`
	Completion             string             `xorm:"VARCHAR(16) NOT NULL DEFAULT ''"`
	Receipt                string             `xorm:"TEXT NOT NULL"`
	CreatedUnix            timeutil.TimeStamp `xorm:"created NOT NULL"`
	UpdatedUnix            timeutil.TimeStamp `xorm:"updated NOT NULL"`
}

func init() {
	db.RegisterModel(new(Operation))
	db.RegisterModel(new(Reservation))
}

// ValidKind reports whether kind is a supported background operation kind.
func ValidKind(kind string) bool {
	switch kind {
	case KindRefPublish, KindPRCreate, KindReviewSubmit, KindMerge:
		return true
	default:
		return false
	}
}

// IsTerminal reports whether the effect state is final. Indeterminate keeps
// its fence and is not terminal: reconciliation may still resolve it.
func (op *Operation) IsTerminal() bool {
	return op.EffectState == EffectCommitted || op.EffectState == EffectNotCommitted
}
