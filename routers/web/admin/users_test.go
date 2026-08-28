// Copyright 2017 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package admin

import (
	"context"
	"strconv"
	"testing"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	api "forgejo.org/modules/structs"
	"forgejo.org/modules/test"
	"forgejo.org/modules/translation"
	"forgejo.org/modules/web"
	"forgejo.org/services/contexttest"
	"forgejo.org/services/forms"
	"forgejo.org/services/mailer"
	"forgejo.org/tests/forgery"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewUserPost_MustChangePassword(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx, _ := contexttest.MockContext(t, "admin/users/new")

	u := unittest.AssertExistsAndLoadBean(t, &user_model.User{
		IsAdmin: true,
		ID:      2,
	})

	ctx.Doer = u

	username := "gitea"
	email := "gitea@gitea.io"

	form := forms.AdminCreateUserForm{
		LoginType:          "local",
		LoginName:          "local",
		UserName:           username,
		Email:              email,
		Password:           "abc123ABC!=$",
		SendNotify:         false,
		MustChangePassword: true,
	}

	web.SetForm(ctx, &form)
	NewUserPost(ctx)

	assert.NotEmpty(t, ctx.Flash.SuccessMsg)

	u, err := user_model.GetUserByName(ctx, username)

	require.NoError(t, err)
	assert.Equal(t, username, u.Name)
	assert.Equal(t, email, u.Email)
	assert.True(t, u.MustChangePassword)
}

func TestNewUserPost_MustChangePasswordFalse(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx, _ := contexttest.MockContext(t, "admin/users/new")

	u := unittest.AssertExistsAndLoadBean(t, &user_model.User{
		IsAdmin: true,
		ID:      2,
	})

	ctx.Doer = u

	username := "gitea"
	email := "gitea@gitea.io"

	form := forms.AdminCreateUserForm{
		LoginType:          "local",
		LoginName:          "local",
		UserName:           username,
		Email:              email,
		Password:           "abc123ABC!=$",
		SendNotify:         false,
		MustChangePassword: false,
	}

	web.SetForm(ctx, &form)
	NewUserPost(ctx)

	assert.NotEmpty(t, ctx.Flash.SuccessMsg)

	u, err := user_model.GetUserByName(ctx, username)

	require.NoError(t, err)
	assert.Equal(t, username, u.Name)
	assert.Equal(t, email, u.Email)
	assert.False(t, u.MustChangePassword)
}

func TestNewUserPost_InvalidEmail(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx, _ := contexttest.MockContext(t, "admin/users/new")

	u := unittest.AssertExistsAndLoadBean(t, &user_model.User{
		IsAdmin: true,
		ID:      2,
	})

	ctx.Doer = u

	username := "gitea"
	email := "gitea@gitea.io\r\n"

	form := forms.AdminCreateUserForm{
		LoginType:          "local",
		LoginName:          "local",
		UserName:           username,
		Email:              email,
		Password:           "abc123ABC!=$",
		SendNotify:         false,
		MustChangePassword: false,
	}

	web.SetForm(ctx, &form)
	NewUserPost(ctx)

	assert.NotEmpty(t, ctx.Flash.ErrorMsg)
}

func TestNewUserPost_VisibilityDefaultPublic(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx, _ := contexttest.MockContext(t, "admin/users/new")

	u := unittest.AssertExistsAndLoadBean(t, &user_model.User{
		IsAdmin: true,
		ID:      2,
	})

	ctx.Doer = u

	username := "gitea"
	email := "gitea@gitea.io"

	form := forms.AdminCreateUserForm{
		LoginType:          "local",
		LoginName:          "local",
		UserName:           username,
		Email:              email,
		Password:           "abc123ABC!=$",
		SendNotify:         false,
		MustChangePassword: false,
	}

	web.SetForm(ctx, &form)
	NewUserPost(ctx)

	assert.NotEmpty(t, ctx.Flash.SuccessMsg)

	u, err := user_model.GetUserByName(ctx, username)

	require.NoError(t, err)
	assert.Equal(t, username, u.Name)
	assert.Equal(t, email, u.Email)
	// As default user visibility
	assert.Equal(t, setting.Service.DefaultUserVisibilityMode, u.Visibility)
}

func TestNewUserPost_VisibilityPrivate(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx, _ := contexttest.MockContext(t, "admin/users/new")

	u := unittest.AssertExistsAndLoadBean(t, &user_model.User{
		IsAdmin: true,
		ID:      2,
	})

	ctx.Doer = u

	username := "gitea"
	email := "gitea@gitea.io"

	form := forms.AdminCreateUserForm{
		LoginType:          "local",
		LoginName:          "local",
		UserName:           username,
		Email:              email,
		Password:           "abc123ABC!=$",
		SendNotify:         false,
		MustChangePassword: false,
		Visibility:         api.VisibleTypePrivate,
	}

	web.SetForm(ctx, &form)
	NewUserPost(ctx)

	assert.NotEmpty(t, ctx.Flash.SuccessMsg)

	u, err := user_model.GetUserByName(ctx, username)

	require.NoError(t, err)
	assert.Equal(t, username, u.Name)
	assert.Equal(t, email, u.Email)
	// As default user visibility
	assert.True(t, u.Visibility.IsPrivate())
}

func mailHelper(t *testing.T, expectedTo, expectedSubject string, bodyPredicate func(t *testing.T, b string)) (cleanup func(), calledRes *bool) {
	t.Helper()

	// would be neat to use mailer.MockMailSettings here, but idk what magic import word makes that happen rn :/
	mailService := setting.Mailer{
		From: "test@forgejo.org",
	}

	called := false

	cleanups := []func(){
		test.MockVariableValue(&setting.MailService, &mailService),
		test.MockVariableValue(&setting.Domain, "localhost"),
		test.MockVariableValue(&mailer.SendAsync, func(msgs ...*mailer.Message) {
			if called {
				return
			}
			called = true

			assert.Len(t, msgs, 1)
			assert.Equal(t, expectedTo, msgs[0].To)
			assert.Equal(t, expectedSubject, msgs[0].Subject)

			bodyPredicate(t, msgs[0].Body)
		}),
	}

	// FIXME: This prevents nil dereference errors from the mailer, but it might be better if we could assign mailer.subjectTemplates and mailer.bodyTemplates directly, or simply call mailer.MockMailSettings instead
	mailer.NewContext(context.Background())

	cleanup = func() {
		for _, cleanup := range cleanups {
			cleanup()
		}
	}
	return cleanup, &called
}

func TestEditUserPost_Password(t *testing.T) {
	unittest.PrepareTestEnv(t)

	user := forgery.CreateUser(t, nil)
	idStr := strconv.FormatInt(user.ID, 10)
	ctx, _ := contexttest.MockContext(t, "POST /admin/users/"+idStr+"/edit")
	ctx.SetParams(":userid", idStr) // FIXME: for some reason, the param doesn't come from the path correctly, so we must do this :/

	adminUser := forgery.CreateUser(t, &forgery.CreateUserOptions{
		IsAdmin: true,
	})
	ctx.Doer = adminUser

	translation.InitLocales(t.Context())
	cleanup, called := mailHelper(t, user.EmailTo(), string(translation.NewLocale("en-US").Tr("mail.password_change.subject")), func(t *testing.T, body string) {
		// user gets the nice admin-changed-your-password email
		assert.NotContains(t, body, translation.NewLocale("en-US").Tr("mail.account_security_caution.text_2")) // "caution! 😱"
		assert.Contains(t, body, translation.NewLocale("en-US").Tr("mail.password_change_by_admin.text_1"))    // "an admin did it 😌"
	})
	defer cleanup()

	// for sanity
	u, err := user_model.GetUserByName(t.Context(), user.Name)
	require.NoError(t, err)
	assert.False(t, u.ValidatePassword(t.Context(), "new_password"), "password should not have changed yet")

	// try edit
	form := forms.AdminEditUserForm{
		LoginType:       user.LoginType.String(),
		LoginName:       user.LoginName,
		Password:        "new_password",
		MaxRepoCreation: -1,
		Active:          true,
		ProhibitLogin:   false,
		Visibility:      user.Visibility,
	}
	web.SetForm(ctx, &form)
	EditUserPost(ctx)
	require.Equal(t, 303, ctx.Resp.Status())
	assert.NotEmpty(t, ctx.Flash.SuccessMsg)
	assert.True(t, *called, "email should have been sent")

	u, err = user_model.GetUserByName(t.Context(), user.Name)
	require.NoError(t, err)
	assert.True(t, u.ValidatePassword(t.Context(), "new_password"))
}
