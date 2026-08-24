// Copyright 2023 The Gitea Authors. All rights reserved.
// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path"
	"strconv"
	"strings"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/perm"
	project_model "forgejo.org/models/project"
	repo_model "forgejo.org/models/repo"
	unit_model "forgejo.org/models/unit"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/optional"
	project_module "forgejo.org/modules/project"
	"forgejo.org/modules/test"
	forms_service "forgejo.org/services/forms"
	"forgejo.org/tests"
	"forgejo.org/tests/forgery"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrivateRepoProject(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	// not logged in user
	req := NewRequest(t, "GET", "/user31/-/projects")
	MakeRequest(t, req, http.StatusNotFound)

	sess := loginUser(t, "user1")
	req = NewRequest(t, "GET", "/user31/-/projects")
	sess.MakeRequest(t, req, http.StatusOK)
}

func TestMoveRepoProjectColumns(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	repo2 := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 2})

	project1 := project_model.Project{
		Title:        "new created project",
		RepoID:       repo2.ID,
		Type:         project_module.TypeRepository,
		TemplateType: project_module.TemplateTypeNone,
	}
	err := project_model.CreateProject(db.DefaultContext, &project1)
	require.NoError(t, err)

	for i := range 3 {
		err = project_model.CreateColumn(db.DefaultContext, &project_model.Column{
			Title:     fmt.Sprintf("column %d", i+1),
			ProjectID: project1.ID,
		})
		require.NoError(t, err)
	}

	columns, total, err := project_model.GetColumns(db.DefaultContext, project1.ID, db.ListOptionsAll)
	require.NoError(t, err)
	assert.Len(t, columns, 3)
	assert.EqualValues(t, 0, columns[0].Sorting)
	assert.EqualValues(t, 1, columns[1].Sorting)
	assert.EqualValues(t, 2, columns[2].Sorting)
	assert.Equal(t, int64(3), total)

	sess := loginUser(t, "user1")
	req := NewRequestWithJSON(t, "POST", fmt.Sprintf("/%s/projects/%d/move", repo2.FullName(), project1.ID), map[string]any{
		"columns": []map[string]any{
			{"columnID": columns[1].ID, "sorting": 0},
			{"columnID": columns[2].ID, "sorting": 1},
			{"columnID": columns[0].ID, "sorting": 2},
		},
	})
	sess.MakeRequest(t, req, http.StatusOK)

	columnsAfter, total, err := project_model.GetColumns(db.DefaultContext, project1.ID, db.ListOptionsAll)
	require.NoError(t, err)
	assert.Len(t, columns, 3)
	assert.Equal(t, columns[1].ID, columnsAfter[0].ID)
	assert.Equal(t, columns[2].ID, columnsAfter[1].ID)
	assert.Equal(t, columns[0].ID, columnsAfter[2].ID)
	assert.Equal(t, int64(3), total)

	require.NoError(t, project_model.DeleteProjectByID(db.DefaultContext, project1.ID, optional.Some(project1.RepoID)))
}

