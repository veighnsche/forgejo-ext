// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"net/http"
	"strings"

	user_model "forgejo.org/models/user"
	"forgejo.org/modules/timeutil"
	"forgejo.org/services/context"
	service_message_service "forgejo.org/services/service_message"
)

func SetConfirm(ctx *context.Context) {
	smTypeQuery := strings.TrimSpace(ctx.FormString("sm_type"))
	redirect := strings.TrimSpace(ctx.FormString("redirect_to"))
	confirmed := timeutil.TimeStampNow()

	sm, err := service_message_service.GetServiceMessage(ctx, smTypeQuery)
	if err != nil {
		ctx.Error(http.StatusInternalServerError)
		return
	}

	ctx.Doer.SetConfirm(sm.Type, confirmed)
	err = user_model.UpdateUserCols(ctx, ctx.Doer, "confirms")
	if err != nil {
		ctx.Error(http.StatusInternalServerError)
		return
	}
	ctx.Data["ModalServiceMessageTitle"] = nil
	ctx.Redirect(redirect)
}
