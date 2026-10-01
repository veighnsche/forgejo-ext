// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"context"
	"testing"

	actions_model "forgejo.org/models/actions"
	"forgejo.org/models/db"
	model "forgejo.org/models/nativeoperation"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/require"
)

func TestParseAuthorityResource(t *testing.T) {
	cases := []struct {
		resource string
		op       string
		id1, id2 int64
		ok       bool
	}{
		{"user/2/update", "user/update", 2, 0, true},
		{"team/1/member/2", "team/member", 1, 2, true},
		{"user/2/email/activate", "user/email/activate", 2, 0, true},
		{"repo/1/deploy-key/7", "repo/deploy-key", 1, 7, true},
		{"repo/1/deploy-key", "repo/deploy-key", 1, 0, true},
		{"key/4", "key", 4, 0, true},
		{"users/delete-inactive", "", 0, 0, false},
		{"directory-sync", "", 0, 0, false},
		{"admin/regenerate-hooks", "", 0, 0, false},
		{"user/someone", "", 0, 0, false},
		{"org/somename", "", 0, 0, false},
		{"auth-source/somename", "", 0, 0, false},
		{"team/1/member/2/extra/3", "", 0, 0, false},
		{"user/0/update", "", 0, 0, false},
		{"", "", 0, 0, false},
	}
	for _, tc := range cases {
		op, id1, id2, ok := parseAuthorityResource(tc.resource)
		require.Equal(t, tc.ok, ok, "resource %q", tc.resource)
		if !tc.ok {
			continue
		}
		require.Equal(t, tc.op, op, "resource %q", tc.resource)
		require.Equal(t, tc.id1, id1, "resource %q", tc.resource)
		require.Equal(t, tc.id2, id2, "resource %q", tc.resource)
	}
}

func TestRecoverAuthorityUserConsistent(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	useIsolatedAppData(t)
	svc := admissionService(t, nil, 0)

	claimed := claimOrdinaryOwner(t, ctx, "ord:authority/user/2/update/nonce", Scope{
		Kind:        model.OwnerOrdinary,
		Family:      FamilyAuthority,
		AuthorityOp: "user/update",
		AuthorityID: 2,
	})
	inhibitDomain(t)

	assessment, err := svc.Recover(ctx, claimed.Owner, claimed.Generation)
	require.NoError(t, err)
	require.Equal(t, RecoveryReleased, assessment.Verdict)
	require.Equal(t, EffectAuthorityConsistent, assessment.Effect)
	require.Equal(t, FamilyAuthority, assessment.Family)
}

func TestRecoverAuthorityUserMissing(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	useIsolatedAppData(t)
	svc := admissionService(t, nil, 0)

	claimed := claimOrdinaryOwner(t, ctx, "ord:authority/user/9999/update/nonce", Scope{
		Kind:        model.OwnerOrdinary,
		Family:      FamilyAuthority,
		AuthorityOp: "user/update",
		AuthorityID: 9999,
	})
	inhibitDomain(t)

	assessment, err := svc.Recover(ctx, claimed.Owner, claimed.Generation)
	require.NoError(t, err)
	require.Equal(t, RecoveryFenced, assessment.Verdict)
	require.Equal(t, ReasonRecoveryUnaccounted, assessment.Reason)

	held, err := model.ReadReservation(ctx)
	require.NoError(t, err)
	require.Equal(t, claimed.Owner, held.Owner)
}

func TestRecoverAuthorityTeamMember(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	useIsolatedAppData(t)
	svc := admissionService(t, nil, 0)

	claimed := claimOrdinaryOwner(t, ctx, "ord:authority/team/1/member/2/nonce", Scope{
		Kind:         model.OwnerOrdinary,
		Family:       FamilyAuthority,
		AuthorityOp:  "team/member",
		AuthorityID:  1,
		AuthorityID2: 2,
	})
	inhibitDomain(t)

	assessment, err := svc.Recover(ctx, claimed.Owner, claimed.Generation)
	require.NoError(t, err)
	require.Equal(t, RecoveryReleased, assessment.Verdict)
	require.Equal(t, EffectAuthorityConsistent, assessment.Effect)
}

