// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package user_test

import (
	"context"
	"testing"

	sdk "forgejo.org/extension-sdk"
	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/extensions"
	"forgejo.org/modules/setting"
	"github.com/stretchr/testify/require"
)

type usernamePolicy func(context.Context, string, sdk.PolicyRequest) (sdk.PolicyDecision, error)

func (f usernamePolicy) EvaluateRequiredPolicy(ctx context.Context, id string, request sdk.PolicyRequest) (sdk.PolicyDecision, error) {
	return f(ctx, id, request)
}

func configureUsernamePolicy(t *testing.T) {
	t.Helper()
	previous := setting.Extensions
	setting.Extensions.Enabled = true
	setting.Extensions.RequiredIDs = []string{"policy"}
	t.Cleanup(func() { setting.Extensions = previous; extensions.SetPolicyRuntime(nil) })
}

func TestCreateUserRequiredPolicy(t *testing.T) {
	configureUsernamePolicy(t)
	for _, admin := range []bool{false, true} {
		for _, outcome := range []string{"allow", "deny", "unavailable", "timeout"} {
			t.Run(outcome+map[bool]string{false: "-normal", true: "-admin"}[admin], func(t *testing.T) {
				require.NoError(t, unittest.PrepareTestDatabase())
				candidate := &user_model.User{Name: "policy-tester", Email: "policy-tester@example.invalid"}
				calls := 0
				extensions.SetPolicyRuntime(usernamePolicy(func(ctx context.Context, id string, request sdk.PolicyRequest) (sdk.PolicyDecision, error) {
					calls++
					require.False(t, db.InTransaction(ctx))
					require.Equal(t, sdk.PolicyForgejoUsername, id)
					require.Equal(t, sdk.PolicyRequest{Operation: "create", Username: candidate.Name}, request)
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
					err = user_model.AdminCreateUser(db.DefaultContext, candidate)
				} else {
					err = user_model.CreateUser(db.DefaultContext, candidate)
				}
				if outcome == "allow" {
					require.NoError(t, err)
					unittest.AssertExistsAndLoadBean(t, &user_model.User{Name: candidate.Name})
				} else {
					require.Error(t, err)
					require.Zero(t, candidate.ID)
					unittest.AssertNotExistsBean(t, &user_model.User{Name: candidate.Name})
					unittest.AssertNotExistsBean(t, &user_model.EmailAddress{Email: candidate.Email})
				}
				if outcome != "unavailable" {
					require.Equal(t, 1, calls)
				}
			})
		}
	}
}

func TestFederatedUserSkipsPersonalUsernamePolicy(t *testing.T) {
	configureUsernamePolicy(t)
	for _, available := range []bool{true, false} {
		require.NoError(t, unittest.PrepareTestDatabase())
		extensions.SetPolicyRuntime(usernamePolicy(func(context.Context, string, sdk.PolicyRequest) (sdk.PolicyDecision, error) {
			t.Fatal("remote ActivityPub identity reached personal OS username policy")
			return sdk.PolicyDecision{ReasonCode: "invalid_host_login"}, nil
		}))
		if !available {
			extensions.SetPolicyRuntime(nil)
		}
		candidate := &user_model.User{Name: "@policy-tester@example.invalid", Email: "federated-policy-tester@example.invalid", Type: user_model.UserTypeActivityPubUser, ProhibitLogin: true}
		mapping := &user_model.FederatedUser{ExternalID: "remote-tester", FederationHostID: 1, InboxPath: "/inbox"}
		require.NoError(t, user_model.CreateFederatedUser(db.DefaultContext, candidate, mapping))
		unittest.AssertExistsAndLoadBean(t, &user_model.FederatedUser{UserID: candidate.ID})
	}
}

func TestRequiredUsernamePolicyRejectsTransactionalCaller(t *testing.T) {
	configureUsernamePolicy(t)
	require.NoError(t, unittest.PrepareTestDatabase())
	extensions.SetPolicyRuntime(usernamePolicy(func(context.Context, string, sdk.PolicyRequest) (sdk.PolicyDecision, error) {
		t.Fatal("policy RPC invoked inside a transaction")
		return sdk.PolicyDecision{}, nil
	}))
	ctx, committer, err := db.TxContext(db.DefaultContext)
	require.NoError(t, err)
	defer committer.Close()
	candidate := &user_model.User{Name: "policy-tester", Email: "policy-tester@example.invalid"}
	require.ErrorIs(t, user_model.CreateUser(ctx, candidate), extensions.ErrRequiredPolicyUnavailable)
	require.Zero(t, candidate.ID)
}

func TestNativeValidationPrecedesRequiredUsernamePolicy(t *testing.T) {
	configureUsernamePolicy(t)
	require.NoError(t, unittest.PrepareTestDatabase())
	extensions.SetPolicyRuntime(usernamePolicy(func(context.Context, string, sdk.PolicyRequest) (sdk.PolicyDecision, error) {
		t.Fatal("policy ran before native validation")
		return sdk.PolicyDecision{}, nil
	}))
	existing := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	require.Error(t, user_model.CreateUser(db.DefaultContext, &user_model.User{Name: "invalid name", Email: "valid@example.com"}))
	require.ErrorIs(t, user_model.CreateUser(db.DefaultContext, &user_model.User{Name: existing.Name, Email: "valid@example.com"}), user_model.ErrUserAlreadyExist{Name: existing.Name})
	err := user_model.CreateUser(db.DefaultContext, &user_model.User{Name: "policy-tester", Email: existing.Email})
	require.True(t, user_model.IsErrEmailAlreadyUsed(err))
}
