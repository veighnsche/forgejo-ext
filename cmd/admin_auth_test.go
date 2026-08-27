// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPLv3-or-later

package cmd

import (
	"context"
	"testing"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/modules/translation"
	"forgejo.org/services/mailer"
	"forgejo.org/tests/forgery"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

// TODO: Surely there's a way to share this helper between the test modules that need it 🤔
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

func TestAdminUserChangePassword(t *testing.T) {
	// Mock cli functions to not exit on error
	defer test.MockVariableValue(&cli.OsExiter, func(code int) {})()

	unittest.InitSettings()
	user := forgery.CreateUser(t, nil)

	translation.InitLocales(t.Context())
	cleanup, called := mailHelper(t, user.EmailTo(), string(translation.NewLocale("en-US").Tr("mail.password_change.subject")), func(t *testing.T, body string) {
		// user gets the nice admin-changed-your-password email
		assert.NotContains(t, body, translation.NewLocale("en-US").Tr("mail.account_security_caution.text_2")) // "caution! 😱"
		assert.Contains(t, body, translation.NewLocale("en-US").Tr("mail.password_change_by_admin.text_1")) // "an admin did it 😌"
	})
	defer cleanup()

	// for sanity
	u, err := user_model.GetUserByName(t.Context(), user.Name)
	require.NoError(t, err)
	assert.False(t, u.ValidatePassword(t.Context(), "new_password"), "password should not have changed yet")

	app := cli.Command{}
	app.Flags = microcmdUserChangePassword().Flags
	app.Action = runChangePassword // FIXME: this seems to fail at the initDB call; this fn may require modification in order to be testable

	args := []string{"change-password", "-u", user.Name, "-p", "new_password"}
	err = app.Run(t.Context(), args)
	require.NoError(t, err)
	assert.True(t, *called, "email should have been sent")

	u, err = user_model.GetUserByName(t.Context(), user.Name)
	require.NoError(t, err)
	assert.True(t, u.ValidatePassword(t.Context(), "new_password"))
}