func TestChangeStatusProject(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	user5 := loginUser(t, "user5")
	user2 := loginUser(t, "user2")

	t.Run("User", func(t *testing.T) {
		project4CloseURL := "/user2/-/projects/4/close"

		t.Run("Doer is not context user", func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()

			user5.MakeRequest(t, NewRequest(t, "POST", project4CloseURL), http.StatusNotFound)
			unittest.AssertExistsIf(t, true, &project_model.Project{ID: 4}, "is_closed = false")
		})

		t.Run("Wrong ID", func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()

			user5.MakeRequest(t, NewRequest(t, "POST", "/user5/-/projects/4/close"), http.StatusNotFound)
			unittest.AssertExistsIf(t, true, &project_model.Project{ID: 4}, "is_closed = false")

			user5.MakeRequest(t, NewRequest(t, "POST", "/user5/-/projects/1/close"), http.StatusNotFound)
			unittest.AssertExistsIf(t, true, &project_model.Project{ID: 1}, "is_closed = false")

			user5.MakeRequest(t, NewRequest(t, "POST", "/user5/-/projects/7/close"), http.StatusNotFound)
			unittest.AssertExistsIf(t, true, &project_model.Project{ID: 7}, "is_closed = false")
		})

		t.Run("Normal", func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()

			user2.MakeRequest(t, NewRequest(t, "POST", project4CloseURL), http.StatusOK)
			unittest.AssertExistsIf(t, true, &project_model.Project{ID: 4}, "is_closed = true")
		})
	})

	t.Run("Organization", func(t *testing.T) {
		project7CloseURL := "/org3/-/projects/7/close"

		t.Run("Doer does not have permission", func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()

			user5.MakeRequest(t, NewRequest(t, "POST", project7CloseURL), http.StatusNotFound)
			unittest.AssertExistsIf(t, true, &project_model.Project{ID: 7}, "is_closed = false")
		})

		t.Run("Normal", func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()

			user2.MakeRequest(t, NewRequest(t, "POST", project7CloseURL), http.StatusOK)
			unittest.AssertExistsIf(t, true, &project_model.Project{ID: 7}, "is_closed = true")
		})
	})

	t.Run("Repository", func(t *testing.T) {
		project1CloseURL := "/user2/repo1/projects/1/close"

		t.Run("Doer does not have permission", func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()

			user5.MakeRequest(t, NewRequest(t, "POST", project1CloseURL), http.StatusNotFound)
			unittest.AssertExistsIf(t, true, &project_model.Project{ID: 1}, "is_closed = false")
		})

		t.Run("Wrong ID", func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()

			user5.MakeRequest(t, NewRequest(t, "POST", "/user5/repo4/projects/1/close"), http.StatusNotFound)
			unittest.AssertExistsIf(t, true, &project_model.Project{ID: 1}, "is_closed = false")
		})

		t.Run("Normal", func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()

			user2.MakeRequest(t, NewRequest(t, "POST", project1CloseURL), http.StatusOK)
			unittest.AssertExistsIf(t, true, &project_model.Project{ID: 1}, "is_closed = true")
		})
	})
}

