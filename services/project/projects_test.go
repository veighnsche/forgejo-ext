// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package project

import (
	"errors"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	project_model "forgejo.org/models/project"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/optional"
	project_module "forgejo.org/modules/project"
	project_structs "forgejo.org/modules/structs"
	"forgejo.org/modules/util"
	"forgejo.org/modules/validation"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	ownerID            = int64(2)
	orgOwnerID         = int64(3)
	repoID             = int64(2)
	isShowClosed       = false
	sortType           = ""
	templateType       = project_module.TemplateTypeNone
	cardType           = project_module.CardTypeTextOnly
	keyword            = "Title"
	projectTitle       = "Project"
	projectDescription = "Description"
	projectType1       = project_module.APIOwnerTypeOrganization
	projectType2       = project_module.TypeIndividual
	projectType3       = project_module.APIOwnerTypeRepository
	columnTitle1       = "Title 1"
	columnTitle2       = "Title 2"
	columnColor        = "#23adff"
	page               = 1
	pageSize           = 10
	notExistStr        = "not exist"
)

func TestMain(m *testing.M) {
	unittest.MainTest(m)
}

func TestGetProjectType(t *testing.T) {
	pT := GetAPIOwnerType(false, false)
	assert.Equal(t, project_module.APIOwnerTypeIndividual, pT)

	pT = GetAPIOwnerType(true, false)
	assert.Equal(t, project_module.APIOwnerTypeOrganization, pT)

	pT = GetAPIOwnerType(false, true)
	assert.Equal(t, project_module.APIOwnerTypeRepository, pT)

	pT = GetAPIOwnerType(true, true)
	assert.Equal(t, project_module.APIOwnerTypeOrganization, pT)
}

func TestGetSearchOpts(t *testing.T) {
	opts := GetSearchOpts(
		ownerID,
		isShowClosed,
		sortType,
		keyword,
		projectType1,
		page,
		pageSize,
	)
	require.NotNil(t, opts)
	assert.Equal(t, ownerID, opts.OwnerID)
	assert.Equal(t, optional.Some(isShowClosed), opts.IsClosed)
	assert.Equal(t, keyword, opts.Title)
	assert.Equal(t, projectType1, opts.Type.ToAPIOwnerType())
	assert.NotNil(t, opts.ListOptions)

	opts = GetSearchOpts(
		repoID,
		isShowClosed,
		sortType,
		keyword,
		projectType3,
		page,
		pageSize,
	)
	require.NotNil(t, opts)
	assert.Equal(t, repoID, opts.RepoID)
	assert.Equal(t, optional.Some(isShowClosed), opts.IsClosed)
	assert.Equal(t, keyword, opts.Title)
	assert.Equal(t, projectType3, opts.Type.ToAPIOwnerType())
	assert.NotNil(t, opts.ListOptions)

	opts = GetSearchOpts(
		repoID,
		!isShowClosed,
		"",
		"",
		projectType3,
	)
	require.NotNil(t, opts)
	assert.Equal(t, repoID, opts.RepoID)
	assert.Equal(t, optional.Some(!isShowClosed), opts.IsClosed)
	assert.Equal(t, db.SearchOrderByNewest, opts.OrderBy)
	assert.Empty(t, opts.Title)
	assert.Equal(t, projectType3, opts.Type.ToAPIOwnerType())
}

func TestListProjectByOptions(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	opts := &project_model.SearchOptions{
		OwnerID: 2,
		Type:    projectType2,
	}
	projects, err := ListProjectsByOptions(t.Context(), opts)
	require.NoError(t, err)
	assert.Equal(t, int64(4), projects[0].ID)
}

func TestGetIssues(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	nonExistingIssueID := int64(999)

	// Case 1: All issues in the list exist
	issue1 := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 1})
	issue2 := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 2})
	issue3 := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 3})
	issueIDs := []int64{issue1.ID, issue2.ID, issue3.ID}
	issueList, complete, err := GetIssues(t.Context(), issueIDs)
	require.NoError(t, err)
	assert.Len(t, issueList, 3)
	assert.True(t, complete)

	// Case 2: An issueID does not exist as issue in DB
	// If DB holds an item that is not in the list, then this issue does not belong in that project or column
	issueIDs = append(issueIDs, nonExistingIssueID)
	_, complete, err = GetIssues(t.Context(), issueIDs)
	require.NoError(t, err)
	assert.False(t, complete)
}

func TestCountProjectsByOptions(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	opts := &project_model.SearchOptions{
		OwnerID: 2,
		Type:    projectType2,
	}
	count, err := CountProjectsByOptions(t.Context(), opts)
	require.NoError(t, err)
	assert.Equal(t, int64(3), count)
}

