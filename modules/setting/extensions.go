// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"path/filepath"
	"strings"
)

// Extensions configures administrator-installed, trusted executable packages.
var Extensions = struct {
	Enabled             bool
	Path                string
	RequiredIDs         []string
	ServiceCallbackPath string
}{}

func loadExtensionsFrom(cfg ConfigProvider) {
	sec := cfg.Section("extensions")
	Extensions.Enabled = sec.Key("ENABLED").MustBool(false)
	// Preserve the explicit path: the manager rejects relative paths rather than
	// silently moving a service trust boundary under the work directory.
	Extensions.ServiceCallbackPath = sec.Key("SERVICE_CALLBACK_PATH").String()
	Extensions.Path = sec.Key("PATH").MustString(filepath.Join(AppDataPath, "extensions"))
	if !filepath.IsAbs(Extensions.Path) {
		Extensions.Path = filepath.Join(AppWorkPath, Extensions.Path)
	}
	Extensions.Path = filepath.Clean(Extensions.Path)
	Extensions.RequiredIDs = nil
	if raw := strings.TrimSpace(sec.Key("REQUIRED_IDS").String()); raw != "" {
		for _, id := range strings.Split(raw, ",") {
			Extensions.RequiredIDs = append(Extensions.RequiredIDs, strings.TrimSpace(id))
		}
	}
}
