// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"context"
	"os"
	"testing"

	"code.forgejo.org/xorm/xorm"
	"code.forgejo.org/xorm/xorm/names"
	sdk "forgejo.org/extension-sdk"
	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	"forgejo.org/models/extensionauth"
	"forgejo.org/models/unittest"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/stretchr/testify/require"
)

// This selected fixture uses the same native token, binding, account and
// repository rows as the ordinary tests, with their engine on disposable PG.
func TestEnrolledSnapshotAuthorityOnPostgres(t *testing.T) {
	dsn := os.Getenv("FORGEJO_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("FORGEJO_TEST_PG_DSN is not set; needs a disposable PostgreSQL")
	}
	engine, err := xorm.NewEngine("pgx", dsn)
	require.NoError(t, err)
	engine.SetMapper(names.GonicMapper{})
	previous, ok := db.DefaultContext.(db.Engined)
	require.True(t, ok)
	db.SetDefaultEngine(context.Background(), engine)
	t.Cleanup(func() {
		db.SetDefaultEngine(context.Background(), previous.Engine())
		require.NoError(t, unittest.InitFixtures(unittest.FixturesOptions{Dir: unittest.FixturesDir()}))
		_ = engine.Close()
	})
	require.NoError(t, db.SyncAllTables())
	require.NoError(t, unittest.InitFixtures(unittest.FixturesOptions{Dir: unittest.FixturesDir()}))
	t.Run("prepared authority withdrawal", TestPreparedAdmissionRechecksPublicOnlyAfterRepositoryBecomesPrivate)
	t.Run("dependency credential intersection", TestSnapshotReadDependencyTargetsRespectEnrolledTokenRestrictions)
	t.Run("mixed scopes require both", func(t *testing.T) {
		unittest.PrepareTestEnv(t)
		ctx := t.Context()
		token := &auth_model.AccessToken{UID: 2, Name: "pg-mixed-read", Scope: "read:issue", ResourceAllRepos: true}
		require.NoError(t, auth_model.NewAccessToken(ctx, token))
		_, err := extensionauth.EnrollBinding(ctx, operationTestInstallation, token.ID, 2, 1, extensionauth.KindMerge)
		require.NoError(t, err)
		request := extensionauth.SnapshotReadRequest{
			InstallationID: operationTestInstallation, TokenSecret: token.Token,
			ActorID: 2, RepositoryID: 1, Families: []string{sdk.SnapshotFamilyIssue, sdk.SnapshotFamilyRefs},
		}
		_, err = extensionauth.VerifySnapshotRead(ctx, request)
		var refusal *extensionauth.RefusalError
		require.ErrorAs(t, err, &refusal)
		require.Equal(t, extensionauth.RefusalInsufficientScope, refusal.Code)
		token.Scope = "read:issue,read:repository"
		_, err = db.GetEngine(ctx).ID(token.ID).Cols("scope").Update(token)
		require.NoError(t, err)
		_, err = extensionauth.VerifySnapshotRead(ctx, request)
		require.NoError(t, err)
	})
}
