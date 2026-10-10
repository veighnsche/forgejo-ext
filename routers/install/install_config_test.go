// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package install

import (
	"path/filepath"
	"testing"

	"forgejo.org/modules/setting"
	"github.com/stretchr/testify/require"
)

func TestInstallDatabasePasswordSaveBoundary(t *testing.T) {
	t.Run("URI environment keeps URI without resolved password", func(t *testing.T) {
		cfg, err := setting.NewConfigProviderFromData(`[database]
PASSWD = resolved-from-install-form
`)
		require.NoError(t, err)
		setting.EnvironmentToConfig(cfg, []string{
			"FORGEJO__database__PASSWD_URI=file:/etc/forgejo/db_passwd",
		})
		discardResolvedDatabasePasswordWhenURIConfigured(cfg)

		path := filepath.Join(t.TempDir(), "app.ini")
		require.NoError(t, cfg.SaveTo(path))
		saved, err := setting.NewConfigProviderFromFile(path)
		require.NoError(t, err)
		database := saved.Section("database")
		require.Equal(t, "file:/etc/forgejo/db_passwd", database.Key("PASSWD_URI").String())
		require.Nil(t, setting.ConfigSectionKey(database, "PASSWD"))
	})

	t.Run("ordinary password remains without URI", func(t *testing.T) {
		cfg, err := setting.NewConfigProviderFromData(`[database]
PASSWD = ordinary-install-password
`)
		require.NoError(t, err)
		setting.EnvironmentToConfig(cfg, nil)
		discardResolvedDatabasePasswordWhenURIConfigured(cfg)

		path := filepath.Join(t.TempDir(), "app.ini")
		require.NoError(t, cfg.SaveTo(path))
		saved, err := setting.NewConfigProviderFromFile(path)
		require.NoError(t, err)
		database := saved.Section("database")
		require.Equal(t, "ordinary-install-password", database.Key("PASSWD").String())
	})
}