func TestNewProject(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	org3 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 3})
	repo2 := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 2})

	opts := project_structs.CreateProjectOptions{
		Title:        "Test",
		Description:  "Test",
		TemplateType: project_module.APITemplateTypeNone.String(),
		CardType:     project_module.APICardTypeTextOnly.String(),
	}

	var nilRepo *repo_model.Repository

	proj, err := NewProject(&opts, user2, nilRepo, project_module.APIOwnerTypeIndividual)
	require.NoError(t, err)
	assert.Equal(t, opts.Title, proj.Title)
	assert.Equal(t, opts.Description, proj.Description)
	assert.Equal(t, project_module.TypeIndividual, proj.Type)

	opts = project_structs.CreateProjectOptions{
		Title:        "Test",
		Description:  "Test",
		TemplateType: project_module.APITemplateTypeNone.String(),
		CardType:     project_module.APICardTypeTextOnly.String(),
	}

	proj, err = NewProject(&opts, org3, nilRepo, project_module.APIOwnerTypeOrganization)
	require.NoError(t, err)
	assert.Equal(t, opts.Title, proj.Title)
	assert.Equal(t, opts.Description, proj.Description)
	assert.Equal(t, project_module.TypeOrganization, proj.Type)

	opts = project_structs.CreateProjectOptions{
		Title:        "Test",
		Description:  "Test",
		TemplateType: project_module.APITemplateTypeNone.String(),
		CardType:     project_module.APICardTypeTextOnly.String(),
	}

	proj, err = NewProject(&opts, user2, repo2, project_module.APIOwnerTypeRepository)
	require.NoError(t, err)
	assert.Equal(t, opts.Title, proj.Title)
	assert.Equal(t, opts.Description, proj.Description)
	assert.Equal(t, project_module.TypeRepository, proj.Type)

	opts = project_structs.CreateProjectOptions{
		Title:        "Test",
		Description:  "Test",
		TemplateType: project_module.APITemplateTypeNone.String(),
		CardType:     project_module.APICardTypeTextOnly.String(),
	}

	_, err = NewProject(&opts, user2, nilRepo, project_module.APIOwnerTypeRepository)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Repo type given, but repo struct was empty")

	invalidCardType := project_module.APICardType("invalid")
	invalidTemplateType := project_module.APITemplateType("invalid")
	invalidProjectType := project_module.APIOwnerType("99")

	opts = project_structs.CreateProjectOptions{
		Title:        "Test",
		Description:  "Test",
		TemplateType: project_module.APITemplateTypeNone.String(),
		CardType:     invalidCardType.String(),
	}

	_, err = NewProject(&opts, user2, nilRepo, project_module.APIOwnerTypeRepository)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Field APICardType")

	opts = project_structs.CreateProjectOptions{
		Title:        "Test",
		Description:  "Test",
		TemplateType: invalidTemplateType.String(),
		CardType:     project_module.APICardTypeTextOnly.String(),
	}

	_, err = NewProject(&opts, user2, nilRepo, project_module.APIOwnerTypeRepository)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Field APITemplateType")

	opts = project_structs.CreateProjectOptions{
		Title:        "Test",
		Description:  "Test",
		TemplateType: project_module.APITemplateTypeNone.String(),
		CardType:     project_module.APICardTypeTextOnly.String(),
	}

	_, err = NewProject(&opts, user2, nilRepo, invalidProjectType)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Field APIOwnerType")
}

func TestGetValidProjectColumnByID(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	validProjectID := int64(1)

	t.Run("Valid Column ID", func(t *testing.T) {
		validColumnID := int64(1)
		_, err := GetValidProjectColumnByID(t.Context(), validProjectID, validColumnID)
		require.NoError(t, err)
	})
	t.Run("Empty Column ID", func(t *testing.T) {
		emptyColumnID := int64(0)
		_, err := GetValidProjectColumnByID(t.Context(), validProjectID, emptyColumnID)
		assert.True(t, validation.IsErrNotValid(err))
	})
	t.Run("Not Existing Column ID", func(t *testing.T) {
		nonExistingColumnID := int64(99999)
		_, err := GetValidProjectColumnByID(t.Context(), validProjectID, nonExistingColumnID)
		assert.Contains(t, err.Error(), notExistStr)
	})
	t.Run("Different Column ID", func(t *testing.T) {
		differentColID := int64(4)
		_, err := GetValidProjectColumnByID(t.Context(), validProjectID, differentColID)
		assert.True(t, errors.Is(err, util.ErrInvalidArgument))
	})
}

