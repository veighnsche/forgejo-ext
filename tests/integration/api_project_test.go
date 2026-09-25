// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"testing"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/organization"
	"forgejo.org/models/perm"
	project_model "forgejo.org/models/project"
	"forgejo.org/models/unit"
	"forgejo.org/models/unittest"
	project_module "forgejo.org/modules/project"
	"forgejo.org/modules/structs"
	api "forgejo.org/modules/structs"
	"forgejo.org/services/convert"
	"forgejo.org/tests"
	"forgejo.org/tests/forgery"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type runOpts struct {
	token         string
	owner         string
	repo          string
	projectID     int64
	shouldSucceed bool
	ownerType     project_module.APIOwnerType
}

func getProjectAPIBaseString(opts *runOpts) string {
	var projectAPIBaseString string
	switch opts.ownerType {
	case project_module.APIOwnerTypeIndividual:
		projectAPIBaseString = "/api/v1/users/" + opts.owner
	case project_module.APIOwnerTypeOrganization:
		projectAPIBaseString = "/api/v1/orgs/" + opts.owner
	case project_module.APIOwnerTypeRepository:
		projectAPIBaseString = "/api/v1/repos/" + opts.owner + "/" + opts.repo
	}
	return projectAPIBaseString
}

// createProject creates a project.
func createProject(t *testing.T, runOpts *runOpts, projectName string) api.Project {
	var project api.Project
	endpoint := getProjectAPIBaseString(runOpts) + "/projects"
	resp := jsonRequestWithAuth(t, runOpts.token, "POST", endpoint, http.StatusCreated,
		&api.CreateOrUpdateProjectOptions{
			Title:        projectName,
			Description:  projectName,
			TemplateType: project_module.APITemplateTypeNone.String(),
			CardType:     project_module.APICardTypeTextOnly.String(),
		},
	)
	DecodeJSON(t, resp, &project)
	return project
}

// createProjectColumn creates a column.
func createProjectColumn(t *testing.T, runOpts *runOpts, columnName string) api.ProjectColumn {
	var projectColumn api.ProjectColumn
	endpoint := getProjectAPIBaseString(runOpts) + fmt.Sprintf("/projects/%d/columns", runOpts.projectID)
	resp := jsonRequestWithAuth(t, runOpts.token, "POST",
		endpoint,
		http.StatusCreated,
		api.CreateProjectColumnOptions{
			Title: columnName,
		},
	)
	DecodeJSON(t, resp, &projectColumn)
	return projectColumn
}

// createProjectIssue creates an issue.
func createProjectIssue(t *testing.T, runOpts *runOpts, columnID int64, issueName string) api.ProjectIssue {
	// create issue
	var issue api.Issue
	resp := jsonRequestWithAuth(t, runOpts.token, "POST",
		fmt.Sprintf("/api/v1/repos/%s/%s/issues?state=all", runOpts.owner, runOpts.repo),
		http.StatusCreated,
		&api.CreateIssueOption{
			Body:  issueName,
			Title: issueName,
		},
	)
	DecodeJSON(t, resp, &issue)

	// create project issue
	var projectIssue api.ProjectIssue
	endpoint := getProjectAPIBaseString(runOpts) + fmt.Sprintf("/projects/%d/columns/%d/issues", runOpts.projectID, columnID)
	if columnID == 0 {
		endpoint = getProjectAPIBaseString(runOpts) + fmt.Sprintf("/projects/%d/issues", runOpts.projectID)
	}
	resp = jsonRequestWithAuth(t, runOpts.token, "POST", endpoint,
		http.StatusCreated,
		&api.CreateProjectIssueOptions{
			IssueID: issue.ID,
		},
	)
	DecodeJSON(t, resp, &projectIssue)
	return projectIssue
}

// TestProjectAPIListProjectsPagination tests ListProjects in the Project API
// with pagination.
func TestProjectAPIListProjectsPagination(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	err := unittest.PrepareTestDatabase()
	require.NoError(t, err)

	// user and token, repo
	user := forgery.CreateUser(t, nil)
	session := loginUser(t, user.Name)
	token := getTokenForLoggedInUser(t, session, auth_model.AccessTokenScopeWriteProject)
	repo := forgery.CreateRepository(t, user, nil)

	// create projects
	numProjects := 100
	projects := []*api.Project{}
	for range numProjects {
		projects = append(projects, convert.ToAPIProject(forgery.CreateProject(t, repo, nil)))
	}

	// list projects
	limit := 10   // maximum number of entries in api call response
	numCalls := 0 // number of performed api calls
	gotProjects := []*api.Project{}
	for i := range numProjects {
		var projResp []*api.Project
		resp := requestWithAuth(
			t, token, "GET",
			fmt.Sprintf(
				"/api/v1/repos/%v/%v/projects?page=%d&limit=%d",
				user.Name,
				repo.Name,
				i+1,
				limit,
			),
			http.StatusOK,
		)
		DecodeJSON(t, resp, &projResp)
		numCalls++

		assert.Equal(t, strconv.Itoa(numProjects), resp.Result().Header.Get("X-Total-Count"))
		assert.NotEmpty(t, resp.Result().Header.Get("Link"))
		gotProjects = append(gotProjects, projResp...)

		if len(gotProjects) == numProjects {
			break
		}
	}
	assert.Len(t, gotProjects, numProjects)
	assert.Equal(t, projects, gotProjects)
	assert.Equal(t, numProjects/limit, numCalls)
}

