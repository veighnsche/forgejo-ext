// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"os"
	"testing"

	"code.forgejo.org/xorm/xorm"
	"code.forgejo.org/xorm/xorm/names"
	_ "github.com/jackc/pgx/v5/stdlib" // pgx driver for the disposable PostgreSQL fixture

	"github.com/stretchr/testify/require"
)

// Test_addNativeOperationOnPostgres proves a fresh PostgreSQL initializes the
// native-operation tables and the idle reservation seed, and that a repeated
// upgrade is a no-op. It skips without FORGEJO_TEST_PG_DSN, so ordinary runs
// stay on SQLite.
func Test_addNativeOperationOnPostgres(t *testing.T) {
	dsn := os.Getenv("FORGEJO_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("FORGEJO_TEST_PG_DSN is not set; needs a disposable PostgreSQL")
	}
	x, err := xorm.NewEngine("pgx", dsn)
	require.NoError(t, err)
	x.SetMapper(names.GonicMapper{})
	t.Cleanup(func() { _ = x.Close() })

	_, err = x.Exec("DROP TABLE IF EXISTS `operation`")
	require.NoError(t, err)
	_, err = x.Exec("DROP TABLE IF EXISTS `reservation`")
	require.NoError(t, err)

	require.NoError(t, addNativeOperation(x))

	type reservationRow struct {
		ID       int64
		Revision int64
		Owner    string
	}
	var rows []reservationRow
	require.NoError(t, x.Table("reservation").Find(&rows))
	require.Len(t, rows, 1)
	require.Equal(t, int64(1), rows[0].ID)
	require.Equal(t, int64(1), rows[0].Revision)
	require.Empty(t, rows[0].Owner)

	// A repeated upgrade keeps the single seed row.
	require.NoError(t, addNativeOperation(x))
	rows = nil
	require.NoError(t, x.Table("reservation").Find(&rows))
	require.Len(t, rows, 1)

	_, err = x.Exec("DROP TABLE IF EXISTS `operation`")
	require.NoError(t, err)
	_, err = x.Exec("DROP TABLE IF EXISTS `reservation`")
	require.NoError(t, err)
}
