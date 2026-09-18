// Copyright 2025 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package templates

import (
	"context"
	"html/template"

	"forgejo.org/models/user"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/translation"
)

type Context struct {
	context.Context
	Locale        translation.Locale
	AvatarUtils   *AvatarUtils
	Data          map[string]any
	Doer          func() *user.User
	TimestampType optional.Option[string]
}

var _ context.Context = Context{}

func NewContext(ctx context.Context) *Context {
	return &Context{Context: ctx}
}

var GetTimestampType = getTimestampType

func getTimestampType(ctx *Context, userID int64) (string, error) {
	return user.GetUserSetting(ctx.Context, userID, user.SettingsKeyTimestampType)
}

func (ctx *Context) TimeSince(time any) template.HTML {
	has, timestampType := ctx.TimestampType.Get()
	if !has {
		doer := ctx.Doer()
		timestampType = TimestampRelative
		if doer != nil {
			timestampType, _ = GetTimestampType(ctx, doer.ID)
			ctx.TimestampType = optional.Some(timestampType)
		}
	}

	return NewDateUtils().TimeSince(time, timestampType)
}