func TestProjectPermissionsAndConsistency(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	ctx := t.Context()

	newTestIssue := func(t *testing.T, session *TestSession, repo *repo_model.Repository, project *project_model.Project, expectedStatus int) *httptest.ResponseRecorder {
		t.Helper()

		req := NewRequest(t, "GET", path.Join(repo.FullName(), "issues", "new"))
		resp := session.MakeRequest(t, req, http.StatusOK)

		htmlDoc := NewHTMLParser(t, resp.Body)
		link, exists := htmlDoc.doc.Find("#new-issue").Attr("action")
		require.True(t, exists, "The template has changed")

		payload := map[string]string{
			"title":   "Hello",
			"content": "World",
		}
		if project != nil {
			payload["project_id"] = strconv.FormatInt(project.ID, 10)
		}

		req = NewRequestWithValues(t, "POST", link, payload)
		return session.MakeRequest(t, req, expectedStatus)
	}

	newTestIssueSuccess := func(t *testing.T, session *TestSession, repo *repo_model.Repository, project *project_model.Project) *issues_model.Issue {
		t.Helper()

		resp := newTestIssue(t, session, repo, project, http.StatusOK)

		issueURL := test.RedirectURL(resp)

		indexStr := issueURL[strings.LastIndexByte(issueURL, '/')+1:]
		index, err := strconv.Atoi(indexStr)
		require.NoError(t, err, "Invalid issue href: %s", issueURL)

		issue := &issues_model.Issue{RepoID: repo.ID, Index: int64(index)}
		unittest.AssertExistsAndLoadBean(t, issue)

		if project != nil {
			require.NoError(t, issue.LoadProject(ctx))
			require.NotNil(t, issue.Project)
			require.Equal(t, project.ID, issue.Project.ID)
		}

		return issue
	}

	updateIssueProject := func(t *testing.T, session *TestSession, repo *repo_model.Repository, project *project_model.Project, issue *issues_model.Issue, expectedStatus int) {
		t.Helper()

		req := NewRequestWithValues(t, "POST", path.Join(repo.FullName(), "issues", "projects"), map[string]string{
			"issue_ids": strconv.FormatInt(issue.ID, 10),
			"id":        strconv.FormatInt(project.ID, 10),
		})
		session.MakeRequest(t, req, expectedStatus)

		if expectedStatus == http.StatusOK {
			issue := &issues_model.Issue{ID: issue.ID}
			unittest.AssertExistsAndLoadBean(t, issue)
			issue.LoadProject(ctx)
			require.Equal(t, project.ID, issue.Project.ID)
		}
	}

	clearIssueProject := func(t *testing.T, session *TestSession, repo *repo_model.Repository, issue *issues_model.Issue) {
		t.Helper()

		req := NewRequestWithValues(t, "POST", path.Join(repo.FullName(), "issues", "projects"), map[string]string{
			"issue_ids": strconv.FormatInt(issue.ID, 10),
		})
		session.MakeRequest(t, req, http.StatusOK)
	}

	t.Run("New issue with project ID in query string", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		unittest.LoadFixtures()

		getNewIssue := func(t *testing.T, session *TestSession, repo *repo_model.Repository, projectID int64, expectedStatus int) *httptest.ResponseRecorder {
			t.Helper()

			req := NewRequest(t, "GET", fmt.Sprintf("%s?project=%d", path.Join(repo.FullName(), "issues", "new"), projectID))
			return session.MakeRequest(t, req, expectedStatus)
		}

		t.Run("does not exist anywhere", func(t *testing.T) {
			owner := forgery.CreateUser(t, nil)
			doer := owner

			repo := forgery.CreateRepository(t, owner, nil)
			invalidProjectID := int64(4234243)
			session := loginUser(t, doer.Name)
			resp := getNewIssue(t, session, repo, invalidProjectID, http.StatusNotFound)
			assert.Contains(t, resp.Body.String(), "Not found.")
		})

		t.Run("is a valid repository project", func(t *testing.T) {
			owner := forgery.CreateUser(t, nil)
			doer := owner

			repo := forgery.CreateRepository(t, owner, nil)
			project := forgery.CreateProject(t, repo, nil)
			session := loginUser(t, doer.Name)
			getNewIssue(t, session, repo, project.ID, http.StatusOK)
		})

		t.Run("is invalid because it is a repository project that belongs to a different repository", func(t *testing.T) {
			owner := forgery.CreateUser(t, nil)
			doer := owner

			repo := forgery.CreateRepository(t, owner, nil)
			otherRepo := forgery.CreateRepository(t, owner, nil)
			projectFromOtherRepo := forgery.CreateProject(t, otherRepo, nil)
			session := loginUser(t, doer.Name)
			resp := getNewIssue(t, session, repo, projectFromOtherRepo.ID, http.StatusNotFound)
			assert.Contains(t, resp.Body.String(), "Not found.")
		})

		t.Run("is a valid owner project", func(t *testing.T) {
			user := forgery.CreateUser(t, nil)
			repo := forgery.CreateRepository(t, user, nil)
			doer := user

			project := forgery.CreateProject(t, user, nil)
			forgery.DisableRepoUnits(t, repo, unit_model.TypeProjects)
			session := loginUser(t, doer.Name)
			getNewIssue(t, session, repo, project.ID, http.StatusOK)
		})

		t.Run("is invalid because it is an owner project that belongs to a different owner", func(t *testing.T) {
			user := forgery.CreateUser(t, nil)
			repo := forgery.CreateRepository(t, user, nil)
			doer := user

			otherUser := forgery.CreateUser(t, nil)
			projectFromOtherUser := forgery.CreateProject(t, otherUser, nil)
			session := loginUser(t, doer.Name)
			resp := getNewIssue(t, session, repo, projectFromOtherUser.ID, http.StatusNotFound)
			assert.Contains(t, resp.Body.String(), "Not found.")
		})
	})

	t.Run("Project ID", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		unittest.LoadFixtures()

		t.Run("does not exist anywhere", func(t *testing.T) {
			owner := forgery.CreateUser(t, nil)
			doer := owner

			repo := forgery.CreateRepository(t, owner, nil)
			invalidProject := &project_model.Project{ID: 4234243}
			session := loginUser(t, doer.Name)
			newTestIssue(t, session, repo, invalidProject, http.StatusNotFound)
			issue := newTestIssueSuccess(t, session, repo, nil)

			updateIssueProject(t, session, repo, invalidProject, issue, http.StatusNotFound)
		})

		t.Run("is a valid repository project", func(t *testing.T) {
			owner := forgery.CreateUser(t, nil)
			doer := owner

			repo := forgery.CreateRepository(t, owner, nil)
			projectA := forgery.CreateProject(t, repo, nil)
			session := loginUser(t, doer.Name)
			issue := newTestIssueSuccess(t, session, repo, projectA)

			projectB := forgery.CreateProject(t, repo, nil)
			updateIssueProject(t, session, repo, projectB, issue, http.StatusOK)
			clearIssueProject(t, session, repo, issue)
		})

		t.Run("is invalid because it is a repository project that belongs to a different repository", func(t *testing.T) {
			owner := forgery.CreateUser(t, nil)
			doer := owner

			repo := forgery.CreateRepository(t, owner, nil)
			otherRepo := forgery.CreateRepository(t, owner, nil)
			projectFromOtherRepo := forgery.CreateProject(t, otherRepo, nil)
			session := loginUser(t, doer.Name)
			newTestIssue(t, session, repo, projectFromOtherRepo, http.StatusNotFound)
			issue := newTestIssueSuccess(t, session, repo, nil)

			updateIssueProject(t, session, repo, projectFromOtherRepo, issue, http.StatusNotFound)
		})

		t.Run("is a valid owner project", func(t *testing.T) {
			user := forgery.CreateUser(t, nil)
			repo := forgery.CreateRepository(t, user, nil)
			doer := user

			projectA := forgery.CreateProject(t, user, nil)
			forgery.DisableRepoUnits(t, repo, unit_model.TypeProjects)
			session := loginUser(t, doer.Name)
			issue := newTestIssueSuccess(t, session, repo, projectA)

			projectB := forgery.CreateProject(t, user, nil)
			updateIssueProject(t, session, repo, projectB, issue, http.StatusOK)
			clearIssueProject(t, session, repo, issue)
		})

		t.Run("is invalid because it is an owner project that belongs to a different owner", func(t *testing.T) {
			user := forgery.CreateUser(t, nil)
			repo := forgery.CreateRepository(t, user, nil)
			doer := user

			otherUser := forgery.CreateUser(t, nil)
			projectFromOtherUser := forgery.CreateProject(t, otherUser, nil)
			session := loginUser(t, doer.Name)
			newTestIssue(t, session, repo, projectFromOtherUser, http.StatusNotFound)
			issue := newTestIssueSuccess(t, session, repo, nil)

			updateIssueProject(t, session, repo, projectFromOtherUser, issue, http.StatusNotFound)
		})
	})

	t.Run("Repository project", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		unittest.LoadFixtures()

		t.Run("doer is owner", func(t *testing.T) {
			owner := forgery.CreateUser(t, nil)
			doer := owner

			repo := forgery.CreateRepository(t, owner, nil)
			projectA := forgery.CreateProject(t, repo, nil)
			session := loginUser(t, doer.Name)
			issue := newTestIssueSuccess(t, session, repo, projectA)

			projectB := forgery.CreateProject(t, repo, nil)
			updateIssueProject(t, session, repo, projectB, issue, http.StatusOK)
			clearIssueProject(t, session, repo, issue)
		})

		t.Run("doer is owner but the projects unit is disabled", func(t *testing.T) {
			owner := forgery.CreateUser(t, nil)
			doer := owner

			repo := forgery.CreateRepository(t, owner, nil)
			project := forgery.CreateProject(t, repo, nil)
			forgery.DisableRepoUnits(t, repo, unit_model.TypeProjects)

			session := loginUser(t, doer.Name)
			newTestIssue(t, session, repo, project, http.StatusNotFound)
			issue := newTestIssueSuccess(t, session, repo, nil)

			updateIssueProject(t, session, repo, project, issue, http.StatusNotFound)
		})

		t.Run("doer is collaborator with write permissions", func(t *testing.T) {
			doer := forgery.CreateUser(t, nil)
			user := forgery.CreateUser(t, nil)
			repo := forgery.CreateRepository(t, user, &forgery.CreateRepositoryOptions{
				Collaborators: map[*user_model.User]perm.AccessMode{doer: perm.AccessModeWrite},
			})

			projectA := forgery.CreateProject(t, repo, nil)
			session := loginUser(t, doer.Name)
			issue := newTestIssueSuccess(t, session, repo, projectA)

			projectB := forgery.CreateProject(t, repo, nil)
			updateIssueProject(t, session, repo, projectB, issue, http.StatusOK)
			clearIssueProject(t, session, repo, issue)
		})

		t.Run("doer is collaborator with read permissions", func(t *testing.T) {
			doer := forgery.CreateUser(t, nil)
			user := forgery.CreateUser(t, nil)
			repo := forgery.CreateRepository(t, user, &forgery.CreateRepositoryOptions{
				Collaborators: map[*user_model.User]perm.AccessMode{doer: perm.AccessModeRead},
			})

			project := forgery.CreateProject(t, repo, nil)
			session := loginUser(t, doer.Name)
			newTestIssue(t, session, repo, project, http.StatusForbidden)
			issue := newTestIssueSuccess(t, session, repo, nil)

			updateIssueProject(t, session, repo, project, issue, http.StatusNotFound)
		})
	})

	t.Run("Organization project", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		unittest.LoadFixtures()

		t.Run("doer is the organization owner", func(t *testing.T) {
			owner := forgery.CreateUser(t, nil)
			doer := owner
			org := forgery.CreateOrganisation(t, owner)

			repo := forgery.CreateRepository(t, org.AsUser(), nil)
			projectA := forgery.CreateProject(t, org, nil)
			forgery.DisableRepoUnits(t, repo, unit_model.TypeProjects)
			session := loginUser(t, doer.Name)
			issue := newTestIssueSuccess(t, session, repo, projectA)

			projectB := forgery.CreateProject(t, org, nil)
			updateIssueProject(t, session, repo, projectB, issue, http.StatusOK)
			clearIssueProject(t, session, repo, issue)
		})

		t.Run("doer in team with write permissions", func(t *testing.T) {
			doer := forgery.CreateUser(t, nil)
			owner := forgery.CreateUser(t, nil)
			org := forgery.CreateOrganisation(t, owner)
			forgery.CreateTeam(t, org, &forgery.CreateTeamOptions{
				Mode:    perm.AccessModeWrite,
				Members: []*user_model.User{doer},
			})

			repo := forgery.CreateRepository(t, org.AsUser(), nil)
			projectA := forgery.CreateProject(t, org, nil)
			forgery.DisableRepoUnits(t, repo, unit_model.TypeProjects)
			session := loginUser(t, doer.Name)
			issue := newTestIssueSuccess(t, session, repo, projectA)

			projectB := forgery.CreateProject(t, org, nil)
			updateIssueProject(t, session, repo, projectB, issue, http.StatusOK)
			clearIssueProject(t, session, repo, issue)
		})

		t.Run("doer in a team with read permissions", func(t *testing.T) {
			doer := forgery.CreateUser(t, nil)
			org := forgery.CreateOrganisation(t, nil)
			forgery.CreateTeam(t, org, &forgery.CreateTeamOptions{
				Mode:    perm.AccessModeRead,
				Members: []*user_model.User{doer},
			})

			repo := forgery.CreateRepository(t, org.AsUser(), nil)
			project := forgery.CreateProject(t, org, nil)
			forgery.DisableRepoUnits(t, repo, unit_model.TypeProjects)
			session := loginUser(t, doer.Name)
			newTestIssue(t, session, repo, project, http.StatusForbidden)
			issue := newTestIssueSuccess(t, session, repo, nil)

			updateIssueProject(t, session, repo, project, issue, http.StatusNotFound)
		})

		t.Run("doer not in any team", func(t *testing.T) {
			doer := forgery.CreateUser(t, nil)
			org := forgery.CreateOrganisation(t, nil)

			repo := forgery.CreateRepository(t, org.AsUser(), nil)
			project := forgery.CreateProject(t, org, nil)
			forgery.DisableRepoUnits(t, repo, unit_model.TypeProjects)
			session := loginUser(t, doer.Name)
			newTestIssue(t, session, repo, project, http.StatusForbidden)
			issue := newTestIssueSuccess(t, session, repo, nil)

			updateIssueProject(t, session, repo, project, issue, http.StatusNotFound)
		})
	})

	t.Run("User project", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		unittest.LoadFixtures()

		t.Run("doer is owner", func(t *testing.T) {
			user := forgery.CreateUser(t, nil)
			repo := forgery.CreateRepository(t, user, nil)
			doer := user

			projectA := forgery.CreateProject(t, user, nil)
			forgery.DisableRepoUnits(t, repo, unit_model.TypeProjects)
			session := loginUser(t, doer.Name)
			issue := newTestIssueSuccess(t, session, repo, projectA)

			projectB := forgery.CreateProject(t, user, nil)
			updateIssueProject(t, session, repo, projectB, issue, http.StatusOK)
			clearIssueProject(t, session, repo, issue)
		})

		t.Run("doer is collaborator with write permissions", func(t *testing.T) {
			doer := forgery.CreateUser(t, nil)
			user := forgery.CreateUser(t, nil)
			repo := forgery.CreateRepository(t, user, &forgery.CreateRepositoryOptions{
				Collaborators: map[*user_model.User]perm.AccessMode{doer: perm.AccessModeWrite},
			})

			projectA := forgery.CreateProject(t, user, nil)
			forgery.DisableRepoUnits(t, repo, unit_model.TypeProjects)
			session := loginUser(t, doer.Name)
			issue := newTestIssueSuccess(t, session, repo, projectA)

			projectB := forgery.CreateProject(t, user, nil)
			updateIssueProject(t, session, repo, projectB, issue, http.StatusOK)
			clearIssueProject(t, session, repo, issue)
		})

		t.Run("doer is collaborator with read permissions", func(t *testing.T) {
			doer := forgery.CreateUser(t, nil)
			user := forgery.CreateUser(t, nil)
			repo := forgery.CreateRepository(t, user, &forgery.CreateRepositoryOptions{
				Collaborators: map[*user_model.User]perm.AccessMode{doer: perm.AccessModeRead},
			})

			project := forgery.CreateProject(t, user, nil)
			forgery.DisableRepoUnits(t, repo, unit_model.TypeProjects)
			session := loginUser(t, doer.Name)
			newTestIssue(t, session, repo, project, http.StatusForbidden)
			issue := newTestIssueSuccess(t, session, repo, nil)

			updateIssueProject(t, session, repo, project, issue, http.StatusNotFound)
		})

		t.Run("doer is not a collaborator or owner", func(t *testing.T) {
			doer := forgery.CreateUser(t, nil)
			user := forgery.CreateUser(t, nil)
			repo := forgery.CreateRepository(t, user, nil)

			project := forgery.CreateProject(t, user, nil)
			forgery.DisableRepoUnits(t, repo, unit_model.TypeProjects)
			session := loginUser(t, doer.Name)
			newTestIssue(t, session, repo, project, http.StatusForbidden)
			issue := newTestIssueSuccess(t, session, repo, nil)

			updateIssueProject(t, session, repo, project, issue, http.StatusNotFound)
		})
	})
}

