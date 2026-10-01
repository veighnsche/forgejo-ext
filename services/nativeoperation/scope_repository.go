// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"context"
	"errors"

	model "forgejo.org/models/nativeoperation"
	execcontext "forgejo.org/modules/nativeoperation"
)

// RecordScopeRefResult records the realized new OID of a single-ref
// ordinary scope for the owner carried by ctx. It refuses when the context
// carries no execution: result recording without ownership hides a missing
// claim.
func RecordScopeRefResult(ctx context.Context, newOID string) error {
	exec := execcontext.FromContext(ctx)
	if exec == nil || exec.Owner == "" {
		return errors.New("ref result recording requires ownership")
	}
	return UpdateScopeRefResult(ctx, exec.Owner, newOID)
}

// UpdateScopeRefResult records the realized new OID of a single-ref
// ordinary scope after the effect is computed but before it is pushed. Only
// the holding owner records; anything else refuses with ErrWrongOwner and
// the scope is untouched.
func UpdateScopeRefResult(ctx context.Context, owner, newOID string) error {
	if owner == "" || newOID == "" {
		return model.ErrWrongOwner
	}
	reservation, err := model.ReadReservation(ctx)
	if err != nil {
		return err
	}
	if reservation.Owner != owner {
		return model.ErrWrongOwner
	}
	scope, err := decodeScope(reservation.Scope)
	if err != nil {
		return err
	}
	if scope.NewOID != "" && scope.NewOID != newOID {
		return model.ErrWrongOwner
	}
	scope.NewOID = newOID
	encoded, err := encodeScope(scope)
	if err != nil {
		return err
	}
	return model.UpdateScopeWhere(ctx, owner, encoded)
}

// UpdateScopeRefs records realized end states for refs in the held owner's
// scope. Entries recorded before the effect keep their old OID while the
// new OID is set; refs without an entry are added with an empty old OID.
// Only the holding owner records; anything else refuses with ErrWrongOwner
// and the scope is untouched.
func UpdateScopeRefs(ctx context.Context, owner string, refs []ScopedRef) error {
	if owner == "" || len(refs) == 0 {
		return model.ErrWrongOwner
	}
	reservation, err := model.ReadReservation(ctx)
	if err != nil {
		return err
	}
	if reservation.Owner != owner {
		return model.ErrWrongOwner
	}
	scope, err := decodeScope(reservation.Scope)
	if err != nil {
		return err
	}
	index := make(map[string]int, len(scope.Refs))
	for i, prev := range scope.Refs {
		index[prev.Ref] = i
	}
	for _, next := range refs {
		if i, ok := index[next.Ref]; ok {
			scope.Refs[i].NewOID = next.NewOID
			continue
		}
		index[next.Ref] = len(scope.Refs)
		scope.Refs = append(scope.Refs, ScopedRef{Ref: next.Ref, NewOID: next.NewOID})
	}
	encoded, err := encodeScope(scope)
	if err != nil {
		return err
	}
	return model.UpdateScopeWhere(ctx, owner, encoded)
}

// AppendScopeRefs unions observed ref effects into the scope of the
// currently held owner: prepared tuples, fetch results or realized OIDs
// that were unknowable at claim time. Only the holding owner appends;
// anything else refuses with ErrWrongOwner and the scope is untouched. A
// contradicting tuple for an already recorded ref refuses without
// changing the scope.
func AppendScopeRefs(ctx context.Context, owner string, refs []ScopedRef) error {
	if owner == "" || len(refs) == 0 {
		return model.ErrWrongOwner
	}
	reservation, err := model.ReadReservation(ctx)
	if err != nil {
		return err
	}
	if reservation.Owner != owner {
		return model.ErrWrongOwner
	}
	scope, err := decodeScope(reservation.Scope)
	if err != nil {
		return err
	}
	known := make(map[string]ScopedRef, len(scope.Refs)+len(refs))
	for _, prev := range scope.Refs {
		known[prev.Ref] = prev
	}
	for _, next := range refs {
		prev, ok := known[next.Ref]
		if !ok {
			known[next.Ref] = next
			scope.Refs = append(scope.Refs, next)
			continue
		}
		if prev.OldOID != next.OldOID || prev.NewOID != next.NewOID {
			return model.ErrWrongOwner
		}
	}
	encoded, err := encodeScope(scope)
	if err != nil {
		return err
	}
	return model.UpdateScopeWhere(ctx, owner, encoded)
}
