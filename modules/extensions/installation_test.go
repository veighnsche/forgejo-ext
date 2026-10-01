// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInstallationIdentityLifecycle(t *testing.T) {
	root := t.TempDir()
	m, err := Install(root, installationPackage(t, "1"), false)
	require.NoError(t, err)
	first, err := LoadInstallation(root, m.ID)
	require.NoError(t, err)
	require.NotEmpty(t, first)

	// Replacement and enable/disable preserve the installation UUID.
	_, err = Install(root, installationPackage(t, "2"), true)
	require.NoError(t, err)
	preserved, err := LoadInstallation(root, m.ID)
	require.NoError(t, err)
	require.Equal(t, first, preserved)
	require.NoError(t, SetEnabled(root, m.ID, false))
	require.NoError(t, SetEnabled(root, m.ID, true))
	preserved, err = LoadInstallation(root, m.ID)
	require.NoError(t, err)
	require.Equal(t, first, preserved)

	// Removal retires the UUID; a later installation cannot adopt it.
	require.NoError(t, Remove(root, m.ID))
	_, err = LoadInstallation(root, m.ID)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = Install(root, installationPackage(t, "3"), false)
	require.NoError(t, err)
	second, err := LoadInstallation(root, m.ID)
	require.NoError(t, err)
	require.NotEqual(t, first, second)
}

func TestInstallationIdentityRejectsInvalid(t *testing.T) {
	root := t.TempDir()
	_, err := EnsureInstallation(root, "not an id")
	require.Error(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".installations"), 0o700))
	require.NoError(t, os.WriteFile(InstallationPath(root, "example"), []byte("{invalid"), 0o600))
	_, err = LoadInstallation(root, "example")
	require.ErrorContains(t, err, "invalid")
	_, err = EnsureInstallation(root, "example")
	require.ErrorContains(t, err, "invalid")
}