func TestProjectAPIProjects(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	// template: templates/projects/list.tmpl
	user2 := loginUser(t, "user2")
	t.Run("User", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		projectsURL := "/user2/-/projects"

		// no closed project
		t.Run("get open", func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			resp := user2.MakeRequest(t, NewRequest(t, "GET", projectsURL), http.StatusOK)
			doc := NewHTMLParser(t, resp.Body)
			projectList := doc.Find(".milestone-list li")
			assert.Equal(t, 3, projectList.Length())
		})

		t.Run("get closed", func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			resp := user2.MakeRequest(t, NewRequest(t, "GET", projectsURL+"?state=closed"), http.StatusOK)
			doc := NewHTMLParser(t, resp.Body)
			projectList := doc.Find(".milestone-list li")
			assert.Equal(t, 0, projectList.Length())
		})

		// one closed project
		user2.MakeRequest(t, NewRequest(t, "POST", projectsURL+"/4/close"), http.StatusOK)
		t.Run("get open, one closed", func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			resp := user2.MakeRequest(t, NewRequest(t, "GET", projectsURL), http.StatusOK)
			doc := NewHTMLParser(t, resp.Body)
			projectList := doc.Find(".milestone-list li")
			assert.Equal(t, 2, projectList.Length())
		})

		t.Run("get closed, one close", func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			resp := user2.MakeRequest(t, NewRequest(t, "GET", projectsURL+"?state=closed"), http.StatusOK)
			doc := NewHTMLParser(t, resp.Body)
			projectList := doc.Find(".milestone-list li")
			assert.Equal(t, 1, projectList.Length())
		})
	})

	t.Run("Organization", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		projectsURL := "/org3/-/projects"

		t.Run("get open", func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			resp := user2.MakeRequest(t, NewRequest(t, "GET", projectsURL), http.StatusOK)
			doc := NewHTMLParser(t, resp.Body)
			projectList := doc.Find(".milestone-list li")
			assert.Equal(t, 1, projectList.Length())
		})

		t.Run("get closed", func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			resp := user2.MakeRequest(t, NewRequest(t, "GET", projectsURL+"?state=closed"), http.StatusOK)
			doc := NewHTMLParser(t, resp.Body)
			projectList := doc.Find(".milestone-list li")
			assert.Equal(t, 0, projectList.Length())
		})
	})

	t.Run("Repository", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		projectsURL := "/user2/repo1/projects"

		t.Run("get open", func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			resp := user2.MakeRequest(t, NewRequest(t, "GET", projectsURL), http.StatusOK)
			doc := NewHTMLParser(t, resp.Body)
			projectList := doc.Find(".milestone-list li")
			assert.Equal(t, 1, projectList.Length())
		})
		t.Run("get closed", func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			resp := user2.MakeRequest(t, NewRequest(t, "GET", projectsURL+"?state=closed"), http.StatusOK)
			doc := NewHTMLParser(t, resp.Body)
			projectList := doc.Find(".milestone-list li")
			assert.Equal(t, 0, projectList.Length())
		})
	})
}