// TestProjectAPIListProjectColumnsPagination tests ListProjectColumns in the
// Project API with pagination.
func TestProjectAPIListProjectColumnsPagination(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	err := unittest.PrepareTestDatabase()
	require.NoError(t, err)

	// user and token, repo
	user := forgery.CreateUser(t, nil)
	session := loginUser(t, user.Name)
	token := getTokenForLoggedInUser(t, session, auth_model.AccessTokenScopeWriteProject)
	repo := forgery.CreateRepository(t, user, nil)

	// create project
	project := forgery.CreateProject(t, repo, nil)

	// create columns
	numColumns := 20
	columns := []api.ProjectColumn{}
	for i := range numColumns {
		n := fmt.Sprintf("column-%d", i)
		columns = append(columns,
			createProjectColumn(t, &runOpts{
				token:     token,
				owner:     user.LowerName,
				repo:      repo.LowerName,
				ownerType: project_module.APIOwnerTypeRepository,
				projectID: project.ID,
			}, n))
	}

	// list columns
	limit := 2    // maximum number of entries in api call response
	numCalls := 0 // number of performed api calls
	gotColumns := []api.ProjectColumn{}
	for i := range numColumns {
		var colResp []api.ProjectColumn
		resp := requestWithAuth(
			t, token, "GET",
			fmt.Sprintf(
				"/api/v1/repos/%v/%v/projects/%d/columns?page=%d&limit=%d",
				user.Name,
				repo.Name,
				project.ID,
				i+1,
				limit,
			),
			http.StatusOK,
		)
		DecodeJSON(t, resp, &colResp)
		numCalls++

		assert.Equal(t, strconv.Itoa(numColumns), resp.Result().Header.Get("X-Total-Count"))
		assert.NotEmpty(t, resp.Result().Header.Get("Link"))
		gotColumns = append(gotColumns, colResp...)

		if len(gotColumns) == numColumns {
			break
		}
	}
	assert.Len(t, gotColumns, numColumns)
	assert.Equal(t, columns, gotColumns)
	assert.Equal(t, numColumns/limit, numCalls)
}

// TestProjectAPIListProjectIssuesPagination tests ListProjectColumnIssues and
// ListProjectIssues in the Project API with pagination.
func TestProjectAPIListProjectIssuesPagination(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	err := unittest.PrepareTestDatabase()
	require.NoError(t, err)

	// user and token, repo
	user := forgery.CreateUser(t, nil)
	session := loginUser(t, user.Name)
	token := getTokenForLoggedInUser(t, session,
		auth_model.AccessTokenScopeWriteProject,
		auth_model.AccessTokenScopeWriteIssue,
	)
	repo := forgery.CreateRepository(t, user, nil)

	// create project
	project := forgery.CreateProject(t, repo, nil)

	// create column
	column := createProjectColumn(t, &runOpts{
		token:     token,
		owner:     user.LowerName,
		repo:      repo.LowerName,
		ownerType: project_module.APIOwnerTypeRepository,
		projectID: project.ID,
	}, "test-column")

	// create issues
	numIssues := 100
	issues := []api.ProjectIssue{}
	for i := range numIssues {
		n := fmt.Sprintf("issue-%d", i)
		issues = append(issues,
			createProjectIssue(t,
				&runOpts{
					token:     token,
					owner:     user.LowerName,
					repo:      repo.LowerName,
					projectID: project.ID,
					ownerType: project_module.APIOwnerTypeRepository,
				}, column.ID, n))
	}

	// list issues
	listIssues := func(t *testing.T, url string) {
		limit := 10   // maximum number of entries in api call response
		numCalls := 0 // number of performed api calls
		gotIssues := []api.ProjectIssue{}
		for i := range numIssues {
			var issueResp []api.ProjectIssue
			resp := requestWithAuth(
				t, token, "GET",
				fmt.Sprintf(
					"%s?page=%d&limit=%d",
					url,
					i+1,
					limit,
				),
				http.StatusOK,
			)
			DecodeJSON(t, resp, &issueResp)
			numCalls++

			assert.Equal(t, strconv.Itoa(numIssues), resp.Result().Header.Get("X-Total-Count"))
			assert.NotEmpty(t, resp.Result().Header.Get("Link"))
			gotIssues = append(gotIssues, issueResp...)

			if len(gotIssues) == numIssues {
				break
			}
		}
		assert.Len(t, gotIssues, numIssues)
		assert.Equal(t, issues, gotIssues)
		assert.Equal(t, numIssues/limit, numCalls)
	}
	t.Run("ListProjectColumnIssues", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		url := fmt.Sprintf(
			"/api/v1/repos/%v/%v/projects/%d/columns/%d/issues",
			user.Name,
			repo.Name,
			project.ID,
			column.ID,
		)
		listIssues(t, url)
	})
	t.Run("ListProjectIssues", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		url := fmt.Sprintf(
			"/api/v1/repos/%s/%s/projects/%d/issues",
			user.Name,
			repo.Name,
			project.ID,
		)
		listIssues(t, url)
	})
}

