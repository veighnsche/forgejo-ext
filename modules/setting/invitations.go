// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT
//
// largely copied from oauth2.go

package setting

import (
	"forgejo.org/modules/jwtx"
	"forgejo.org/modules/log"
)

var Invitation = struct {
	SigningKey     jwtx.SigningKey
	Verifier       *jwtx.Verifier
	ExpirationTime int64 `ini:"EXPIRATION_TIME_SECONDS"`

	// How many a user can invite over the interval
	RegCount        int `ini:"COUNT"`
	RegIntervalDays int `ini:"INTERVAL_DAYS"`

	// Condition to be able to invite
	CanInviteAccountAgeDays int  `ini:"CAN_INVITE_ACCOUNT_AGE_DAYS"`
	CanInviteRequire2FA     bool `ini:"CAN_INVITE_REQUIRE_2FA"`
}{
	ExpirationTime:          1 * 24 * 3600,
	RegCount:                10, // 10 per month
	RegIntervalDays:         30,
	CanInviteAccountAgeDays: 7,
	CanInviteRequire2FA:     true,
}

func loadInvitationsFrom(rootCfg ConfigProvider) {
	if !Service.InvitationOnly {
		return
	}
	const name = "service.invitation"
	mustMapSetting(rootCfg, name, &Invitation)
	keyCfg, err := loadKeyCfg(rootCfg, name, "JWT_", "RS256", "invitation/private.pem")
	if err == nil {
		Invitation.SigningKey, Invitation.Verifier, err = jwtx.Init(&keyCfg)
	}
	if err != nil {
		log.Fatal("invitation initialization failed: %v", err)
	}
}