func TestProjectAPIRenderNewProject(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	// template: templates/projects/new.tmpl
	user2 := loginUser(t, "user2")
	for testName, projectURL := range map[string]string{
		"User":         "/user2/-/projects/new",
		"Organization": "/org3/-/projects/new",
		"Repository":   "/user2/repo1/projects/new",
	} {
		t.Run(testName, func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			resp := user2.MakeRequest(t, NewRequest(t, "GET", projectURL), http.StatusOK)
			doc := NewHTMLParser(t, resp.Body)
			for _, cc := range project_module.GetAPICardConfig() {
				doc.AssertElement(t, fmt.Sprintf(".item[data-id='%s']", cc.CardType), true)
			}
		})
	}
}

func TestProjectAPICreateProject(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	user2 := loginUser(t, "user2")
	for testName, projectURL := range map[string]string{
		"User":         "/user2/-/projects/new",
		"Organization": "/org3/-/projects/new",
		"Repository":   "/user2/repo1/projects/new",
	} {
		t.Run(testName, func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			projectOpts := forms_service.CreateProjectForm{
				Title:        "Project 1",
				Content:      "Test",
				TemplateType: project_module.APITemplateTypeNone.String(),
				CardType:     project_module.APICardTypeTextOnly.String(),
			}
			user2.MakeRequest(t, NewRequestWithJSON(t, "POST", projectURL, &projectOpts), http.StatusSeeOther)
		})
	}
}

