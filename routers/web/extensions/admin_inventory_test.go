// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"bytes"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	packages "forgejo.org/modules/extensions"
	"forgejo.org/modules/json"
	"forgejo.org/modules/setting"

	"github.com/stretchr/testify/require"
)

func TestAdminCatalogInventory(t *testing.T) {
	root := adminTestRoot(t)
	startAdminManager(t, root, sampleCatalogDoc(strings.Repeat("a", 64)))
	read := func() []adminStatusRow {
		ctx, recorder := adminTestContext(t, http.MethodGet, "/admin/extensions/status", true)
		AdminExtensionsStatus(ctx)
		require.Equal(t, http.StatusOK, recorder.Code)
		var rows []adminStatusRow
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &rows))
		require.Len(t, rows, 1)
		return rows
	}
	rows := read()
	require.Equal(t, "sample", rows[0].ID)
	require.False(t, rows[0].Installed)
	require.Nil(t, rows[0].InstalledVersion)
	require.Equal(t, "1.0", *rows[0].AvailableVersion)
	require.Equal(t, "available", rows[0].State)
	require.Equal(t, "0", rows[0].Generation)
	require.False(t, rows[0].DesiredEnabled)
	require.Equal(t, "stopped", rows[0].Backend)
	require.Equal(t, "not_required", rows[0].Effects)
	require.Nil(t, rows[0].Error)

	installation, err := packages.EnsureInstallation(root, "sample")
	require.NoError(t, err)
	pending := "activate"
	_, err = packages.StoreIntent(root, "sample", installation, true, &pending, "0")
	require.NoError(t, err)
	rows = read()
	require.Equal(t, "1", rows[0].Generation)
	require.True(t, rows[0].DesiredEnabled)
	require.False(t, rows[0].Installed)
	require.Equal(t, "failed", rows[0].State)
	require.NotNil(t, rows[0].Error)
	after, err := packages.LoadIntent(root, "sample")
	require.NoError(t, err)
	require.Equal(t, "1", after.Generation)
	require.Equal(t, &pending, after.PendingOperation)
}

func TestAdminRequiredDesiredState(t *testing.T) {
	root := adminTestRoot(t)
	startAdminManager(t, root, "")
	writeAdminPackage(t, root, "sample", false)
	setting.Extensions.RequiredIDs = []string{"sample"}
	ctx, recorder := adminTestContext(t, http.MethodGet, "/admin/extensions/status", true)
	AdminExtensionsStatus(ctx)
	var rows []adminStatusRow
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &rows))
	require.Len(t, rows, 1)
	require.True(t, rows[0].Required)
	require.True(t, rows[0].DesiredEnabled)
}

// The accepted POST has already saved intent before acquisition. Status must
// preserve that failed operation even though no package directory was installed.
func TestAdminAcquisitionFailureStatus(t *testing.T) {
	root := adminTestRoot(t)
	doc := strings.ReplaceAll(sampleCatalogDoc(strings.Repeat("a", 64)), "https://publisher.example/releases/download/v1/sample-1.0.0.tar.gz", "https://127.0.0.1:1/sample.tar.gz")
	manager := startAdminManager(t, root, doc)
	ctx, recorder := adminTestContext(t, http.MethodPost, "/admin/extensions/enable/sample", true)
	setAdminOrigin(t, ctx)
	withExtensionID(ctx, "sample")
	setAdminForm(ctx, "0", adminDocSHA(doc))
	AdminEnableExtension(ctx)
	require.Equal(t, http.StatusSeeOther, recorder.Code)
	require.Eventually(t, func() bool { return manager.Observe("sample").State == packages.LifecycleFailed }, 5*time.Second, 10*time.Millisecond)
	ctx, recorder = adminTestContext(t, http.MethodGet, "/admin/extensions/status", true)
	AdminExtensionsStatus(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	var rows []adminStatusRow
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &rows))
	require.Len(t, rows, 1)
	row := rows[0]
	require.False(t, row.Installed)
	require.Equal(t, "1", row.Generation)
	require.True(t, row.DesiredEnabled)
	require.Equal(t, "failed", row.State)
	require.NotNil(t, row.Error)
	require.Equal(t, packages.LifecycleErrAcquireFailed, row.Error.Code)
	intent, err := packages.LoadIntent(root, "sample")
	require.NoError(t, err)
	require.Equal(t, "1", intent.Generation)
	require.True(t, intent.DesiredEnabled)
	require.Nil(t, intent.PendingOperation, "acquisition failed before external effects")
}

func TestAdminRenderedActions(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(setting.StaticRootPath, "templates/admin/extensions.tmpl"))
	require.NoError(t, err)
	view, err := template.New("extensions").Funcs(template.FuncMap{
		"AppSubUrl": func() string { return "" },
		"dict": func(args ...any) map[string]any {
			out := map[string]any{}
			for i := 0; i < len(args); i += 2 {
				out[args[i].(string)] = args[i+1]
			}
			return out
		},
	}).Parse(`{{define "admin/layout_head"}}{{end}}{{define "admin/layout_footer"}}{{end}}` + string(data))
	require.NoError(t, err)
	for _, tc := range []struct {
		name, state                                                          string
		installed, desired, unconfirmed, required, disable, enable, settings bool
	}{
		{"failed cleanup", "failed", true, true, true, false, true, true, false},
		{"uncertain disabled", "disabled", true, false, true, false, true, true, false},
		{"available retry", "failed", false, true, false, false, true, true, false},
		{"enabled settings", "enabled", true, true, false, false, true, false, true},
		{"required", "enabled", true, true, false, true, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := adminEntry{ID: "sample", State: tc.state, Installed: tc.installed, DesiredEnabled: tc.desired, Unconfirmed: tc.unconfirmed, Required: tc.required, Generation: "3", SettingsPage: "main", AvailableVersion: "1.0"}
			var body bytes.Buffer
			require.NoError(t, view.Execute(&body, map[string]any{"ExtensionsEnabled": true, "CatalogSHA256": strings.Repeat("a", 64), "ExtensionEntries": []adminEntry{row}}))
			html := body.String()
			require.Equal(t, tc.disable, strings.Contains(html, `action="/admin/extensions/disable/sample"`))
			require.Equal(t, tc.enable, strings.Contains(html, `action="/admin/extensions/enable/sample"`))
			require.Equal(t, tc.settings, strings.Contains(html, `href="/admin/extensions/sample/settings/main"`))
			if tc.disable || tc.enable {
				require.Contains(t, html, `name="expected_generation" value="3"`)
				require.Contains(t, html, `name="catalog_sha256" value="`+strings.Repeat("a", 64)+`"`)
			}
		})
	}
}
