// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	sdk "forgejo.org/extension-sdk"
	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/extensions"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/tests"
	"github.com/stretchr/testify/require"
)

type integrationUsernamePolicy func(context.Context, string, sdk.PolicyRequest) (sdk.PolicyDecision, error)

func (f integrationUsernamePolicy) EvaluateRequiredPolicy(ctx context.Context, id string, request sdk.PolicyRequest) (sdk.PolicyDecision, error) {
	return f(ctx, id, request)
}

func TestUsernamePolicyHTTPEntrypoints(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.Service.EnableCaptcha, false)()
	previous := setting.Extensions
	setting.Extensions.Enabled = true
	setting.Extensions.RequiredIDs = []string{"policy"}
	t.Cleanup(func() { setting.Extensions = previous; extensions.SetPolicyRuntime(nil) })
	admin := loginUser(t, "user1")
	token := getUserToken(t, "user1", auth_model.AccessTokenScopeWriteAdmin)
	self := loginUser(t, "user2")
	for _, outcome := range []string{"deny", "unavailable", "timeout"} {
		t.Run(outcome, func(t *testing.T) {
			calls := 0
			extensions.SetPolicyRuntime(integrationUsernamePolicy(func(ctx context.Context, id string, request sdk.PolicyRequest) (sdk.PolicyDecision, error) {
				calls++
				require.False(t, db.InTransaction(ctx))
				require.Equal(t, sdk.PolicyForgejoUsername, id)
				if outcome == "timeout" {
					return sdk.PolicyDecision{}, context.DeadlineExceeded
				}
				return sdk.PolicyDecision{ReasonCode: "reserved"}, nil
			}))
			status := http.StatusUnprocessableEntity
			if outcome != "deny" {
				status = http.StatusServiceUnavailable
			}
			if outcome == "unavailable" {
				extensions.SetPolicyRuntime(nil)
			}
			signup := NewRequestWithValues(t, "POST", "/user/sign_up", map[string]string{
				"user_name": "policy-signup", "email": "policy-signup@example.com", "password": "ExamplePassword!1", "retype": "ExamplePassword!1",
			})
			MakeRequest(t, signup, status)
			admin.MakeRequest(t, NewRequestWithValues(t, "POST", "/admin/users/new", map[string]string{
				"user_name": "policy-admin", "email": "policy-admin@example.com", "password": "ExamplePassword!1", "login_type": "0",
			}), status)
			MakeRequest(t, NewRequestWithValues(t, "POST", "/api/v1/admin/users", map[string]string{
				"username": "policy-api", "email": "policy-api@example.com", "password": "ExamplePassword!1",
			}).AddTokenAuth(token), status)
			self.MakeRequest(t, NewRequestWithValues(t, "POST", "/user/settings", map[string]string{"name": "User2"}), status)
			for _, name := range []string{"policy-signup", "policy-admin", "policy-api"} {
				unittest.AssertNotExistsBean(t, &user_model.User{LowerName: name})
			}
			candidate := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
			require.Equal(t, "user2", candidate.Name)
			unittest.AssertNotExistsBean(t, &user_model.Redirect{LowerName: "user2"})
			if outcome != "unavailable" {
				require.Equal(t, 4, calls)
			}
		})
	}
}

// Used by the integration binary's existing SDK subprocess helper.
func testUsernamePolicies() map[string]sdk.PolicyHandler {
	mode := os.Getenv("FORGEJO_EXTENSION_TEST_USERNAME_POLICY")
	if mode == "" {
		return nil
	}
	return map[string]sdk.PolicyHandler{sdk.PolicyForgejoUsername: func(ctx context.Context, request sdk.PolicyRequest) (sdk.PolicyDecision, error) {
		if mode == "timeout" {
			<-ctx.Done()
			return sdk.PolicyDecision{}, ctx.Err()
		}
		return sdk.PolicyDecision{Allowed: mode == "allow", ReasonCode: "reserved"}, nil
	}}
}

func TestUsernamePolicyCLIEntrypoint(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	root, err := os.MkdirTemp(os.TempDir(), "p-")
	require.NoError(t, err)
	defer os.RemoveAll(root)
	packageDir := filepath.Join(root, "policy")
	require.NoError(t, os.MkdirAll(packageDir, 0o700))
	executable, err := os.Executable()
	require.NoError(t, err)

	manifest := `{"protocol":1,"id":"policy","name":"Policy","version":"1","executable":"run","capabilities":["native.contribution.authorize"],"policies":["forgejo.username"]}`
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "extension.json"), []byte(manifest), 0o600))
	config, err := setting.NewConfigProviderFromFile(setting.CustomConf)
	require.NoError(t, err)
	config.Section("extensions").Key("ENABLED").SetValue("true")
	config.Section("extensions").Key("PATH").SetValue(root)
	config.Section("extensions").Key("REQUIRED_IDS").SetValue("policy")
	config.Section("extensions").Key("SERVICE_CALLBACK_PATH").SetValue("")
	configPath := filepath.Join(root, "cli.ini")
	require.NoError(t, config.SaveTo(configPath))
	defer test.MockVariableValue(&setting.CustomConf, configPath)()
	for _, mode := range []string{"deny", "timeout", "allow"} {
		t.Run(mode, func(t *testing.T) {
			// The host deliberately does not inherit arbitrary environment into packages.
			script := "#!/bin/sh\nexport FORGEJO_EXTENSION_TEST_USERNAME_POLICY=" + mode + "\nexec '" + strings.ReplaceAll(executable, "'", "'\"'\"'") + "'\n"
			require.NoError(t, os.WriteFile(filepath.Join(packageDir, "run"), []byte(script), 0o700))
			name := "policy-cli-" + mode
			_, err := runMainApp("admin", "user", "create", "--username", name, "--email", name+"@example.com", "--password", "ExamplePassword!1")
			if mode == "allow" {
				require.NoError(t, err)
				unittest.AssertExistsAndLoadBean(t, &user_model.User{Name: name})
			} else {
				var exitErr *exec.ExitError
				require.ErrorAs(t, err, &exitErr)
				expected := "username rejected by required extension policy"
				if mode == "timeout" {
					expected = "required extension policy unavailable"
				}
				require.Contains(t, string(exitErr.Stderr), expected)
				unittest.AssertNotExistsBean(t, &user_model.User{Name: name})
				unittest.AssertNotExistsBean(t, &user_model.EmailAddress{Email: name + "@example.com"})
			}
		})
	}
}
