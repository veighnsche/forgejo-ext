// Copyright 2026 The Forgejo Authors.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	"forgejo.org/models/db"
	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addForeignKeyLFSMetaObject(t *testing.T) {
	type Pointer struct {
		Oid string `json:"oid" xorm:"UNIQUE(s) INDEX NOT NULL"`
	}

	type Repository struct {
		ID int64 `json:"id"`
	}

	type LFSMetaObject struct {
		ID           int64 `xorm:"pk autoincr"`
		Pointer      `xorm:"extends"`
		RepositoryID int64 `xorm:"UNIQUE(s) INDEX NOT NULL"`
	}

	type NewLFSMetaObject struct {
		ID           int64 `xorm:"pk autoincr"`
		Pointer      `xorm:"extends"`
		RepositoryID int64 `xorm:"UNIQUE(s) INDEX NOT NULL REFERENCES(repository, id)"`
	}

	// Prepare and load the testing database
	x, deferable := migration_tests.PrepareTestEnv(t, 0, new(LFSMetaObject), new(Repository))
	defer deferable()
	if x == nil || t.Failed() {
		return
	}

	cnt, err := x.Table("lfs_meta_object").Count()
	require.NoError(t, err)
	assert.EqualValues(t, 4, cnt)

	require.NoError(t, addForeignKeyLFSMetaObject(x))

	var remainingRecords []*NewLFSMetaObject
	require.NoError(t,
		db.GetEngine(t.Context()).
			Table("lfs_meta_object").
			Select("`id`, `oid`, `repository_id`").
			OrderBy("`id`").
			Find(&remainingRecords))
	assert.Equal(t,
		[]*NewLFSMetaObject{
			{
				ID:           1,
				Pointer:      Pointer{Oid: "0b8d8b5f15046343fd32f451df93acc2bdd9e6373be478b968e4cad6b6647351"},
				RepositoryID: 54,
			},
			{
				ID:           2,
				Pointer:      Pointer{Oid: "2eccdb43825d2a49d99d542daa20075cff1d97d9d2349a8977efe9c03661737c"},
				RepositoryID: 54,
			},
			{
				ID:           4,
				Pointer:      Pointer{Oid: "2eccdb43825d2a49d99d542daa20075cff1d97d9d2349a8977efe9c03661737c"},
				RepositoryID: 55,
			},
		}, remainingRecords)
}