// Test the use cases
func TestProjectAPICRUD(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	err := unittest.PrepareTestDatabase()
	require.NoError(t, err)

	// user and token, repo
	user := forgery.CreateUser(t, nil)
	session := loginUser(t, user.Name)
	writeToken := getTokenForLoggedInUser(t, session,
		auth_model.AccessTokenScopeWriteProject,
		auth_model.AccessTokenScopeWriteIssue,
	)
	readToken := getTokenForLoggedInUser(t, session,
		auth_model.AccessTokenScopeReadProject,
	)
	repo := forgery.CreateRepository(t, user, nil)

	isClosed := func(s string) bool {
		if s == "closed" {
			return true
		}
		return false
	}

	// Create, Get project for an owner
	project := createProject(t, &runOpts{token: writeToken, owner: user.Name, ownerType: project_module.APIOwnerTypeIndividual}, "Project 1")

	assert.NotZero(t, project.ID)
	assert.Equal(t, "Project 1", project.Title)
	assert.Equal(t, "Project 1", project.Description)
	assert.Equal(t, user.Name, project.OwnerName)
	assert.Empty(t, project.RepoName)
	assert.Equal(t, "open", project.Status)
	assert.Equal(t, project_module.APITemplateTypeNone.String(), project.TemplateType)
	assert.Equal(t, project_module.APICardTypeTextOnly.String(), project.CardType)

	userGetEndpoint := fmt.Sprintf("/api/v1/users/%v", user.Name)
	resp := getProject(t, readToken, userGetEndpoint, project.ID, http.StatusOK)
	var projResp api.Project
	DecodeJSON(t, resp, &projResp)

	assert.Equal(t, project.ID, projResp.ID)

	// Create project for a repository
	t.Run("Create, Get project for repository", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		project := createProject(t, &runOpts{token: writeToken, owner: user.Name, repo: repo.Name, ownerType: project_module.APIOwnerTypeRepository}, "Project 2")

		assert.NotZero(t, project.ID)
		assert.Equal(t, "Project 2", project.Title)
		assert.Equal(t, "Project 2", project.Description)
		assert.Equal(t, repo.Name, project.RepoName)
		assert.Equal(t, "open", project.Status)
		assert.Equal(t, project_module.APITemplateTypeNone.String(), project.TemplateType)
		assert.Equal(t, project_module.APICardTypeTextOnly.String(), project.CardType)

		repoGetEndpoint := fmt.Sprintf("/api/v1/repos/%v/%v", user.Name, repo.LowerName)
		resp = getProject(t, readToken, repoGetEndpoint, project.ID, http.StatusOK)

		var projResp2 api.Project
		DecodeJSON(t, resp, &projResp2)

		assert.Equal(t, project.ID, projResp2.ID)
	})

	// First column is always default column
	projectColumn1 := createProjectColumn(t, &runOpts{
		token:     writeToken,
		owner:     user.Name,
		ownerType: project_module.APIOwnerTypeIndividual,
		projectID: project.ID,
	}, "Col1")

	// Color can be nil
	assert.NotZero(t, projectColumn1.ID)
	assert.Equal(t, "Col1", projectColumn1.Title)
	assert.Equal(t, project.ID, projectColumn1.ProjectID)
	assert.True(t, projectColumn1.Default)
	assert.NotNil(t, projectColumn1.Sorting) // Sorting is zero by default, but "NOT NULL" according to DB model

	projectColumn2 := createProjectColumn(t, &runOpts{
		token:     writeToken,
		owner:     user.Name,
		ownerType: project_module.APIOwnerTypeIndividual,
		projectID: project.ID,
	}, "Col2")

	assert.NotZero(t, projectColumn2.ID)
	assert.NotEqual(t, projectColumn1.ID, projectColumn2.ID)
	assert.Equal(t, "Col2", projectColumn2.Title)
	assert.Equal(t, project.ID, projectColumn2.ProjectID)
	assert.False(t, projectColumn2.Default)
	assert.NotEqual(t, projectColumn1.Sorting, projectColumn2.Sorting)

	// Add issue to a project, to the default column
	projectIssue1 := createProjectIssue(t,
		&runOpts{
			token:     writeToken,
			owner:     user.Name,
			repo:      repo.Name,
			projectID: project.ID,
			ownerType: project_module.APIOwnerTypeIndividual,
		}, 0, "TestIssue")

	assert.NotZero(t, projectIssue1.ID)
	assert.Equal(t, project.ID, projectIssue1.ProjectID)
	assert.Equal(t, projectColumn1.ID, projectIssue1.ProjectColumnID)
	assert.NotNil(t, projectIssue1.Sorting) // Sorting is zero by default, but "NOT NULL" according to DB model

	// Add issue directly to a column of a project
	projectIssue2 := createProjectIssue(t,
		&runOpts{
			token:     writeToken,
			owner:     user.Name,
			repo:      repo.Name,
			projectID: project.ID,
			ownerType: project_module.APIOwnerTypeIndividual,
		}, projectColumn1.ID, "TestIssue2")

	assert.NotZero(t, projectIssue2.ID)
	assert.NotEqual(t, projectIssue1.ID, projectIssue2.ID)
	assert.Equal(t, project.ID, projectIssue2.ProjectID)
	assert.Equal(t, projectColumn1.ID, projectIssue2.ProjectColumnID)
	assert.NotEqual(t, projectIssue1.Sorting, projectIssue2.Sorting)

	// Update properties of a project
	t.Run("Update properties of project", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		projOpts := api.CreateOrUpdateProjectOptions{
			Title:       "Project 15",
			Description: "ABC",
			CardType:    project_module.APICardTypeImagesAndText.String(),
			Status:      project.Status,
		}
		endpoint := fmt.Sprintf("/api/v1/users/%v/projects/%v", user.Name, project.ID)
		jsonRequestWithAuth(t, writeToken, "PATCH", endpoint, http.StatusOK, projOpts)
		p := unittest.AssertExistsAndLoadBean(t, &project_model.Project{ID: project.ID})

		assert.Equal(t, projOpts.Title, p.Title)
		assert.Equal(t, projOpts.Description, p.Description)
		assert.Equal(t, projOpts.CardType, p.CardType.ToAPICardType().String())
		assert.Equal(t, isClosed(projOpts.Status), p.IsClosed)
	})

	// Change status of a project (open, closed)
	t.Run("Change status of project", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		// close project
		projOpts := api.CreateOrUpdateProjectOptions{
			Title:       "Project 15",
			Description: "ABC",
			CardType:    project_module.APICardTypeImagesAndText.String(),
			Status:      project_module.APIStatusClosed.String(),
		}
		endpoint := fmt.Sprintf("/api/v1/users/%v/projects/%v", user.Name, project.ID)
		jsonRequestWithAuth(t, writeToken, "PATCH", endpoint, http.StatusOK, projOpts)

		p := unittest.AssertExistsAndLoadBean(t, &project_model.Project{ID: project.ID})
		assert.Equal(t, isClosed(projOpts.Status), p.IsClosed)

		// re-open project
		projOpts.Status = project_module.APIStatusOpen.String()
		jsonRequestWithAuth(t, writeToken, "PATCH", endpoint, http.StatusOK, projOpts)

		p = unittest.AssertExistsAndLoadBean(t, &project_model.Project{ID: project.ID})
		assert.Equal(t, isClosed(projOpts.Status), p.IsClosed)
	})

	// Update properties of a column
	t.Run("Update properties of column", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		colOpts := api.CreateProjectColumnOptions{
			Color:   "#00aabb",
			Title:   "Backlog",
			Default: projectColumn1.Default,
			Sorting: projectColumn1.Sorting,
		}
		endpoint := fmt.Sprintf("/api/v1/users/%v/projects/%v/columns/%v", user.Name, project.ID, projectColumn1.ID)
		jsonRequestWithAuth(t, writeToken, "PATCH", endpoint, http.StatusOK, colOpts)

		c := unittest.AssertExistsAndLoadBean(t, &project_model.Column{ID: projectColumn1.ID, ProjectID: project.ID})

		assert.Equal(t, colOpts.Color, c.Color)
		assert.Equal(t, colOpts.Title, c.Title)
	})

	// Set default column of a project
	t.Run("Set default column of project", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		// set second column as default
		colOpts := api.CreateProjectColumnOptions{
			Default: true,
			Sorting: projectColumn2.Sorting,
			Title:   projectColumn2.Title,
			Color:   projectColumn2.Color,
		}
		endpoint := fmt.Sprintf("/api/v1/users/%v/projects/%v/columns/%v", user.Name, project.ID, projectColumn2.ID)
		jsonRequestWithAuth(t, writeToken, "PATCH", endpoint, http.StatusOK, colOpts)
		c1 := unittest.AssertExistsAndLoadBean(t, &project_model.Column{ID: projectColumn1.ID, ProjectID: project.ID})
		c2 := unittest.AssertExistsAndLoadBean(t, &project_model.Column{ID: projectColumn2.ID, ProjectID: project.ID})

		assert.False(t, c1.Default)
		assert.True(t, c2.Default)

		// set first column as default again
		colOpts = api.CreateProjectColumnOptions{
			Default: true,
			Sorting: projectColumn1.Sorting,
			Title:   projectColumn1.Title,
			Color:   projectColumn1.Color,
		}
		endpoint = fmt.Sprintf("/api/v1/users/%v/projects/%v/columns/%v", user.Name, project.ID, projectColumn1.ID)
		jsonRequestWithAuth(t, writeToken, "PATCH", endpoint, http.StatusOK, colOpts)
		c1 = unittest.AssertExistsAndLoadBean(t, &project_model.Column{ID: projectColumn1.ID, ProjectID: project.ID})
		c2 = unittest.AssertExistsAndLoadBean(t, &project_model.Column{ID: projectColumn2.ID, ProjectID: project.ID})

		assert.True(t, c1.Default)
		assert.False(t, c2.Default)
	})

	// Reorder column in a project
	t.Run("Reorder column in project", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		// move second column to first column's position
		colOpts := api.CreateProjectColumnOptions{
			Sorting: projectColumn1.Sorting,
			Default: projectColumn2.Default,
			Title:   projectColumn2.Title,
			Color:   projectColumn2.Color,
		}
		endpoint := fmt.Sprintf("/api/v1/users/%v/projects/%v/columns/%v", user.Name, project.ID, projectColumn2.ID)
		jsonRequestWithAuth(t, writeToken, "PATCH", endpoint, http.StatusOK, colOpts)

		// query new values explicitly
		c1 := unittest.AssertExistsAndLoadBean(t, &project_model.Column{ID: projectColumn1.ID, ProjectID: project.ID})
		c2 := unittest.AssertExistsAndLoadBean(t, &project_model.Column{ID: projectColumn2.ID, ProjectID: project.ID})

		assert.Less(t, c2.Sorting, c1.Sorting)

		// move first column back to first position
		colOpts = api.CreateProjectColumnOptions{
			Sorting: projectColumn1.Sorting,
			Default: projectColumn1.Default,
			Title:   projectColumn1.Title,
			Color:   projectColumn1.Color,
		}
		endpoint = fmt.Sprintf("/api/v1/users/%v/projects/%v/columns/%v", user.Name, project.ID, projectColumn1.ID)
		jsonRequestWithAuth(t, writeToken, "PATCH", endpoint, http.StatusOK, colOpts)

		c1 = unittest.AssertExistsAndLoadBean(t, &project_model.Column{ID: projectColumn1.ID, ProjectID: project.ID})
		c2 = unittest.AssertExistsAndLoadBean(t, &project_model.Column{ID: projectColumn2.ID, ProjectID: project.ID})

		assert.Less(t, c1.Sorting, c2.Sorting)
	})

	// Reorder issue in a column of a project
	t.Run("Reorder issue in column of project", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		assert.Equal(t, projectIssue1.ProjectColumnID, projectIssue2.ProjectColumnID)

		// move second issue to first issue's position
		updatePCIOpts := api.UpdateProjectColumnIssueOptions{
			ProjectColumnID: projectIssue2.ProjectColumnID,
			Sorting:         projectIssue1.Sorting,
		}
		endpoint := fmt.Sprintf("/api/v1/users/%v/projects/%v/columns/%v/issues/%v", user.Name, project.ID, projectIssue2.ProjectColumnID, projectIssue2.ID)
		jsonRequestWithAuth(t, writeToken, "PATCH", endpoint, http.StatusOK, updatePCIOpts)

		i1 := unittest.AssertExistsAndLoadBean(t, &project_model.ProjectIssue{ID: projectIssue1.ID, ProjectID: project.ID})
		i2 := unittest.AssertExistsAndLoadBean(t, &project_model.ProjectIssue{ID: projectIssue2.ID, ProjectID: project.ID})

		assert.Less(t, i2.Sorting, i1.Sorting)

		// move first issue back to first position
		updatePCIOpts = api.UpdateProjectColumnIssueOptions{
			ProjectColumnID: projectColumn1.ID,
			Sorting:         projectIssue1.Sorting,
		}
		endpoint = fmt.Sprintf("/api/v1/users/%v/projects/%v/columns/%v/issues/%v", user.Name, project.ID, projectIssue1.ProjectColumnID, projectIssue1.ID)
		jsonRequestWithAuth(t, writeToken, "PATCH", endpoint, http.StatusOK, updatePCIOpts)

		i1 = unittest.AssertExistsAndLoadBean(t, &project_model.ProjectIssue{ID: projectIssue1.ID, ProjectID: project.ID})
		i2 = unittest.AssertExistsAndLoadBean(t, &project_model.ProjectIssue{ID: projectIssue2.ID, ProjectID: project.ID})

		assert.Less(t, i1.Sorting, i2.Sorting)
	})

	// Move issue from one column to a different column of the same project
	t.Run("Move issue to other column of project", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		// move second issue to second column
		updatePCIOpts := api.UpdateProjectColumnIssueOptions{
			ProjectColumnID: projectColumn2.ID,
		}
		endpoint := fmt.Sprintf("/api/v1/users/%v/projects/%v/columns/%v/issues/%v", user.Name, project.ID, projectColumn1.ID, projectIssue2.ID)
		jsonRequestWithAuth(t, writeToken, "PATCH", endpoint, http.StatusOK, updatePCIOpts)

		i2 := unittest.AssertExistsAndLoadBean(t, &project_model.ProjectIssue{ID: projectIssue2.ID, ProjectID: project.ID})

		assert.Equal(t, updatePCIOpts.ProjectColumnID, i2.ProjectColumnID)

		// move second issue back to first column
		updatePCIOpts = api.UpdateProjectColumnIssueOptions{
			ProjectColumnID: projectColumn1.ID,
		}
		endpoint = fmt.Sprintf("/api/v1/users/%v/projects/%v/columns/%v/issues/%v", user.Name, project.ID, projectColumn2.ID, projectIssue2.ID)
		jsonRequestWithAuth(t, writeToken, "PATCH", endpoint, http.StatusOK, updatePCIOpts)

		i2 = unittest.AssertExistsAndLoadBean(t, &project_model.ProjectIssue{ID: projectIssue2.ID, ProjectID: project.ID})

		assert.Equal(t, updatePCIOpts.ProjectColumnID, i2.ProjectColumnID)
	})

	// List projects of an owner (user/organization)
	t.Run("List projects of owner", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		var projResp []*api.Project
		endpoint := fmt.Sprintf("/api/v1/users/%v/projects", user.Name)
		resp := requestWithAuth(t, readToken, "GET", endpoint, http.StatusOK)
		DecodeJSON(t, resp, &projResp)

		// there are already some projects in the db for user2
		if !slices.ContainsFunc(projResp, func(p *api.Project) bool {
			return p.ID == project.ID
		}) {
			t.Error("project not in project list")
		}
	})

	// List projects of a repository
	t.Run("List projects of repository", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		var projResp []api.Project
		endpoint := fmt.Sprintf("/api/v1/repos/%v/%v/projects", user.Name, repo.Name)
		resp := requestWithAuth(t, readToken, "GET", endpoint, http.StatusOK)
		DecodeJSON(t, resp, &projResp)

		assert.NotEmpty(t, projResp)
	})

	// List colums of a project
	t.Run("List columns of a project", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		var colResp []api.ProjectColumn
		endpoint := fmt.Sprintf("/api/v1/users/%v/projects/%v/columns", user.Name, project.ID)
		resp := requestWithAuth(t, readToken, "GET", endpoint, http.StatusOK)
		DecodeJSON(t, resp, &colResp)

		// We make sure the order is fixed
		assert.Equal(t, projectColumn1.ID, colResp[0].ID)
		assert.Equal(t, projectColumn2.ID, colResp[1].ID)
	})

	// List issues in a column of a project
	t.Run("List issues in a column of a project", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		var issueResp []*api.ProjectIssue
		endpoint := fmt.Sprintf("/api/v1/users/%v/projects/%v/columns/%v/issues", user.Name, project.ID, projectColumn1.ID)
		resp := requestWithAuth(t, readToken, "GET", endpoint, http.StatusOK)
		DecodeJSON(t, resp, &issueResp)

		if !slices.ContainsFunc(issueResp, func(p *api.ProjectIssue) bool {
			return p.ID == projectIssue1.ID
		}) {
			t.Error("project issue not in project issue list")
		}
	})

	// List issues in a project
	t.Run("List issues in a a project", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		var issueResp []*api.ProjectIssue
		endpoint := fmt.Sprintf("/api/v1/users/%v/projects/%v/issues", user.Name, project.ID)
		resp := requestWithAuth(t, readToken, "GET", endpoint, http.StatusOK)
		DecodeJSON(t, resp, &issueResp)

		if !slices.ContainsFunc(issueResp, func(p *api.ProjectIssue) bool {
			return p.ID == projectIssue1.ID
		}) {
			t.Error("project issue not in project issue list")
		}
	})

	// Remove issue from (a column of) a project
	t.Run("Remove issue from a column of a project", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		endpoint := fmt.Sprintf("/api/v1/users/%v/projects/%v/columns/%v/issues/%v", user.Name, project.ID, projectColumn1.ID, projectIssue1.ID)
		requestWithAuth(t, writeToken, "DELETE", endpoint, http.StatusOK)

		unittest.AssertNotExistsBean(t, &project_model.ProjectIssue{
			ID:        projectIssue1.ID,
			IssueID:   projectIssue1.IssueID,
			ProjectID: projectIssue1.ProjectID,
		})
	})

	// Remove column from a project
	t.Run("Remove column from a project", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		// cannot delete default column -> delete column 2
		endpoint := fmt.Sprintf("/api/v1/users/%v/projects/%v/columns/%v", user.Name, project.ID, projectColumn2.ID)
		requestWithAuth(t, writeToken, "DELETE", endpoint, http.StatusOK)

		unittest.AssertNotExistsBean(t, &project_model.Column{
			ID:        projectColumn2.ID,
			ProjectID: projectColumn2.ProjectID,
		})
	})

	// Remove project
	t.Run("Remove project", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		endpoint := fmt.Sprintf("/api/v1/users/%v/projects/%v", user.Name, project.ID)
		requestWithAuth(t, writeToken, "DELETE", endpoint, http.StatusOK)

		unittest.AssertNotExistsBean(t, &project_model.Project{
			ID: project.ID,
		})
	})
}

