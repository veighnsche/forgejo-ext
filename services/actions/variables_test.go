// Copyright 2025 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package actions

import (
	"testing"

	"forgejo.org/modules/testhelper"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServicesAction_envNameCIRegexMatch(t *testing.T) {
	testhelper.Setup(t)
	require.ErrorContains(t, envNameCIRegexMatch("ci"), "cannot be ci")
	require.ErrorContains(t, envNameCIRegexMatch("CI"), "cannot be ci")
	assert.NoError(t, envNameCIRegexMatch("CI_SOMETHING"))
}
