// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidatePublishService(t *testing.T) {
	require.NoError(t, ValidatePublishService("receive-pack"))

	for _, service := range []string{"", "upload-pack", "upload-archive", "git-receive-pack", "receive-pack "} {
		err := ValidatePublishService(service)
		require.ErrorIs(t, err, ErrPublishServiceRejected, "service %q", service)
	}
}

func TestValidatePublishGitProtocol(t *testing.T) {
	require.NoError(t, ValidatePublishGitProtocol(""))
	require.NoError(t, ValidatePublishGitProtocol("version=2"))

	for _, header := range []string{"version=2;evil=1", "version =2", "../version=2", "version=2\ninjected=x", "a=b=c"} {
		err := ValidatePublishGitProtocol(header)
		require.ErrorIs(t, err, ErrPublishProtocolRejected, "header %q", header)
	}
}

func TestValidatePublishPushOptions(t *testing.T) {
	require.NoError(t, ValidatePublishPushOptions(nil))
	require.NoError(t, ValidatePublishPushOptions(map[string]string{}))

	err := ValidatePublishPushOptions(map[string]string{"operation-id": "op-1"})
	require.ErrorIs(t, err, ErrPublishPushOptionsRejected)

	err = ValidatePublishPushOptions(map[string]string{"signed": ""})
	require.ErrorIs(t, err, ErrPublishPushOptionsRejected)
	assert.ErrorContains(t, err, "push options")
}

func TestParsePublishBinding(t *testing.T) {
	admission := strings.Repeat("a", 43)

	binding, present, err := ParsePublishBinding("", "")
	require.NoError(t, err)
	require.False(t, present)

	binding, present, err = ParsePublishBinding("op-1", admission)
	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, "op-1", binding.OperationID)
	require.Equal(t, admission, binding.Admission)

	for name, values := range map[string][2]string{
		"operation only":     {"op-1", ""},
		"admission only":     {"", admission},
		"bad operation":      {"op 1", admission},
		"long operation":     {strings.Repeat("o", 129), admission},
		"short admission":    {"op-1", "short"},
		"interior-space tag": {"op-1", admission[:20] + " " + admission[21:]},
	} {
		_, present, err := ParsePublishBinding(values[0], values[1])
		require.ErrorIs(t, err, ErrPublishBindingRejected, name)
		require.True(t, present, name)
	}
}
