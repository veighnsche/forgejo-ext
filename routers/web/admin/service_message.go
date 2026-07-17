// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	service_message_model "forgejo.org/models/service_message"
	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	service_message_module "forgejo.org/modules/service_message"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/web"
	"forgejo.org/services/context"
	"forgejo.org/services/forms"
	service_message_service "forgejo.org/services/service_message"
)

const (
	tplServiceMessage base.TplName = "admin/service_message"
)

func GetServiceMessage(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("admin.service_message")
	ctx.Data["PageIsAdminServiceMessage"] = true
	sm, err := service_message_service.GetServiceMessage(ctx, service_message_service.SMTypeModal)
	if err != nil {
		if errors.Is(err, service_message_module.ErrServiceMessageNotExist) {
			sm = &service_message_model.ServiceMessage{}
		} else {
			ctx.HTML(http.StatusInternalServerError, tplServiceMessage)
			return
		}
	}
	ctx.Data["ServiceMessageTitle"] = sm.Title
	ctx.Data["ServiceMessageText"] = sm.Text
	ctx.Data["RenderedContent"] = templates.RenderMarkdownToHtml(ctx, sm.Text)
	ctx.HTML(http.StatusOK, tplServiceMessage)
}

func CreateOrUpdateServiceMessage(ctx *context.Context) {
	if ctx.Written() {
		return
	}
	form := web.GetForm(ctx).(*forms.ServiceMessageForm)
	smType := strings.TrimSpace(ctx.FormString("sm_type"))
	smOpts := service_message_module.ServiceMessageOptions{
		Title: form.Title,
		Text:  form.Text,
		Type:  smType,
	}
	serviceMessage, err := service_message_service.NewServiceMessage(&smOpts)
	if err != nil {
		ctx.Flash.Error(fmt.Errorf("Could not create service message: %s", err.Error()))
		return
	}
	err = service_message_service.CreateOrUpdateServiceMessage(ctx, serviceMessage)
	if err != nil {
		ctx.Flash.Error(fmt.Errorf("Could not create service message: %s", err.Error()))
		return
	}
	log.Debug("Done creating Service Message.")
	ctx.Redirect(setting.AppSubURL + "/admin/service_message")
}

func DeleteServiceMessage(ctx *context.Context) {
	if ctx.Written() {
		return
	}
	smType := strings.TrimSpace(ctx.FormString("sm_type"))
	sm, err := service_message_service.GetServiceMessage(ctx, smType)
	if err != nil {
		log.Error(err.Error())
		ctx.Flash.Error(fmt.Sprintf("Error deleting service message: %s", err.Error()))
		return
	}
	err = service_message_service.DeleteServiceMessage(ctx, sm)
	if err != nil {
		log.Error(err.Error())
		ctx.Flash.Error(fmt.Sprintf("Error deleting service message: %s", err.Error()))
		return
	}
	log.Debug("Deleted Service Message %s", smType)
	ctx.JSONRedirect(setting.AppSubURL + "/admin/service_message")
}
