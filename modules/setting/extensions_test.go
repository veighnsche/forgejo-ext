// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtensionSettings(t *testing.T) {
	previous := Extensions
	dataPath, workPath := AppDataPath, AppWorkPath
	t.Cleanup(func() { Extensions, AppDataPath, AppWorkPath = previous, dataPath, workPath })
	AppWorkPath = t.TempDir()
	AppDataPath = filepath.Join(AppWorkPath, "data")
	cfg, err := NewConfigProviderFromData("")
	require.NoError(t, err)
	loadExtensionsFrom(cfg)
	require.False(t, Extensions.Enabled)
	require.Equal(t, filepath.Join(AppDataPath, "extensions"), Extensions.Path)
	require.Empty(t, Extensions.RequiredIDs)
	require.Empty(t, Extensions.ServiceCallbackPath)
	cfg, err = NewConfigProviderFromData("[extensions]\nENABLED = true\nPATH = packages\nREQUIRED_IDS = soda, audit\n")
	require.NoError(t, err)
	loadExtensionsFrom(cfg)
	require.True(t, Extensions.Enabled)
	require.Equal(t, filepath.Join(AppWorkPath, "packages"), Extensions.Path)
	require.Equal(t, []string{"soda", "audit"}, Extensions.RequiredIDs)
	for _, path := range []string{"relative/callback.sock", "/run/fountain/callback.sock", ""} {
		cfg, err = NewConfigProviderFromData("[extensions]\nSERVICE_CALLBACK_PATH = " + path + "\n")
		require.NoError(t, err)
		loadExtensionsFrom(cfg)
		require.Equal(t, path, Extensions.ServiceCallbackPath)
	}
}
