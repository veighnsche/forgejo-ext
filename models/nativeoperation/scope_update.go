// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"context"

	"forgejo.org/models/db"
)

// UpdateScopeWhere rewrites the held reservation's scope JSON only while
// owner still holds it. Owners record observed ref effects (prepared
// tuples, fetch results, realized OIDs) that were unknowable at claim
// time; anything else refuses with ErrWrongOwner and the scope is
// untouched. The mutation must preserve the scope's kind, family and
// repository identity.
func UpdateScopeWhere(ctx context.Context, owner, scopeJSON string) error {
	if owner == "" || scopeJSON == "" {
		return ErrWrongOwner
	}
	affected, err := db.GetEngine(ctx).Where("id=1 AND owner=?", owner).Cols("scope").Update(&Reservation{Scope: scopeJSON})
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrWrongOwner
	}
	return nil
}
