// Copyright 2025 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later
package templates

import (
	"context"
	"html/template"
	"testing"
	"time"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	"forgejo.org/models/user"
	"forgejo.org/modules/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContext(t *testing.T) {
	type ctxKey struct{}

	// Test that the original context is used for its context functions.
	ctx := NewContext(context.WithValue(t.Context(), ctxKey{}, "there"))
	assert.Equal(t, "there", ctx.Value(ctxKey{}))
}

func TestContextTimeSince(t *testing.T) {
	const userID = 2

	require.NoError(t, unittest.PrepareTestDatabase())

	tz, err := time.LoadLocation("UTC")
	require.NoError(t, err)

	testTime := time.Date(2026, 9, 18, 12, 0, 0, 0, tz)
	resultAbsolute := template.HTML(`<span data-testid="absolute-time-1789732800">2026-09-18 12:00:00</span>`)
	resultRelative := template.HTML(`<relative-time prefix="" tense="past" datetime="2026-09-18T12:00:00Z" data-tooltip-content data-tooltip-interactive="true">2026-09-18 12:00:00 +00:00</relative-time>`)

	testCases := []struct {
		name         string
		settingValue string
		expected     template.HTML
	}{
		{"TimestampAbsolute", TimestampAbsolute, resultAbsolute},
		{"TimestampRelative", TimestampRelative, resultRelative},
		{"use relative as default", "", resultRelative},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := NewContext(db.DefaultContext)
			ctx.Doer = func() *user.User {
				return &user.User{ID: userID}
			}

			newSetting := &user.Setting{UserID: userID, SettingKey: user.SettingsKeyTimestampType, SettingValue: testCase.settingValue}

			err = user.SetUserSetting(db.DefaultContext, newSetting.UserID, newSetting.SettingKey, newSetting.SettingValue)
			require.NoError(t, err)

			assert.Equal(t, testCase.expected, ctx.TimeSince(testTime))
		})
	}

	t.Run("memoized timestamp type", func(t *testing.T) {
		// Replace GetUserSetting with a function that returns absolute once, then relative for every other call.
		called := false
		defer test.MockVariableValue(&GetTimestampType, func(ctx *Context, userID int64) (string, error) {
			value := TimestampAbsolute
			if called {
				value = TimestampRelative
			}
			called = true
			return value, nil
		})()

		ctx := NewContext(db.DefaultContext)
		ctx.Doer = func() *user.User {
			return &user.User{ID: userID}
		}

		// The first setting value is absolute, the result should be an absolute timestamp.
		assert.Equal(t, resultAbsolute, ctx.TimeSince(testTime))

		// The second value is relative, but the result should still be an absolute timestamp.
		assert.Equal(t, resultAbsolute, ctx.TimeSince(testTime))
	})
}
