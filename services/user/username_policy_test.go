// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package user

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	sdk "forgejo.org/extension-sdk"
	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/extensions"
	"forgejo.org/modules/setting"
	"github.com/stretchr/testify/require"
)

type renamePolicy func(context.Context, string, sdk.PolicyRequest) (sdk.PolicyDecision, error)

func (f renamePolicy) EvaluateRequiredPolicy(ctx context.Context, id string, request sdk.PolicyRequest) (sdk.PolicyDecision, error) {
	return f(ctx, id, request)
}

func TestRenameRequiredPolicy(t *testing.T) {
	previous := setting.Extensions
	setting.Extensions.Enabled = true
	setting.Extensions.RequiredIDs = []string{"policy"}
	oldRoot := setting.RepoRootPath
	setting.RepoRootPath = t.TempDir()
	t.Cleanup(func() {
		setting.Extensions = previous
		setting.RepoRootPath = oldRoot
		extensions.SetPolicyRuntime(nil)
	})
	for _, admin := range []bool{false, true} {
		for _, name := range []string{"User2", "policy-renamed"} {
			for _, outcome := range []string{"allow", "deny", "unavailable", "timeout"} {
				t.Run(outcome+name+map[bool]string{true: "-admin", false: "-self"}[admin], func(t *testing.T) {
					require.NoError(t, unittest.PrepareTestDatabase())
					candidate := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
					original := *candidate
					originalRepo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
					originalPath := user_model.UserPath(original.Name)
					require.NoError(t, os.MkdirAll(originalPath, 0o700))
					marker := filepath.Join(originalPath, "marker")
					require.NoError(t, os.WriteFile(marker, []byte("keep"), 0o600))
					calls := 0
					extensions.SetPolicyRuntime(renamePolicy(func(ctx context.Context, id string, request sdk.PolicyRequest) (sdk.PolicyDecision, error) {
						calls++
						require.False(t, db.InTransaction(ctx))
						require.Equal(t, sdk.PolicyForgejoUsername, id)
						require.Equal(t, sdk.PolicyRequest{Operation: "rename", Username: name, UserID: "2"}, request)
						if outcome == "timeout" {
							return sdk.PolicyDecision{}, context.DeadlineExceeded
						}
						return sdk.PolicyDecision{Allowed: outcome == "allow", ReasonCode: "reserved"}, nil
					}))
					if outcome == "unavailable" {
						extensions.SetPolicyRuntime(nil)
					}
					var err error
					if admin {
						err = AdminRenameUser(db.DefaultContext, candidate, name)
					} else {
						err = RenameUser(db.DefaultContext, candidate, name)
					}
					if outcome == "allow" {
						require.NoError(t, err)
						require.Equal(t, name, candidate.Name)
						// Keep the next independent case's fixture filesystem empty.
						require.NoError(t, os.RemoveAll(user_model.UserPath(name)))
					} else {
						require.Error(t, err)
						require.Equal(t, original, *candidate)
						stored := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
						require.Equal(t, original.Name, stored.Name)
						repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
						require.Equal(t, originalRepo.OwnerName, repo.OwnerName)
						unittest.AssertNotExistsBean(t, &user_model.Redirect{LowerName: original.LowerName})
						data, readErr := os.ReadFile(marker)
						require.NoError(t, readErr)
						require.Equal(t, "keep", string(data))
					}
					if outcome != "unavailable" {
						require.Equal(t, 1, calls)
					}
				})
			}
		}
	}
}

func TestOrganizationRenameSkipsPersonalPolicy(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	previous := setting.Extensions
	setting.Extensions.RequiredIDs = []string{"missing"}
	t.Cleanup(func() { setting.Extensions = previous })
	org := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 3})
	require.NoError(t, RenameUser(db.DefaultContext, org, "User3"))
}