func TestRecoverAuthorityOrgTypeMismatch(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	useIsolatedAppData(t)
	svc := admissionService(t, nil, 0)

	claimed := claimOrdinaryOwner(t, ctx, "ord:authority/org/2/email/nonce", Scope{
		Kind:        model.OwnerOrdinary,
		Family:      FamilyAuthority,
		AuthorityOp: "org/email",
		AuthorityID: 2,
	})
	inhibitDomain(t)

	assessment, err := svc.Recover(ctx, claimed.Owner, claimed.Generation)
	require.NoError(t, err)
	require.Equal(t, RecoveryFenced, assessment.Verdict)
	require.Equal(t, ReasonRecoveryUnaccounted, assessment.Reason)
}

func TestRecoverAuthorityCrossFamilyDeleteStaysFenced(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	useIsolatedAppData(t)
	svc := admissionService(t, nil, 0)

	claimed := claimOrdinaryOwner(t, ctx, "ord:authority/user/2/delete/nonce", Scope{
		Kind:        model.OwnerOrdinary,
		Family:      FamilyAuthority,
		AuthorityOp: "user/delete",
		AuthorityID: 2,
	})
	inhibitDomain(t)

	assessment, err := svc.Recover(ctx, claimed.Owner, claimed.Generation)
	require.NoError(t, err)
	require.Equal(t, RecoveryFenced, assessment.Verdict)
	require.Equal(t, ReasonRecoveryUnaccounted, assessment.Reason)

	held, err := model.ReadReservation(ctx)
	require.NoError(t, err)
	require.Equal(t, claimed.Owner, held.Owner)
}

func TestRecoverAuthorityMissingOpStaysFenced(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	useIsolatedAppData(t)
	svc := admissionService(t, nil, 0)

	claimed := claimOrdinaryOwner(t, ctx, "ord:authority/directory-sync/nonce", Scope{
		Kind:   model.OwnerOrdinary,
		Family: FamilyAuthority,
	})
	inhibitDomain(t)

	assessment, err := svc.Recover(ctx, claimed.Owner, claimed.Generation)
	require.NoError(t, err)
	require.Equal(t, RecoveryFenced, assessment.Verdict)
	require.Equal(t, ReasonRecoveryUnknownFamily, assessment.Reason)
}

func TestRecoverAuthorityUnknownOpStaysFenced(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	useIsolatedAppData(t)
	svc := admissionService(t, nil, 0)

	claimed := claimOrdinaryOwner(t, ctx, "ord:authority/user/2/future/nonce", Scope{
		Kind:        model.OwnerOrdinary,
		Family:      FamilyAuthority,
		AuthorityOp: "user/future",
		AuthorityID: 2,
	})
	inhibitDomain(t)

	assessment, err := svc.Recover(ctx, claimed.Owner, claimed.Generation)
	require.NoError(t, err)
	require.Equal(t, RecoveryFenced, assessment.Verdict)
	require.Equal(t, ReasonRecoveryUnknownFamily, assessment.Reason)
}

func TestRecoverAuthorityAbsentTokenReleases(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	useIsolatedAppData(t)
	svc := admissionService(t, nil, 0)

	claimed := claimOrdinaryOwner(t, ctx, "ord:authority/token/9999/nonce", Scope{
		Kind:        model.OwnerOrdinary,
		Family:      FamilyAuthority,
		AuthorityOp: "token",
		AuthorityID: 9999,
	})
	inhibitDomain(t)

	assessment, err := svc.Recover(ctx, claimed.Owner, claimed.Generation)
	require.NoError(t, err)
	require.Equal(t, RecoveryReleased, assessment.Verdict)
	require.Equal(t, EffectAuthorityConsistent, assessment.Effect)
}

func TestRecoverAuthorityCollaborator(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	useIsolatedAppData(t)
	svc := admissionService(t, nil, 0)

	claimed := claimOrdinaryOwner(t, ctx, "ord:authority/repo/1/collaborator/2/nonce", Scope{
		Kind:         model.OwnerOrdinary,
		RepositoryID: 1,
		Family:       FamilyAuthority,
		AuthorityOp:  "repo/collaborator",
		AuthorityID:  1,
		AuthorityID2: 2,
	})
	inhibitDomain(t)

	assessment, err := svc.Recover(ctx, claimed.Owner, claimed.Generation)
	require.NoError(t, err)
	require.Equal(t, RecoveryReleased, assessment.Verdict)
	require.Equal(t, EffectAuthorityConsistent, assessment.Effect)
}

