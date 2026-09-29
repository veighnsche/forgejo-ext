// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import "path/filepath"

// Extensions configures administrator-installed, trusted executable packages.
var Extensions = struct {
	Enabled bool
	Path    string
}{}

func loadExtensionsFrom(cfg ConfigProvider) {
	sec := cfg.Section("extensions")
	Extensions.Enabled = sec.Key("ENABLED").MustBool(false)
	Extensions.Path = sec.Key("PATH").MustString(filepath.Join(AppDataPath, "extensions"))
	if !filepath.IsAbs(Extensions.Path) {
		Extensions.Path = filepath.Join(AppWorkPath, Extensions.Path)
	}
	Extensions.Path = filepath.Clean(Extensions.Path)
}
