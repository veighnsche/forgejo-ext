// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package user

import (
	"context"
	"strconv"

	sdk "forgejo.org/extension-sdk"
	"forgejo.org/models/db"
	"forgejo.org/modules/extensions"
	"forgejo.org/modules/setting"
)

// CheckUsernamePolicy applies the personal-account veto before identity mutation.
// Organizations and remote ActivityPub identities do not represent personal OS
// logins; ActivityPub handles also intentionally use a different native syntax.
func CheckUsernamePolicy(ctx context.Context, u *User, operation, username string) error {
	if u.IsOrganization() || u.IsActivityPub() || len(setting.Extensions.RequiredIDs) == 0 {
		return nil
	}
	// Unknown transactional callers must fail closed without performing RPC under locks.
	if db.InTransaction(ctx) {
		return extensions.ErrRequiredPolicyUnavailable
	}
	request := sdk.PolicyRequest{Operation: operation, Username: username}
	if operation == "rename" {
		request.UserID = strconv.FormatInt(u.ID, 10)
	}
	return extensions.CheckRequiredPolicy(ctx, sdk.PolicyForgejoUsername, request)
}