func addOrRemoveTeamUser(t *testing.T, token, userName, method string, teamID int64) *httptest.ResponseRecorder {
	endpoint := fmt.Sprintf("/api/v1//teams/%v/members/%v", teamID, userName)
	return requestWithAuth(t, token, method, endpoint, NoExpectedStatus)
}

func addorRemoveCollaboratorToRepo(t *testing.T, token, owner, repoName, user, method string, opts *api.AddCollaboratorOption) *httptest.ResponseRecorder {
	endpoint := fmt.Sprintf("/api/v1/repos/%v/%v/collaborators/%v", owner, repoName, user)
	return jsonRequestWithAuth(t, token, method, endpoint, NoExpectedStatus, opts)
}

func getProject(t *testing.T, token, projectAPIBaseString string, pID int64, status int) *httptest.ResponseRecorder {
	return projectsIDEndpoint(t, token, "GET", projectAPIBaseString, pID, status)
}

func deleteProject(t *testing.T, token, projectAPIBaseString string, pID int64, status int) *httptest.ResponseRecorder {
	return projectsIDEndpoint(t, token, "DELETE", projectAPIBaseString, pID, status)
}

func projectsIDEndpoint(t *testing.T, token, method, projectAPIBaseString string, pID int64, status int) *httptest.ResponseRecorder {
	projectAPIString := fmt.Sprintf("%v/projects/%v", projectAPIBaseString, pID)
	return requestWithAuth(t, token, method, projectAPIString, status)
}

