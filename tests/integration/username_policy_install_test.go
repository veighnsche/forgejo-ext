// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"database/sql"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"forgejo.org/modules/setting"

	"github.com/stretchr/testify/require"
)

// Exercise the real installer in a separate process: it reloads global settings,
// migrates the database, starts the required package and creates the first user.
//
//nolint:usetesting // Keep Unix socket paths short or retain the external fixture root.
func TestUsernamePolicyInstaller(t *testing.T) {
	if !setting.Database.Type.IsSQLite3() {
		t.Skip("isolated installer fixture uses SQLite")
	}
	for _, mode := range []string{"deny", "timeout", "missing", "allow"} {
		t.Run(mode, func(t *testing.T) {
			root, err := os.MkdirTemp(os.TempDir(), "p-")
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, os.RemoveAll(root)) })
			t.Cleanup(func() {
				if t.Failed() {
					data, _ := os.ReadFile(filepath.Join(root, "server.log"))
					t.Logf("installer errors: %s", data)
				}
			})
			packageRoot := filepath.Join(root, "e")
			executable, err := os.Executable()
			require.NoError(t, err)
			const manifest = `{"protocol":1,"id":"policy","name":"Policy","version":"1","executable":"run","capabilities":["native.contribution.authorize"],"policies":["forgejo.username"]}`
			var catalogPath string
			writePolicy := func(outcome string) {
				catalogPath = prepareUsernamePolicyPackage(t, packageRoot, manifest, executable, outcome, mode != "missing")
			}
			writePolicy(mode)
			configPath := filepath.Join(root, "app.ini")
			databasePath := filepath.Join(root, "data", "forgejo.db")
			configText := fmt.Sprintf("RUN_MODE = prod\n[server]\nHTTP_ADDR = 127.0.0.1\nSTATIC_ROOT_PATH = %s\nAPP_DATA_PATH = %s\nDISABLE_SSH = true\n[database]\nDB_TYPE = sqlite3\nPATH = %s\n[security]\nINSTALL_LOCK = false\n[extensions]\nENABLED = true\nPATH = %s\nCATALOG_PATH = %s\nREQUIRED_IDS = policy\n[log]\nMODE = console\nLEVEL = Error\n", setting.StaticRootPath, filepath.Join(root, "data"), databasePath, packageRoot, catalogPath)
			require.NoError(t, os.WriteFile(configPath, []byte(configText), 0o600))

			client := &http.Client{Timeout: 30 * time.Second}
			start := func() (string, func()) {
				listener, err := net.Listen("tcp", "127.0.0.1:0")
				require.NoError(t, err)
				address := listener.Addr().String()
				_, port, err := net.SplitHostPort(address)
				require.NoError(t, err)
				require.NoError(t, listener.Close())
				command := exec.Command(executable, "web", "--port", port)
				command.Env = append(os.Environ(), "GITEA_TEST_CLI=true", "GITEA_CONF="+configPath, "GITEA_WORK_DIR="+root, "FORGEJO_WORK_DIR="+root, "GITEA_CUSTOM="+filepath.Join(root, "custom"))
				logFile, err := os.Create(filepath.Join(root, "server.log"))
				require.NoError(t, err)
				command.Stdout, command.Stderr = logFile, logFile
				require.NoError(t, command.Start())
				stopped := false
				stop := func() {
					if !stopped {
						stopped = true
						_ = command.Process.Kill()
						_ = command.Wait()
						require.NoError(t, logFile.Close())
					}
				}
				t.Cleanup(stop)
				baseURL := "http://" + address
				require.Eventually(t, func() bool {
					response, err := client.Get(baseURL)
					if err != nil {
						return false
					}
					defer response.Body.Close()
					return response.StatusCode == http.StatusOK
				}, 15*time.Second, 50*time.Millisecond, "installer must start")
				return baseURL, stop
			}
			post := func(baseURL string) int {
				parsed, err := url.Parse(baseURL)
				require.NoError(t, err)
				response, err := client.PostForm(baseURL, url.Values{
					"db_type": {"sqlite3"}, "db_path": {databasePath}, "app_name": {"Policy fixture"},
					"repo_root_path": {filepath.Join(root, "repositories")}, "run_user": {setting.RunUser},
					"domain": {"127.0.0.1"}, "http_port": {parsed.Port()}, "app_url": {baseURL + "/"},
					"log_root_path": {filepath.Join(root, "log")}, "disable_registration": {"on"},
					"admin_name": {"policy-admin"}, "admin_email": {"policy-admin@example.com"},
					"admin_passwd": {"ExamplePassword!1"}, "admin_confirm_passwd": {"ExamplePassword!1"},
					"password_algorithm": {"pbkdf2"}, "reinstall_confirm_first": {"on"},
					"reinstall_confirm_second": {"on"}, "reinstall_confirm_third": {"on"},
				})
				require.NoError(t, err)
				_, err = io.Copy(io.Discard, response.Body)
				require.NoError(t, err)
				require.NoError(t, response.Body.Close())
				return response.StatusCode
			}
			baseURL, stop := start()
			expectedStatus := http.StatusServiceUnavailable
			if mode == "deny" {
				expectedStatus = http.StatusUnprocessableEntity
			} else if mode == "allow" {
				expectedStatus = http.StatusOK
			}
			require.Equal(t, expectedStatus, post(baseURL))
			stop()
			config, err := setting.NewConfigProviderFromFile(configPath)
			require.NoError(t, err)
			require.Equal(t, mode == "allow", config.Section("security").Key("INSTALL_LOCK").MustBool())
			database, err := sql.Open("sqlite3", databasePath)
			require.NoError(t, err)
			defer database.Close()
			var userCount, emailCount int
			require.NoError(t, database.QueryRow(`SELECT count(*) FROM "user" WHERE lower_name = 'policy-admin'`).Scan(&userCount))
			require.NoError(t, database.QueryRow(`SELECT count(*) FROM email_address WHERE email = 'policy-admin@example.com'`).Scan(&emailCount))
			if mode != "allow" {
				require.Zero(t, userCount)
				require.Zero(t, emailCount)
				require.NoDirExists(t, filepath.Join(root, "repositories", "policy-admin"))
				return
			}
			require.Equal(t, 1, userCount)
			require.Equal(t, 1, emailCount)
			var isAdmin, isActive bool
			require.NoError(t, database.QueryRow(`SELECT is_admin, is_active FROM "user" WHERE lower_name = 'policy-admin'`).Scan(&isAdmin, &isActive))
			require.True(t, isAdmin)
			require.True(t, isActive)
			var before string
			require.NoError(t, database.QueryRow(`SELECT passwd FROM "user" WHERE lower_name = 'policy-admin'`).Scan(&before))
			// Reinstallation retains an existing account even when new names are denied.
			config.Section("security").Key("INSTALL_LOCK").SetValue("false")
			require.NoError(t, config.SaveTo(configPath))
			writePolicy("deny")
			baseURL, stop = start()
			require.Equal(t, http.StatusOK, post(baseURL))
			stop()
			config, err = setting.NewConfigProviderFromFile(configPath)
			require.NoError(t, err)
			require.True(t, config.Section("security").Key("INSTALL_LOCK").MustBool())
			var after string
			require.NoError(t, database.QueryRow(`SELECT passwd FROM "user" WHERE lower_name = 'policy-admin'`).Scan(&after))
			require.Equal(t, before, after)
		})
	}
}