func TestRecoverAuthorityAbsentDeployKeyReleases(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	useIsolatedAppData(t)
	svc := admissionService(t, nil, 0)

	claimed := claimOrdinaryOwner(t, ctx, "ord:authority/repo/1/deploy-key/9999/nonce", Scope{
		Kind:         model.OwnerOrdinary,
		RepositoryID: 1,
		Family:       FamilyAuthority,
		AuthorityOp:  "repo/deploy-key",
		AuthorityID:  1,
		AuthorityID2: 9999,
	})
	inhibitDomain(t)

	assessment, err := svc.Recover(ctx, claimed.Owner, claimed.Generation)
	require.NoError(t, err)
	require.Equal(t, RecoveryReleased, assessment.Verdict)
	require.Equal(t, EffectAuthorityConsistent, assessment.Effect)
}

func TestAuthorityClaimRecordsAttributableScope(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	useIsolatedAppData(t)

	err := WithAuthorityOwnership(ctx, "team/1/member/2", 0, func(ctx context.Context) error {
		reservation, err := model.ReadReservation(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, reservation.Owner)
		scope, err := decodeScope(reservation.Scope)
		require.NoError(t, err)
		require.Equal(t, FamilyAuthority, scope.Family)
		require.Equal(t, "team/member", scope.AuthorityOp)
		require.Equal(t, int64(1), scope.AuthorityID)
		require.Equal(t, int64(2), scope.AuthorityID2)
		return nil
	})
	require.NoError(t, err)

	err = WithAuthorityOwnership(ctx, "directory-sync", 0, func(ctx context.Context) error {
		reservation, err := model.ReadReservation(ctx)
		require.NoError(t, err)
		scope, err := decodeScope(reservation.Scope)
		require.NoError(t, err)
		require.Empty(t, scope.AuthorityOp)
		return nil
	})
	require.NoError(t, err)
}

func insertDeletedUser(t *testing.T, ctx context.Context, name string) int64 {
	t.Helper()
	user := &user_model.User{Name: name, LowerName: name, Email: name + "@example.com"}
	unittest.AssertSuccessfulInsert(t, user)
	_, err := db.GetEngine(ctx).ID(user.ID).Delete(new(user_model.User))
	require.NoError(t, err)
	return user.ID
}

func TestRecoverAuthorityUserDeleteReleased(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	useIsolatedAppData(t)
	svc := admissionService(t, nil, 0)

	userID := insertDeletedUser(t, ctx, "ft09-gone")
	claimed := claimOrdinaryOwner(t, ctx, "ord:authority/user/9001/delete/nonce", Scope{
		Kind:        model.OwnerOrdinary,
		Family:      FamilyAuthority,
		AuthorityOp: "user/delete",
		AuthorityID: userID,
	})
	inhibitDomain(t)

	assessment, err := svc.Recover(ctx, claimed.Owner, claimed.Generation)
	require.NoError(t, err)
	require.Equal(t, RecoveryReleased, assessment.Verdict)
	require.Equal(t, "deleted", assessment.Effect)
}

func TestRecoverAuthorityUserDeletePresentFences(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	useIsolatedAppData(t)
	svc := admissionService(t, nil, 0)

	claimed := claimOrdinaryOwner(t, ctx, "ord:authority/user/2/delete/nonce", Scope{
		Kind:        model.OwnerOrdinary,
		Family:      FamilyAuthority,
		AuthorityOp: "user/delete",
		AuthorityID: 2,
	})
	inhibitDomain(t)

	assessment, err := svc.Recover(ctx, claimed.Owner, claimed.Generation)
	require.NoError(t, err)
	require.Equal(t, RecoveryFenced, assessment.Verdict)
	require.Equal(t, ReasonRecoveryUnaccounted, assessment.Reason)
}

func TestRecoverAuthorityUserDeleteDanglingFences(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	useIsolatedAppData(t)
	svc := admissionService(t, nil, 0)

	userID := insertDeletedUser(t, ctx, "ft09-dangling")
	unittest.AssertSuccessfulInsert(t, &user_model.EmailAddress{UID: userID, Email: "ft09-dangling@example.com"})
	claimed := claimOrdinaryOwner(t, ctx, "ord:authority/user/9002/delete/nonce", Scope{
		Kind:        model.OwnerOrdinary,
		Family:      FamilyAuthority,
		AuthorityOp: "user/delete",
		AuthorityID: userID,
	})
	inhibitDomain(t)

	assessment, err := svc.Recover(ctx, claimed.Owner, claimed.Generation)
	require.NoError(t, err)
	require.Equal(t, RecoveryFenced, assessment.Verdict)
	require.Equal(t, ReasonRecoveryUnaccounted, assessment.Reason)
	require.Contains(t, assessment.Checks[0], "dangling email=1")
}