func requestWithAuth(
	t *testing.T,
	token, method, endpoint string,
	status int,
) *httptest.ResponseRecorder {
	req := NewRequest(t, method, endpoint).AddTokenAuth(token)
	return MakeRequest(t, req, status)
}

func jsonRequestWithAuth(t *testing.T, token, method, endpoint string, statusCode int, opts any) *httptest.ResponseRecorder {
	req := NewRequestWithJSON(
		t, method,
		endpoint,
		&opts,
	).AddTokenAuth(token)
	resp := MakeRequest(t, req, statusCode)
	return resp
}

func runProjectWriteActions(t *testing.T, runOpts *runOpts, projectOpts *api.CreateOrUpdateProjectOptions) {
	projectAPIBaseString := getProjectAPIBaseString(runOpts)
	// Create Project
	endpoint := projectAPIBaseString + "/projects"
	resp := jsonRequestWithAuth(t, runOpts.token, "POST", endpoint, NoExpectedStatus, projectOpts)
	var proj *api.Project
	if runOpts.shouldSucceed {
		require.Equal(t, http.StatusCreated, resp.Code)
		DecodeJSON(t, resp, &proj)
		assert.Equal(t, projectOpts.Title, proj.Title)
		// Delete Project
		deleteProject(t, runOpts.token, projectAPIBaseString, proj.ID, http.StatusOK)
	} else {
		assert.NotEqual(t, http.StatusCreated, resp.Code)
	}
}

