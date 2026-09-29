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
	cfg, err = NewConfigProviderFromData("[extensions]\nENABLED = true\nPATH = packages\n")
	require.NoError(t, err)
	loadExtensionsFrom(cfg)
	require.True(t, Extensions.Enabled)
	require.Equal(t, filepath.Join(AppWorkPath, "packages"), Extensions.Path)
}