func TestRecoverAuthorityOrgDeleteReleased(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	useIsolatedAppData(t)
	svc := admissionService(t, nil, 0)

	userID := insertDeletedUser(t, ctx, "ft09-org-gone")
	claimed := claimOrdinaryOwner(t, ctx, "ord:authority/org/9003/delete/nonce", Scope{
		Kind:        model.OwnerOrdinary,
		Family:      FamilyAuthority,
		AuthorityOp: "org/delete",
		AuthorityID: userID,
	})
	inhibitDomain(t)

	assessment, err := svc.Recover(ctx, claimed.Owner, claimed.Generation)
	require.NoError(t, err)
	require.Equal(t, RecoveryReleased, assessment.Verdict)
	require.Equal(t, "deleted", assessment.Effect)
}

func TestRecoverAuthorityOrgDeletePresentFences(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	useIsolatedAppData(t)
	svc := admissionService(t, nil, 0)

	claimed := claimOrdinaryOwner(t, ctx, "ord:authority/org/3/delete/nonce", Scope{
		Kind:        model.OwnerOrdinary,
		Family:      FamilyAuthority,
		AuthorityOp: "org/delete",
		AuthorityID: 3,
	})
	inhibitDomain(t)

	assessment, err := svc.Recover(ctx, claimed.Owner, claimed.Generation)
	require.NoError(t, err)
	require.Equal(t, RecoveryFenced, assessment.Verdict)
	require.Equal(t, ReasonRecoveryUnaccounted, assessment.Reason)
}

func TestRecoverAuthorityUserBlockCommitted(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	useIsolatedAppData(t)
	svc := admissionService(t, nil, 0)

	unittest.AssertSuccessfulInsert(t, &user_model.BlockedUser{UserID: 2, BlockID: 4242})
	claimed := claimOrdinaryOwner(t, ctx, "ord:authority/user/2/block/4242/nonce", Scope{
		Kind:         model.OwnerOrdinary,
		Family:       FamilyAuthority,
		AuthorityOp:  "user/block",
		AuthorityID:  2,
		AuthorityID2: 4242,
	})
	inhibitDomain(t)

	assessment, err := svc.Recover(ctx, claimed.Owner, claimed.Generation)
	require.NoError(t, err)
	require.Equal(t, RecoveryReleased, assessment.Verdict)
	require.Equal(t, EffectAuthorityConsistent, assessment.Effect)
}

func TestRecoverAuthorityUserBlockAbsent(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	useIsolatedAppData(t)
	svc := admissionService(t, nil, 0)

	claimed := claimOrdinaryOwner(t, ctx, "ord:authority/user/2/block/4243/nonce", Scope{
		Kind:         model.OwnerOrdinary,
		Family:       FamilyAuthority,
		AuthorityOp:  "user/block",
		AuthorityID:  2,
		AuthorityID2: 4243,
	})
	inhibitDomain(t)

	assessment, err := svc.Recover(ctx, claimed.Owner, claimed.Generation)
	require.NoError(t, err)
	require.Equal(t, RecoveryReleased, assessment.Verdict)
	require.Equal(t, EffectAuthorityConsistent, assessment.Effect)
}

func TestRecoverAuthorityUserBlockSurvivorFences(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	useIsolatedAppData(t)
	svc := admissionService(t, nil, 0)

	blocked := &user_model.User{Name: "ft09-blocked", LowerName: "ft09-blocked", Email: "ft09-blocked@example.com"}
	unittest.AssertSuccessfulInsert(t, blocked)
	unittest.AssertSuccessfulInsert(t, &user_model.BlockedUser{UserID: 2, BlockID: blocked.ID})
	unittest.AssertSuccessfulInsert(t, &actions_model.ActionUser{UserID: blocked.ID, RepoID: 1})
	claimed := claimOrdinaryOwner(t, ctx, "ord:authority/user/2/block/4244/nonce", Scope{
		Kind:         model.OwnerOrdinary,
		Family:       FamilyAuthority,
		AuthorityOp:  "user/block",
		AuthorityID:  2,
		AuthorityID2: blocked.ID,
	})
	inhibitDomain(t)

	assessment, err := svc.Recover(ctx, claimed.Owner, claimed.Generation)
	require.NoError(t, err)
	require.Equal(t, RecoveryFenced, assessment.Verdict)
	require.Equal(t, ReasonRecoveryUnaccounted, assessment.Reason)
}
