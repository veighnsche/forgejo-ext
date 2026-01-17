// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package setting

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/invited"
	"forgejo.org/models/user"
	"forgejo.org/modules/base"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/timeutil"
	"forgejo.org/services/context"

	"github.com/golang-jwt/jwt/v5"
)

type InvitationClaims struct {
	jwt.RegisteredClaims
	InviterID int64 `json:"inviter"`
	// UseCount/UseInterval are the limits to apply to the user when using this token
	//
	// this way, for example, an admin can grant a user a token to invite more
	// people than they usually could, and we do not need to maintain an extra
	// limit per user. This, for example, can be useful for onboarding new orgs,
	// where one person shares an invitation token
	//
	// because limits are applied to the user, the user can not invite more people
	// by creating a new token
	RegCount           int `json:"rcnt"`
	RegIntervalSeconds int `json:"rint"`
}

const (
	tplSettingsInvitations      base.TplName = "user/settings/invitations"
	tplSettingsInvitationsAdmin base.TplName = "user/settings/invitations_admin"
	tplSettingsCantInvite       base.TplName = "user/settings/cantinvite"
)

func generateInvitationToken(ctx *context.Context, issuer, signUpLink string, inviterID int64, regCount, regIntervalSeconds int, expSeconds int64) string {
	expirationDate := timeutil.TimeStampNow().Add(expSeconds)

	ctx.Data["exp"] = expirationDate.Format(time.RFC3339)

	claims := InvitationClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   ctx.Doer.Name,
			Audience:  []string{signUpLink},
			ExpiresAt: jwt.NewNumericDate(expirationDate.AsTime()),
			// NotBefore: jwt.NewNumericDate(now),
			// IssuedAt: jwt.NewNumericDate(now),
		},
		InviterID:          inviterID,
		RegCount:           regCount,
		RegIntervalSeconds: regIntervalSeconds,
	}

	token, err := setting.Invitation.SigningKey.JWT(claims)
	if err != nil {
		ctx.Error(http.StatusInternalServerError, "Error signing token")
	}

	return token
}

func checkCanInvite(ctx *context.Context) bool {
	if ctx.Doer.IsAdmin {
		return true
	}
	reasons := []string{}

	if ctx.Doer.Type != user.UserTypeIndividual {
		reasons = append(reasons, "You must be an individual user")
	}

	if !ctx.Doer.IsActive {
		reasons = append(reasons, "Your account must be active")
	}

	age := time.Since(ctx.Doer.CreatedUnix.AsLocalTime())
	if age < time.Duration(setting.Invitation.CanInviteAccountAgeDays)*24*time.Hour {
		reasons = append(reasons, fmt.Sprintf("Your account must be at least %d days old", setting.Invitation.CanInviteAccountAgeDays))
	}

	if setting.Invitation.CanInviteRequire2FA {
		ok, err := auth_model.HasTwoFactorByUID(ctx, ctx.Doer.ID)
		if err != nil {
			reasons = append(reasons, "There was an error getting your 2FA status")
		} else if !ok {
			reasons = append(reasons, "You must have Two Factor Authorization (2FA) enabled")
		}
	}

	if len(reasons) == 0 {
		return true
	}
	ctx.Data["reasons"] = reasons
	ctx.HTML(http.StatusForbidden, tplSettingsCantInvite)
	return false
}

// admin can change, normal user not
type InvitationsForm struct {
	InviterName     string
	RegCount        int
	RegIntervalDays int
	ExpVal          int64
	ExpUnit         string
}

var expUnits = map[string]int64{
	"seconds": 1,
	"minutes": 60,
	"hours":   3600,
	"days":    3600 * 24,
	"weeks":   3600 * 24 * 7,
	"months":  3600 * 24 * 30,
}

func Invitations(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("settings.invitations")
	ctx.Data["PageIsInvitations"] = true

	if !checkCanInvite(ctx) {
		return
	}

	inviter := ctx.Doer
	expSeconds := setting.Invitation.ExpirationTime
	tpl := tplSettingsInvitations
	param := &InvitationsForm{
		InviterName:     inviter.Name,
		RegCount:        setting.Invitation.RegCount,
		RegIntervalDays: setting.Invitation.RegIntervalDays,
		ExpVal:          expSeconds,
		ExpUnit:         "seconds",
	}

	if ctx.Doer.IsAdmin {
		tpl = tplSettingsInvitationsAdmin
		wrong := make(map[string]string)
		var err error

		// avoid pulling in another dependency like gorilla/schema
		query := ctx.Req.URL.Query()
		v := query.Get("InviterName")
		if v != "" {
			inviter, err = user.GetUserByName(ctx, v)
			if err != nil {
				wrong["InviterName"] = " error"
				inviter = ctx.Doer
			}
			param.InviterName = inviter.Name
		}
		v = query.Get("RegCount")
		if v != "" {
			i, err := strconv.Atoi(v)
			if err == nil {
				param.RegCount = i
			} else {
				wrong["RegCount"] = " error"
			}
		}
		v = query.Get("RegIntervalDays")
		if v != "" {
			i, err := strconv.Atoi(v)
			if err == nil {
				param.RegIntervalDays = i
			} else {
				wrong["RegIntervalDays"] = " error"
			}
		}
		v = query.Get("ExpVal")
		if v != "" {
			i, err := strconv.ParseInt(v, 10, 64)
			if err == nil {
				param.ExpVal = i
			} else {
				wrong["ExpVal"] = " error"
			}
		}
		v = query.Get("ExpUnit")
		if v != "" {
			sec, ok := expUnits[v]
			if ok {
				expSeconds = param.ExpVal * sec
				param.ExpUnit = v
			} else {
				wrong["ExpUnit"] = " error"
			}
		}
		ctx.Data["wrong"] = wrong
		if len(wrong) == 0 {
			ctx.Data["showToken"] = true
		}
	}

	regIntervalSeconds := param.RegIntervalDays * 24 * 3600
	regUsed, err := invited.Since(ctx, inviter.ID, int64(regIntervalSeconds))
	if err != nil {
		return
	}
	regLeft := 0
	if int64(param.RegCount) > regUsed {
		regLeft = param.RegCount - int(regUsed)
	}

	ctx.Data["param"] = param
	ctx.Data["regUsed"] = regUsed
	ctx.Data["regLeft"] = regLeft

	baseLink := setting.AppSubURL + "/user/settings/invitations"
	signUpLink := setting.AppSubURL + "/user/sign_up"
	if regLeft > 0 {
		ctx.Data["URL"] = signUpLink + "?jwt=" + generateInvitationToken(ctx, baseLink, signUpLink, inviter.ID, param.RegCount, regIntervalSeconds, expSeconds)
	}

	ctx.HTML(http.StatusOK, tpl)
}
