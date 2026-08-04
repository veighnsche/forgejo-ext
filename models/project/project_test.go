// Copyright 2020 The Gitea Authors. All rights reserved.
// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package project

import (
	"testing"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/optional"
	project_module "forgejo.org/modules/project"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetProjects(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	projects, err := db.Find[Project](db.DefaultContext, SearchOptions{RepoID: 1})
	require.NoError(t, err)

	// 1 value for this repo exists in the fixtures
	assert.Len(t, projects, 1)

	projects, err = db.Find[Project](db.DefaultContext, SearchOptions{RepoID: 3})
	require.NoError(t, err)

	// 1 value for this repo exists in the fixtures
	assert.Len(t, projects, 1)
}

func TestCreateDeleteProject(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	user1 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})

	project := &Project{
		Title:        "Testproject",
		Description:  "Test",
		OwnerID:      user1.ID,
		Owner:        user1,
		RepoID:       0,
		Repo:         &repo_model.Repository{},
		CreatorID:    user1.ID,
		IsClosed:     false,
		TemplateType: project_module.TemplateTypeNone,
		CardType:     project_module.CardTypeTextOnly,
		Type:         project_module.TypeIndividual,
	}

	err := CreateProject(t.Context(), project)
	require.NoError(t, err)

	// try and create duplicate project
	err = CreateProject(t.Context(), project)
	assert.Contains(t, err.Error(), "unique constraint violation")

	err = DeleteProjectByID(t.Context(), project.ID, optional.None[int64]())
	require.NoError(t, err)
}

func TestProjectsSort(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	tests := []struct {
		sortType string
		wants    []int64
	}{
		{
			sortType: "default",
			wants:    []int64{1, 3, 2, 7, 6, 5, 4},
		},
		{
			sortType: "oldest",
			wants:    []int64{4, 5, 6, 7, 2, 3, 1},
		},
		{
			sortType: "recentupdate",
			wants:    []int64{1, 3, 2, 7, 6, 5, 4},
		},
		{
			sortType: "leastupdate",
			wants:    []int64{4, 5, 6, 7, 2, 3, 1},
		},
	}

	for _, tt := range tests {
		projects, count, err := db.FindAndCount[Project](db.DefaultContext, SearchOptions{
			OrderBy: GetSearchOrderBySortType(tt.sortType),
		})
		require.NoError(t, err)
		assert.Equal(t, int64(7), count)
		if assert.Len(t, projects, 7) {
			for i := range projects {
				assert.Equal(t, tt.wants[i], projects[i].ID)
			}
		}
	}
}

func TestChangeProjectStatus(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	t.Run("Unchanged", func(t *testing.T) {
		project := unittest.AssertExistsAndLoadBean(t, &Project{ID: 1})

		require.NoError(t, ChangeProjectStatus(t.Context(), project, project.IsClosed))

		projectAfter := unittest.AssertExistsAndLoadBean(t, &Project{ID: 1})
		assert.Equal(t, project.IsClosed, projectAfter.IsClosed)
	})

	t.Run("Normal", func(t *testing.T) {
		project := unittest.AssertExistsAndLoadBean(t, &Project{ID: 1})
		isClosed := !project.IsClosed
		repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: project.RepoID})

		require.NoError(t, ChangeProjectStatus(t.Context(), project, isClosed))

		projectAfter := unittest.AssertExistsAndLoadBean(t, &Project{ID: 1})
		repoAfter := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: project.RepoID})
		assert.Equal(t, isClosed, projectAfter.IsClosed)
		assert.Equal(t, repo.NumProjects, repoAfter.NumProjects)
		assert.Equal(t, repo.NumOpenProjects-1, repoAfter.NumOpenProjects)
		assert.Equal(t, repo.NumClosedProjects+1, repoAfter.NumClosedProjects)
	})

	t.Run("Invalid ID", func(t *testing.T) {
		project := &Project{ID: 1001, RepoID: 1}
		repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: project.RepoID})

		require.NoError(t, ChangeProjectStatus(t.Context(), project, true))

		repoAfter := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: project.RepoID})
		assert.Equal(t, repo.NumProjects, repoAfter.NumProjects)
		assert.Equal(t, repo.NumOpenProjects, repoAfter.NumOpenProjects)
		assert.Equal(t, repo.NumClosedProjects, repoAfter.NumClosedProjects)
	})
}
