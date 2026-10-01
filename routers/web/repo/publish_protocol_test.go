// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
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