func runProjectReadActions(t *testing.T, opts *runOpts) {
	projectAPIBaseString := getProjectAPIBaseString(opts)
	// Get Project
	resp := getProject(t, opts.token, projectAPIBaseString, opts.projectID, NoExpectedStatus)
	if opts.shouldSucceed {
		assert.Equal(t, http.StatusOK, resp.Code)
	} else {
		assert.NotEqual(t, http.StatusOK, resp.Code)
	}
}

func TestProjectAPIPermissionHandling(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	err := unittest.PrepareTestDatabase()
	require.NoError(t, err)

	// users and tokens
	user1 := forgery.CreateUser(t, &forgery.CreateUserOptions{IsAdmin: true})
	user2 := forgery.CreateUser(t, nil)

	session1 := loginUser(t, user1.Name)
	adminWriteToken := getTokenForLoggedInUser(t,
		session1,
		auth_model.AccessTokenScopeWriteProject,
		auth_model.AccessTokenScopeWriteOrganization,
		auth_model.AccessTokenScopeWriteRepository,
		auth_model.AccessTokenScopeWriteUser,
		auth_model.AccessTokenScopeWriteIssue,
	)

	session2 := loginUser(t, user2.Name)
	userWriteToken := getTokenForLoggedInUser(t,
		session2,
		auth_model.AccessTokenScopeWriteProject,
		auth_model.AccessTokenScopeWriteOrganization,
		auth_model.AccessTokenScopeWriteRepository,
		auth_model.AccessTokenScopeWriteUser,
		auth_model.AccessTokenScopeWriteIssue,
	)
	userReadToken := getTokenForLoggedInUser(t,
		session2,
		auth_model.AccessTokenScopeReadProject,
		auth_model.AccessTokenScopeReadOrganization,
		auth_model.AccessTokenScopeReadRepository,
		auth_model.AccessTokenScopeReadUser,
		auth_model.AccessTokenScopeReadIssue,
	)

	projectOpts := &api.CreateOrUpdateProjectOptions{
		Title:        "Test Project",
		Description:  "Test Project",
		TemplateType: "none",
		CardType:     "text_only",
		Status:       "open",
	}

	// Case: Public Org where User2 is owner
	t.Run("Public Org where User2 is owner", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		pubUser2Org := forgery.CreateOrganisation(t, user2, &forgery.CreateOrganisationOptions{
			Visibility: structs.VisibleTypePublic,
		})

		// Run actions
		runOpts := &runOpts{
			token:         userWriteToken,
			owner:         pubUser2Org.Name,
			shouldSucceed: true,
			ownerType:     project_module.APIOwnerTypeOrganization,
		}
		runProjectWriteActions(t, runOpts, projectOpts)
	})

	// Case: Limited Org where User2 is owner
	t.Run("Limited Org where User2 is owner", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		limUser2Org := forgery.CreateOrganisation(t, user2, &forgery.CreateOrganisationOptions{
			Visibility: structs.VisibleTypeLimited,
		})

		// Run actions
		runOpts := &runOpts{
			token:         userWriteToken,
			owner:         limUser2Org.Name,
			shouldSucceed: true,
			ownerType:     project_module.APIOwnerTypeOrganization,
		}
		runProjectWriteActions(t, runOpts, projectOpts)
	})

	// Case: Private Org where User2 is owner
	t.Run("Private Org where User2 is owner", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		privUser2Org := forgery.CreateOrganisation(t, user2, &forgery.CreateOrganisationOptions{
			Visibility: structs.VisibleTypeLimited,
		})

		// Run actions
		runOpts := &runOpts{
			token:         userWriteToken,
			owner:         privUser2Org.Name,
			shouldSucceed: true,
			ownerType:     project_module.APIOwnerTypeOrganization,
		}
		runProjectWriteActions(t, runOpts, projectOpts)
	})

	// Case: Repo where User2 is owner
	t.Run("Repo where User2 is owner", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		user2Repo := forgery.CreateRepository(t, user2, nil)

		// Run actions
		runOpts := &runOpts{
			token:         userWriteToken,
			owner:         user2.Name,
			repo:          user2Repo.Name,
			shouldSucceed: true,
			ownerType:     project_module.APIOwnerTypeRepository,
		}
		runProjectWriteActions(t, runOpts, projectOpts)
	})

	// Case: Project where User2 is owner
	t.Run("Project where User2 is owner", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		// Run actions
		runOpts := &runOpts{
			token:         userWriteToken,
			owner:         user2.Name,
			shouldSucceed: true,
			ownerType:     project_module.APIOwnerTypeIndividual,
		}
		runProjectWriteActions(t, runOpts, projectOpts)
	})

	// public user1 org, with user2 write access
	pubUser1Org := forgery.CreateOrganisation(t, user1, &forgery.CreateOrganisationOptions{
		Visibility: structs.VisibleTypePublic,
	})

	pubOrgTeamOpts := &forgery.CreateTeamOptions{
		Name: "CanWriteProjects",
		Mode: perm.AccessModeWrite,
		Units: []*organization.TeamUnit{
			{
				OrgID:      pubUser1Org.ID,
				Type:       unit.TypeProjects,
				AccessMode: perm.AccessModeWrite,
			},
		},
	}
	pubUser1OrgTeam := forgery.CreateTeam(t, pubUser1Org, pubOrgTeamOpts)

	_ = addOrRemoveTeamUser(t, adminWriteToken, user2.Name, "PUT", pubUser1OrgTeam.ID)

	// Case: Public Org where User2 team member with write access
	t.Run("Public Org where User2 team member with write access", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		runOpts := &runOpts{
			token:         adminWriteToken,
			owner:         pubUser1Org.Name,
			shouldSucceed: true,
			ownerType:     project_module.APIOwnerTypeOrganization,
		}
		runProjectWriteActions(t, runOpts, projectOpts)
	})

	// limited user1 org with user2 write access
	limUser1Org := forgery.CreateOrganisation(t, user1, &forgery.CreateOrganisationOptions{
		Visibility: structs.VisibleTypeLimited,
	})

	limOrgTeamOpts := &forgery.CreateTeamOptions{
		Name: "CanWriteProjects",
		Mode: perm.AccessModeWrite,
		Units: []*organization.TeamUnit{
			{
				OrgID:      limUser1Org.ID,
				Type:       unit.TypeProjects,
				AccessMode: perm.AccessModeWrite,
			},
		},
	}
	limUser1OrgTeam := forgery.CreateTeam(t, limUser1Org, limOrgTeamOpts)

	_ = addOrRemoveTeamUser(t, adminWriteToken, user2.Name, "PUT", limUser1OrgTeam.ID)

	// Case: Limited Org where User2 team member with write access
	t.Run("Limited Org where User2 team member with write access", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		runOpts := &runOpts{
			token:         adminWriteToken,
			owner:         limUser1Org.Name,
			shouldSucceed: true,
			ownerType:     project_module.APIOwnerTypeOrganization,
		}
		runProjectWriteActions(t, runOpts, projectOpts)
	})

	// private user1 org with user2 write access
	privUser1Org := forgery.CreateOrganisation(t, user1, &forgery.CreateOrganisationOptions{
		Visibility: structs.VisibleTypePrivate,
	})

	privOrgTeamOpts := &forgery.CreateTeamOptions{
		Name: "CanWriteProjects",
		Mode: perm.AccessModeWrite,
		Units: []*organization.TeamUnit{
			{
				OrgID:      privUser1Org.ID,
				Type:       unit.TypeProjects,
				AccessMode: perm.AccessModeWrite,
			},
		},
	}
	privUser1OrgTeam := forgery.CreateTeam(t, privUser1Org, privOrgTeamOpts)

	_ = addOrRemoveTeamUser(t, adminWriteToken, user2.Name, "PUT", privUser1OrgTeam.ID)

	// Case: Private Org where User2 team member with write access
	t.Run("Private Org where User2 team member with write access", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		runOpts := &runOpts{
			token:         adminWriteToken,
			owner:         privUser1Org.Name,
			shouldSucceed: true,
			ownerType:     project_module.APIOwnerTypeOrganization,
		}
		runProjectWriteActions(t, runOpts, projectOpts)
	})

	repoOpts := &api.CreateRepoOption{
		Name: "user1Repo",
	}
	writePerm := "write"
	collabOpts := &api.AddCollaboratorOption{
		Permission: &writePerm,
	}

	// Case: Repo where User2 is not owner, collaborator with write/read access
	t.Run("Repo where User2 is not owner, collaborator with write/read access", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		user1Repo := forgery.CreateRepository(t, user1, nil)

		addorRemoveCollaboratorToRepo(t, adminWriteToken, user1.Name, repoOpts.Name, user2.Name, "PUT", collabOpts)

		// Run actions
		runOpts := &runOpts{
			token:         adminWriteToken,
			owner:         user1.Name,
			repo:          user1Repo.Name,
			shouldSucceed: true,
			ownerType:     project_module.APIOwnerTypeRepository,
		}
		runProjectWriteActions(t, runOpts, projectOpts)
	})

	// Case: Repo where User2 is not owner
	t.Run("Repo where User2 is not owner", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		addorRemoveCollaboratorToRepo(t, adminWriteToken, user1.Name, repoOpts.Name, user2.Name, "DELETE", collabOpts)
		runOpts := &runOpts{
			token:         userWriteToken,
			owner:         user1.Name,
			repo:          repoOpts.Name,
			shouldSucceed: false,
			ownerType:     project_module.APIOwnerTypeRepository,
		}
		runProjectWriteActions(t, runOpts, projectOpts)
	})

	// Case: Repo where User2 is collaborator with read access
	t.Run("Repo where User2 is collaborator with read access", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		readPerm := "read"
		collabOpts.Permission = &readPerm
		addorRemoveCollaboratorToRepo(t, adminWriteToken, user1.Name, repoOpts.Name, user2.Name, "PUT", collabOpts)
		runOpts := &runOpts{
			token:         userWriteToken,
			owner:         user1.Name,
			repo:          repoOpts.Name,
			shouldSucceed: false,
			ownerType:     project_module.APIOwnerTypeRepository,
		}
		runProjectWriteActions(t, runOpts, projectOpts)
	})

	// Case: Public Org where User2 is not member
	t.Run("Public Org where User2 is not member", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		_ = addOrRemoveTeamUser(t, adminWriteToken, user2.Name, "DELETE", pubUser1OrgTeam.ID)
		runOpts := &runOpts{
			token:         userWriteToken,
			owner:         pubUser1Org.Name,
			shouldSucceed: false,
			ownerType:     project_module.APIOwnerTypeOrganization,
		}
		runProjectWriteActions(t, runOpts, projectOpts)
	})

	// Case: Limited Org where User2 is not member
	t.Run("Limited Org where User2 is not member", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		_ = addOrRemoveTeamUser(t, adminWriteToken, user2.Name, "DELETE", limUser1OrgTeam.ID)
		runOpts := &runOpts{
			token:         userWriteToken,
			owner:         limUser1Org.Name,
			shouldSucceed: false,
			ownerType:     project_module.APIOwnerTypeOrganization,
		}
		runProjectWriteActions(t, runOpts, projectOpts)
		runOpts.token = userReadToken
		runProjectReadActions(t, runOpts)
	})

	// Case: Private Org where User2 is not member
	t.Run("Private Org where User2 is not member", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		_ = addOrRemoveTeamUser(t, adminWriteToken, user2.Name, "DELETE", privUser1OrgTeam.ID)
		runOpts := &runOpts{
			token:         userWriteToken,
			owner:         privUser1Org.Name,
			shouldSucceed: false,
			ownerType:     project_module.APIOwnerTypeOrganization,
		}
		runProjectWriteActions(t, runOpts, projectOpts)
		runOpts.token = userReadToken
		runProjectReadActions(t, runOpts)
	})

	// Case: Project where User2 is not owner - e.g. try deleting other peoples project
	t.Run("Project where User2 is not owner", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		var delProj *api.Project
		runOpts := &runOpts{
			token:         userReadToken,
			owner:         privUser1Org.Name,
			shouldSucceed: false,
			ownerType:     project_module.APIOwnerTypeOrganization,
		}
		baseString := getProjectAPIBaseString(runOpts)
		endpoint := baseString + "/projects"
		resp := jsonRequestWithAuth(t, adminWriteToken, "POST", endpoint, http.StatusCreated, projectOpts)
		DecodeJSON(t, resp, &delProj)
		deleteProject(t, userWriteToken, baseString, delProj.ID, http.StatusForbidden)
	})
}
