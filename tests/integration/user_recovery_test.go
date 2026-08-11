// Copyright 2025 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"bytes"
	"net/http"
	"net/url"
	"testing"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/test"
	"forgejo.org/modules/translation"
	"forgejo.org/services/mailer"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForgotPassword(t *testing.T) {
	defer unittest.OverrideFixtures("models/user/fixtures")()
	defer tests.PrepareTestEnv(t)()

	test := func(t *testing.T, user *user_model.User, email *user_model.EmailAddress) {
		t.Helper()

		called := false

		// This closure does two things:
		// 1. it receives the reset password mail and changes the password
		// 2. it receives the password change notification and sets called to true
		defer test.MockVariableValue(&mailer.SendAsync, func(msgs ...*mailer.Message) {
			assert.Len(t, msgs, 1)
			assert.Equal(t, user.EmailTo(), msgs[0].To)
			if msgs[0].Subject == translation.NewLocale("en-US").TrString("mail.password_change.subject") {
				called = true
				return
			}

			assert.EqualValues(t, translation.NewLocale("en-US").Tr("mail.reset_password"), msgs[0].Subject)
			assert.Contains(t, msgs[0].Body, translation.NewLocale("en-US").Tr("mail.reset_password.text", "3 hours"))

			buf := bytes.NewBuffer([]byte(msgs[0].Body))
			doc := NewHTMLParser(t, buf)

			selection := doc.Find("a")
			require.NotNil(t, selection)

			url, err := url.Parse(selection.AttrOr("href", ""))
			require.NoError(t, err)

			recoveryCode := url.Query().Get("code")
			require.NotEmpty(t, recoveryCode)

			req := NewRequestWithValues(t, "POST", "/user/recover_account", map[string]string{
				"code":     recoveryCode,
				"password": "12<345678",
			})
			MakeRequest(t, req, http.StatusSeeOther)
		})()

		req := NewRequestWithValues(t, "POST", "/user/forgot_password", map[string]string{
			"email": email.Email,
		})
		MakeRequest(t, req, http.StatusOK)

		assert.True(t, called)
	}

	t.Run("Unactivated email address", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		test(t, unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 11}), unittest.AssertExistsAndLoadBean(t, &user_model.EmailAddress{UID: 11}, "is_activated = false"))
	})

	t.Run("Activated email address", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		test(t, unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 12}), unittest.AssertExistsAndLoadBean(t, &user_model.EmailAddress{UID: 12}, "is_activated = true"))
	})

	t.Run("Unlink external account", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		u := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1045})
		assert.False(t, u.IsLocal())
		assert.False(t, u.IsPasswordSet())

		test(t, u, unittest.AssertExistsAndLoadBean(t, &user_model.EmailAddress{UID: 1045}))

		u = unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1045})
		assert.True(t, u.IsLocal())
		assert.True(t, u.IsPasswordSet())
	})
}