// Test creation/getting/updating/deleting project for user
func TestProjectAPICRUD(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	// User and auth
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	session := loginUser(t, user2.Name)

	// Create, Get project for an owner
	projectOpts := forms_service.CreateProjectForm{
		Title:        "Project 1",
		Content:      "Test",
		TemplateType: project_module.APITemplateTypeNone.String(),
		CardType:     project_module.APICardTypeTextOnly.String(),
	}

	newProjectEndpoint := fmt.Sprintf("/%v/-/projects/new", user2.Name)
	resp := sessionJSONPOST(t, session, newProjectEndpoint, projectOpts)
	assert.Equal(t, http.StatusSeeOther, resp.Code)

	project := unittest.AssertExistsAndLoadBean(t, &project_model.Project{ID: 4})

	getProjectEndpoint := fmt.Sprintf("/%v/-/projects/%d", user2.Name, project.ID)
	resp = sessionGET(t, session, getProjectEndpoint)
	assert.Equal(t, http.StatusOK, resp.Code)

	// Create columns in a project
	createPCOpt1 := forms_service.EditProjectColumnForm{
		Title:   "Col1",
		Sorting: 0,
		Color:   "#ab1099",
	}

	newProjectColEndpoint := fmt.Sprintf("/%v/-/projects/%d", user2.Name, project.ID)
	resp = sessionJSONPOST(t, session, newProjectColEndpoint, createPCOpt1)
	assert.Equal(t, http.StatusOK, resp.Code)

	// Create, Get project for an owner
	editProjectOpts := forms_service.CreateProjectForm{
		Title:    "Project 1",
		Content:  "Test",
		CardType: project_module.APICardTypeTextOnly.String(),
	}

	editProjectEndpoint := fmt.Sprintf("/%v/-/projects/%d/edit", user2.Name, project.ID)
	resp = sessionJSONPOST(t, session, editProjectEndpoint, editProjectOpts)
	assert.Equal(t, http.StatusSeeOther, resp.Code)

	// Remove project
	deleteProjectEndpoint := fmt.Sprintf("/%v/-/projects/%d/delete", user2.Name, project.ID)
	resp = sessionPOST(t, session, deleteProjectEndpoint)
	assert.Equal(t, http.StatusOK, resp.Code)

	unittest.AssertNotExistsBean(t, &project_model.Project{
		ID: project.ID,
	})
}

func sessionJSONPOST(t *testing.T, session *TestSession, endpoint string, opts any) *httptest.ResponseRecorder {
	req := NewRequestWithJSON(t, "POST", endpoint, &opts)
	return session.MakeRequest(t, req, -1)
}

func sessionPOST(t *testing.T, session *TestSession, endpoint string) *httptest.ResponseRecorder {
	req := NewRequest(t, "POST", endpoint)
	return session.MakeRequest(t, req, -1)
}

func sessionGET(t *testing.T, session *TestSession, endpoint string) *httptest.ResponseRecorder {
	req := NewRequest(t, "GET", endpoint)
	return session.MakeRequest(t, req, -1)
}