func TestCreateColumnInProjectError(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	column1 := &project_model.Column{
		Title:     "Some title",
		ProjectID: 1,
		Color:     "Invalid Color",
	}

	err := CreateColumnInProject(t.Context(), column1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad color code")
}

func TestEditColumnInProjectError(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	column1 := &project_model.Column{
		ID:        1,
		Title:     "Some title",
		ProjectID: 1,
		Color:     "Invalid Color",
	}

	err := EditColumnInProject(t.Context(), column1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad color code")
}

func TestCRUDProject(t *testing.T) {
	project := &project_model.Project{
		OwnerID:      ownerID,
		Title:        projectTitle,
		Type:         projectType2,
		Description:  projectDescription,
		CreatorID:    ownerID,
		TemplateType: templateType,
		CardType:     cardType,
	}
	newTitle := "Updated Title"
	newDescription := "Updated Description"

	err := CreateProject(t.Context(), project)
	require.NoError(t, err)

	wantProject, err := GetProjectByIDForOwner(t.Context(), project.ID, ownerID)
	require.NoError(t, err)
	assert.Equal(t, wantProject.Title, projectTitle)

	t.Run("Wrong OwnerID", func(t *testing.T) {
		repoProject := &project_model.Project{
			RepoID:       repoID,
			Title:        projectTitle,
			Type:         projectType3.ToOwnerType(),
			Description:  projectDescription,
			CreatorID:    ownerID,
			TemplateType: templateType,
			CardType:     cardType,
		}

		orgProject := &project_model.Project{
			OwnerID:      orgOwnerID,
			Title:        projectTitle,
			Type:         projectType1.ToOwnerType(),
			Description:  projectDescription,
			CreatorID:    ownerID,
			TemplateType: templateType,
			CardType:     cardType,
		}

		err := CreateProject(t.Context(), repoProject)
		require.NoError(t, err)

		err = CreateProject(t.Context(), orgProject)
		require.NoError(t, err)

		_, err = GetProjectByIDForOwner(t.Context(), project.ID, 99)
		assert.True(t, errors.Is(err, util.ErrInvalidArgument))

		_, err = GetProjectByIDForOwner(t.Context(), repoProject.ID, 99)
		assert.True(t, errors.Is(err, util.ErrInvalidArgument))

		_, err = GetProjectByIDForOwner(t.Context(), orgProject.ID, 99)
		assert.True(t, errors.Is(err, util.ErrInvalidArgument))
	})

	updated := &project_structs.CreateProjectOptions{
		Title:       newTitle,
		Description: newDescription,
	}
	err = UpdateProject(t.Context(), wantProject, updated)
	require.NoError(t, err)
	assert.Equal(t, wantProject.Title, newTitle)
	assert.Equal(t, wantProject.Description, newDescription)

	t.Run("TestCRUDColumn", func(t *testing.T) {
		column1 := &project_model.Column{
			Title:     columnTitle1,
			ProjectID: project.ID,
		}

		column2 := &project_model.Column{
			Title:     columnTitle2,
			ProjectID: project.ID,
			Color:     columnColor,
		}

		err := CreateColumnInProject(t.Context(), column1)
		require.NoError(t, err)
		assert.True(t, column1.Default)

		wantCol1, _ := GetValidProjectColumnByID(t.Context(), project.ID, column1.ID)
		assert.Equal(t, wantCol1.Title, columnTitle1)

		err = CreateColumnInProject(t.Context(), column2)
		require.NoError(t, err)

		wantCol2, err := GetValidProjectColumnByID(t.Context(), project.ID, column2.ID)
		require.NoError(t, err)
		assert.Equal(t, wantCol2.Title, columnTitle2)
		assert.False(t, wantCol2.Default)

		err = DeleteColumnInProject(t.Context(), column1.ID)
		require.Error(t, err) // Can not delete default col

		err = DeleteColumnInProject(t.Context(), column2.ID)
		require.NoError(t, err)
		unittest.AssertNotExistsBean(t, &project_model.Column{ID: column2.ID})
	})

	t.Run("TestCRUDProjectIssues", func(t *testing.T) {
		// The yml's in models/fixtures provide information about the test DB
		require.NoError(t, unittest.PrepareTestDatabase())
		column1 := unittest.AssertExistsAndLoadBean(t, &project_model.Column{ID: 1})
		project := unittest.AssertExistsAndLoadBean(t, &project_model.Project{ID: 1})
		pI1 := unittest.AssertExistsAndLoadBean(t, &project_model.ProjectIssue{ID: 1})
		pI2 := unittest.AssertExistsAndLoadBean(t, &project_model.ProjectIssue{ID: 2})

		pIs := &project_structs.MovedIssuesOption{
			ProjectIssues: []struct {
				IssueID int64 "json:\"issueID\""
				Sorting int64 "json:\"sorting\""
			}{
				{
					IssueID: pI1.IssueID,
					Sorting: int64(2),
				},
				{
					IssueID: pI2.IssueID,
					Sorting: int64(1),
				},
			},
		}
		err = MoveIssuesOnProjectColumn(t.Context(), column1, pIs)
		require.NoError(t, err)

		pI1 = unittest.AssertExistsAndLoadBean(t, &project_model.ProjectIssue{ID: 1})
		pI2 = unittest.AssertExistsAndLoadBean(t, &project_model.ProjectIssue{ID: 2})
		assert.Equal(t, int64(2), pI1.Sorting)
		assert.Equal(t, int64(1), pI2.Sorting)
		assert.Equal(t, int64(1), pI1.ProjectColumnID)
		assert.Equal(t, int64(1), pI2.ProjectColumnID)

		defaultCol, err := project.GetDefaultColumn(t.Context())
		require.NoError(t, err)

		defaultIssues, _, err := defaultCol.GetIssues(t.Context(), db.ListOptionsAll)
		require.NoError(t, err)

		assert.Contains(t, defaultIssues, pI1)
		assert.Contains(t, defaultIssues, pI2)
	})

	err = DeleteProjectByID(t.Context(), wantProject.ID, optional.None[int64]())
	require.NoError(t, err)
}
