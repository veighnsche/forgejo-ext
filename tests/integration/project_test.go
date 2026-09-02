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
	"forgejo.org/modules/setting"
	project_structs "forgejo.org/modules/structs"
	"forgejo.org/modules/test"
	"forgejo.org/modules/translation"
	forms_service "forgejo.org/services/forms"
	project_service "forgejo.org/services/project"
	"forgejo.org/tests"
	"forgejo.org/tests/forgery"

	"github.com/PuerkitoBio/goquery"
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

func TestProjectWebProjects(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	testProjectList := func(t *testing.T, name string,
		session *TestSession, url, expectElement string, expectLinks []string,
	) {
		// get list of projects from url,
		// check if expectElement exists in reply,
		// check number of projects in list,
		// check links of projects in list
		t.Run(name, func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			resp := session.MakeRequest(t, NewRequest(t, "GET", url), http.StatusOK)
			doc := NewHTMLParser(t, resp.Body)
			doc.AssertElement(t, expectElement, true)

			// template: templates/projects/list.tmpl
			// template lines:
			// <div class="milestone-list">
			//	{{range .Projects}}
			//		<li class="milestone-card">
			// 			<div class="milestone-header">
			//				<h3>
			// [...]
			//					<a class="muted tw-break-anywhere" href="{{.Link ctx}}">{{.Title}}</a>
			//				</h3>
			//			</div>
			// [...]
			//		</li>
			//	{{end}}
			// [...]
			// </div>
			projectList := doc.Find(".milestone-list li.milestone-card")
			assert.Equal(t, len(expectLinks), projectList.Length())
			for _, link := range expectLinks {
				doc.AssertElement(t,
					fmt.Sprintf(".milestone-list li.milestone-card .milestone-header a[href='%s']",
						link),
					true,
				)
			}
		})
	}
	t.Run("User", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		unittest.LoadFixtures()
		ctx := t.Context()

		// create test projects
		user := forgery.CreateUser(t, nil)
		projectA := forgery.CreateProject(t, user, nil)
		projectB := forgery.CreateProject(t, user, nil)
		projectC := forgery.CreateProject(t, user, nil)

		session := loginUser(t, user.Name)

		projectsURL := fmt.Sprintf("/%s/-/projects", user.Name)

		// template: templates/org/projects/list.tmpl
		// template lines:
		// {{if .ContextUser.IsOrganization}}
		// [...]
		// {{else}}
		// 	<div role="main" aria-label="{{.Title}}" class="page-content user profile">
		// [...]
		// 	</div>
		// {{end}}
		expectElement := ".page-content.user.profile"

		// no closed project
		expectOpen := []string{projectA.Link(ctx), projectB.Link(ctx), projectC.Link(ctx)}
		expectClosed := []string{}

		testProjectList(t, "get open",
			session, projectsURL, expectElement, expectOpen)
		testProjectList(t, "get closed",
			session, projectsURL+"?state=closed", expectElement, expectClosed)

		// one closed project
		closeURL := fmt.Sprintf("%s/%d/close", projectsURL, projectA.ID)
		session.MakeRequest(t, NewRequest(t, "POST", closeURL), http.StatusOK)

		expectOpen = []string{projectB.Link(ctx), projectC.Link(ctx)}
		expectClosed = []string{projectA.Link(ctx)}

		testProjectList(t, "get open, one closed",
			session, projectsURL, expectElement, expectOpen)
		testProjectList(t, "get closed, one close",
			session, projectsURL+"?state=closed", expectElement, expectClosed)
	})

	t.Run("Organization", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		unittest.LoadFixtures()
		ctx := t.Context()

		// create test projects
		user := forgery.CreateUser(t, nil)
		org := forgery.CreateOrganisation(t, user)
		projectA := forgery.CreateProject(t, org, nil)
		projectB := forgery.CreateProject(t, org, nil)
		projectC := forgery.CreateProject(t, org, nil)

		session := loginUser(t, user.Name)

		projectsURL := fmt.Sprintf("/%s/-/projects", org.Name)

		// close one project
		closeURL := fmt.Sprintf("%s/%d/close", projectsURL, projectA.ID)
		session.MakeRequest(t, NewRequest(t, "POST", closeURL), http.StatusOK)

		// template: templates/org/projects/list.tmpl
		// {{if .ContextUser.IsOrganization}}
		// 	<div role="main" aria-label="{{.Title}}" class="page-content organization projects">
		// [...]
		// 	</div>
		// {{else}}
		// [...]
		// {{end}}
		expectElement := ".page-content.organization.projects"
		expectOpen := []string{projectB.Link(ctx), projectC.Link(ctx)}
		expectClosed := []string{projectA.Link(ctx)}

		testProjectList(t, "get open",
			session, projectsURL, expectElement, expectOpen)
		testProjectList(t, "get closed",
			session, projectsURL+"?state=closed", expectElement, expectClosed)
	})

	t.Run("Repository", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		unittest.LoadFixtures()
		ctx := t.Context()

		// create test projects
		user := forgery.CreateUser(t, nil)
		repo := forgery.CreateRepository(t, user, nil)
		projectA := forgery.CreateProject(t, repo, nil)
		projectB := forgery.CreateProject(t, repo, nil)
		projectC := forgery.CreateProject(t, repo, nil)

		session := loginUser(t, user.Name)

		projectsURL := fmt.Sprintf("/%s/%s/projects", user.Name, repo.Name)

		// close one project
		closeURL := fmt.Sprintf("%s/%d/close", projectsURL, projectA.ID)
		session.MakeRequest(t, NewRequest(t, "POST", closeURL), http.StatusOK)

		// template: templates/repo/projects/list.tmpl
		// template lines:
		// <div role="main" aria-label="{{.Title}}" class="page-content repository projects milestones">
		// [...]
		// </div>
		expectElement := ".page-content.repository.projects.milestones"
		expectOpen := []string{projectB.Link(ctx), projectC.Link(ctx)}
		expectClosed := []string{projectA.Link(ctx)}

		testProjectList(t, "get open",
			session, projectsURL, expectElement, expectOpen)
		testProjectList(t, "get closed",
			session, projectsURL+"?state=closed", expectElement, expectClosed)
	})
}

func TestProjectWebRenderNewProject(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	unittest.LoadFixtures()

	// create test user, organization, repository
	user := forgery.CreateUser(t, nil)
	org := forgery.CreateOrganisation(t, user)
	repo := forgery.CreateRepository(t, user, nil)

	for _, tt := range []struct {
		name   string
		owner  string
		repo   string
		expect string
	}{
		{
			"User", user.Name, "-",
			".page-content.organization.projects.edit-project.new",
		},
		{
			"Organization", org.Name, "-",
			".page-content.organization.projects.edit-project.new",
		},
		{
			"Repository", user.Name, repo.Name,
			".page-content.repository.projects.edit-project.new.milestone",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			session := loginUser(t, user.Name)
			url := fmt.Sprintf("/%s/%s/projects/new", tt.owner, tt.repo)
			resp := session.MakeRequest(t, NewRequest(t, "GET", url), http.StatusOK)
			doc := NewHTMLParser(t, resp.Body)

			// template: templates/org/projects/new.tmpl
			// template lines:
			// <div role="main" aria-label="{{.Title}}" class="page-content organization projects edit-project new">
			// template: templates/repo/projects/new.tmpl
			// template lines:
			// <div role="main" aria-label="{{.Title}}" class="page-content repository projects edit-project new milestone">
			doc.AssertElement(t, tt.expect, true)

			// template: templates/projects/new.tmpl
			// template lines:
			// <h2 class="ui dividing header">
			// 	{{if .PageIsEditProjects}}
			// [...]
			// 	{{else}}
			// 		{{ctx.Locale.Tr "repo.projects.new"}}
			// 		<div class="sub header">{{ctx.Locale.Tr "repo.projects.new_subheader"}}</div>
			// 	{{end}}
			// </h2>
			assert.Contains(t, doc.Find(".ui.dividing.header").Text(), translation.NewLocale("en-US").Tr("repo.projects.new"))
			assert.Contains(t, doc.Find(".ui.dividing.header .sub.header").Text(), translation.NewLocale("en-US").Tr("repo.projects.new_subheader"))

			// template: templates/projects/new.tmpl
			// template lines:
			// {{range $element := .CardTypes}}
			// 	<div class="item" data-id="{{$element.CardType}}" data-value="{{$element.CardType}}">{{ctx.Locale.Tr $element.Translation}}</div>
			// {{end}}
			for _, cc := range project_module.GetAPICardConfig() {
				doc.AssertElement(t, fmt.Sprintf(".item[data-id='%s']", cc.CardType), true)
			}
		})
	}
}

func TestProjectWebCreateProject(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	unittest.LoadFixtures()

	// create test user, organization, repository
	user := forgery.CreateUser(t, nil)
	org := forgery.CreateOrganisation(t, user)
	repo := forgery.CreateRepository(t, user, nil)

	for _, tt := range []struct {
		name  string
		owner string
		repo  string
	}{
		{"User", user.Name, "-"},
		{"Organization", org.Name, "-"},
		{"Repository", user.Name, repo.Name},
	} {
		t.Run(tt.name, func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			session := loginUser(t, user.Name)
			url := fmt.Sprintf("/%s/%s/projects", tt.owner, tt.repo)

			// create project
			projectOpts := forms_service.CreateProjectForm{
				Title:        "TestProjectWebCreateProject Project 1",
				Content:      "TestProjectWebCreateProject Test Content",
				TemplateType: project_module.APITemplateTypeNone.String(),
				CardType:     project_module.APICardTypeTextOnly.String(),
			}
			session.MakeRequest(t, NewRequestWithJSON(t, "POST", url+"/new", &projectOpts), http.StatusSeeOther)

			// check project was created
			resp := session.MakeRequest(t, NewRequest(t, "GET", url), http.StatusOK)
			doc := NewHTMLParser(t, resp.Body)
			// template: templates/projects/list.tmpl
			// template lines:
			// <div class="milestone-list">
			//	{{range .Projects}}
			//		<li class="milestone-card">
			// 			<div class="milestone-header">
			// [...]
			// 					<a class="muted tw-break-anywhere" href="{{.Link ctx}}">{{.Title}}</a>
			// [...]
			//			</div>
			// [...]
			// 			{{if .Description}}
			// 			<div class="content markup">
			//				{{.RenderedContent}}
			// 			</div>
			// 			{{end}}
			//		</li>
			//	{{end}}
			// [...]
			// </div>
			s := doc.Find(".milestone-list li .milestone-header .muted.tw-break-anywhere").
				FilterFunction(func(i int, s *goquery.Selection) bool {
					return s.Text() == projectOpts.Title
				})
			assert.Equal(t, 1, s.Length())
			s = doc.Find(".milestone-list li .content.markup").
				FilterFunction(func(i int, s *goquery.Selection) bool {
					return strings.Contains(s.Text(), projectOpts.Content)
				})
			assert.Equal(t, 1, s.Length())
		})
	}
}

func TestProjectWebDeleteProject(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	unittest.LoadFixtures()

	// create test user, organization, repository and projects
	user := forgery.CreateUser(t, nil)
	org := forgery.CreateOrganisation(t, user)
	repo := forgery.CreateRepository(t, user, nil)
	userProject := forgery.CreateProject(t, user, nil)
	orgProject := forgery.CreateProject(t, org, nil)
	repoProject := forgery.CreateProject(t, repo, nil)

	// not existing projects
	for _, tt := range []struct {
		name  string
		owner string
		repo  string
	}{
		{"User, not existing project", user.Name, "-"},
		{"Organization, not existing project", org.Name, "-"},
		{"Repository, not existing project", user.Name, repo.Name},
	} {
		t.Run(tt.name, func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			session := loginUser(t, user.Name)
			url := fmt.Sprintf("/%s/%s/projects/1234567890/delete", tt.owner, tt.repo)
			session.MakeRequest(t, NewRequest(t, "POST", url), http.StatusNotFound)
		})
	}

	// wrong owners
	for _, tt := range []struct {
		name      string
		owner     string
		repo      string
		projectID int64
	}{
		{"User, wrong owner", org.Name, "-", userProject.ID},
		{"Organization, wrong owner", user.Name, "-", orgProject.ID},
		{"Repository, wrong owner", user.Name, "-", repoProject.ID},
	} {
		t.Run(tt.name, func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			session := loginUser(t, user.Name)
			url := fmt.Sprintf("/%s/%s/projects/%d/delete", tt.owner, tt.repo, tt.projectID)
			session.MakeRequest(t, NewRequest(t, "POST", url), http.StatusNotFound)
		})
	}

	// no errors
	for _, tt := range []struct {
		name      string
		owner     string
		repo      string
		projectID int64
	}{
		{"User", user.Name, "-", userProject.ID},
		{"Organization", org.Name, "-", orgProject.ID},
		{"Repository", user.Name, repo.Name, repoProject.ID},
	} {
		t.Run(tt.name, func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			session := loginUser(t, user.Name)
			url := fmt.Sprintf("/%s/%s/projects/%d", tt.owner, tt.repo, tt.projectID)

			// check project exists
			session.MakeRequest(t, NewRequest(t, "GET", url), http.StatusOK)

			// delete project
			session.MakeRequest(t, NewRequest(t, "POST", url+"/delete"), http.StatusOK)

			// check project was deleted
			session.MakeRequest(t, NewRequest(t, "GET", url), http.StatusNotFound)
		})
	}
}

func TestProjectWebRenderEditProject(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	unittest.LoadFixtures()

	// create test user, organization, repository and projects
	user := forgery.CreateUser(t, nil)
	org := forgery.CreateOrganisation(t, user)
	repo := forgery.CreateRepository(t, user, nil)
	userProject := forgery.CreateProject(t, user, nil)
	orgProject := forgery.CreateProject(t, org, nil)
	repoProject := forgery.CreateProject(t, repo, nil)

	for _, tt := range []struct {
		name      string
		owner     string
		repo      string
		projectID int64
		expect    string
	}{
		{
			"User", user.Name, "-", userProject.ID,
			".page-content.organization.projects.edit-project.new",
		},
		{
			"Organization", org.Name, "-", orgProject.ID,
			".page-content.organization.projects.edit-project.new",
		},
		{
			"Repository", user.Name, repo.Name, repoProject.ID,
			".page-content.repository.projects.edit-project.new.milestone",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			session := loginUser(t, user.Name)
			url := fmt.Sprintf("/%s/%s/projects/%d/edit", tt.owner, tt.repo, tt.projectID)
			resp := session.MakeRequest(t, NewRequest(t, "GET", url), http.StatusOK)
			doc := NewHTMLParser(t, resp.Body)
			// template: templates/org/projects/new.tmpl
			// template lines:
			// <div role="main" aria-label="{{.Title}}" class="page-content organization projects edit-project new">
			// template: templates/repo/projects/new.tmpl
			// template lines:
			// <div role="main" aria-label="{{.Title}}" class="page-content repository projects edit-project new milestone">
			doc.AssertElement(t, tt.expect, true)

			// template: templates/projects/new.tmpl
			// template lines:
			// <h2 class="ui dividing header">
			// 	{{if .PageIsEditProjects}}
			// 		{{ctx.Locale.Tr "repo.projects.edit"}}
			// 		<div class="sub header">{{ctx.Locale.Tr "repo.projects.edit_subheader"}}</div>
			// 	{{else}}
			// [...]
			// 	{{end}}
			// </h2>
			assert.Contains(t, doc.Find(".ui.dividing.header").Text(), translation.NewLocale("en-US").Tr("repo.projects.edit"))
			assert.Contains(t, doc.Find(".ui.dividing.header .sub.header").Text(), translation.NewLocale("en-US").Tr("repo.projects.edit_subheader"))

			// template: templates/projects/new.tmpl
			// template lines:
			// {{range $element := .CardTypes}}
			// 	<div class="item" data-id="{{$element.CardType}}" data-value="{{$element.CardType}}">{{ctx.Locale.Tr $element.Translation}}</div>
			// {{end}}
			for _, cc := range project_module.GetAPICardConfig() {
				doc.AssertElement(t, fmt.Sprintf(".item[data-id='%s']", cc.CardType), true)
			}
		})
	}

	// wrong owners
	for _, tt := range []struct {
		name      string
		owner     string
		repo      string
		projectID int64
	}{
		{"User, wrong owner", org.Name, "-", userProject.ID},
		{"Organization, wrong owner", user.Name, "-", orgProject.ID},
		{"Repository, wrong owner", user.Name, "-", repoProject.ID},
	} {
		t.Run(tt.name, func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			session := loginUser(t, user.Name)
			url := fmt.Sprintf("/%s/%s/projects/%d/edit", tt.owner, tt.repo, tt.projectID)
			session.MakeRequest(t, NewRequest(t, "GET", url), http.StatusNotFound)
		})
	}
}

func TestProjectWebEditProjectPost(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	unittest.LoadFixtures()

	// create test user, organization, repository and projects
	user := forgery.CreateUser(t, nil)
	org := forgery.CreateOrganisation(t, user)
	repo := forgery.CreateRepository(t, user, nil)
	userProject := forgery.CreateProject(t, user, nil)
	orgProject := forgery.CreateProject(t, org, nil)
	repoProject := forgery.CreateProject(t, repo, nil)

	projectOpts := forms_service.CreateProjectForm{
		Title:    "TestProjectWebEditProjectPost Project 1",
		Content:  "TestProjectWebEditProjectPost Test Text",
		CardType: project_module.APICardTypeTextOnly.String(),
	}

	// no errors
	for _, tt := range []struct {
		name      string
		owner     string
		repo      string
		projectID int64
	}{
		{"User", user.Name, "-", userProject.ID},
		{"Organization", org.Name, "-", orgProject.ID},
		{"Repository", user.Name, repo.Name, repoProject.ID},
	} {
		t.Run(tt.name, func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			session := loginUser(t, user.Name)
			url := fmt.Sprintf("/%s/%s/projects", tt.owner, tt.repo)

			// check project settings do not match already
			project := unittest.AssertExistsAndLoadBean(t, &project_model.Project{ID: tt.projectID})
			assert.NotEqual(t, projectOpts.Title, project.Title)
			assert.NotEqual(t, projectOpts.Content, project.Description)

			// change project settings
			url = fmt.Sprintf("%s/%d/edit", url, tt.projectID)
			session.MakeRequest(t, NewRequestWithJSON(t, "POST", url, &projectOpts), http.StatusSeeOther)

			// check project settings were changed
			project = unittest.AssertExistsAndLoadBean(t, &project_model.Project{ID: tt.projectID})
			assert.Equal(t, projectOpts.Title, project.Title)
			assert.Equal(t, projectOpts.Content, project.Description)
		})
	}

	// wrong owners
	for _, tt := range []struct {
		name      string
		owner     string
		repo      string
		projectID int64
	}{
		{"User, wrong owner", org.Name, "-", userProject.ID},
		{"Organization, wrong owner", user.Name, "-", orgProject.ID},
		{"Repository, wrong owner", user.Name, "-", repoProject.ID},
	} {
		t.Run(tt.name, func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			session := loginUser(t, user.Name)
			url := fmt.Sprintf("/%s/%s/projects/%d/edit", tt.owner, tt.repo, tt.projectID)
			session.MakeRequest(t, NewRequestWithJSON(t, "POST", url, &projectOpts), http.StatusNotFound)
		})
	}
}

func TestProjectWebDeleteProjectColumn(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	// create test user, organization, repository and projects
	user := forgery.CreateUser(t, nil)
	org := forgery.CreateOrganisation(t, user)
	repo := forgery.CreateRepository(t, user, nil)
	userProject := forgery.CreateProject(t, user, nil)
	orgProject := forgery.CreateProject(t, org, nil)
	repoProject := forgery.CreateProject(t, repo, nil)

	// invalid project
	for testName, projectURL := range map[string]string{
		"User, invalid project":         fmt.Sprintf("/%s/-/projects/1234567890/0", user.Name),
		"Organization, invalid project": fmt.Sprintf("/%s/-/projects/1234567890/0", org.Name),
		"Repository, invalid project":   fmt.Sprintf("/%s/%s/projects/1234567890/0", user.Name, repo.Name),
	} {
		t.Run(testName, func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			session := loginUser(t, user.Name)
			session.MakeRequest(t, NewRequest(t, "DELETE", projectURL), http.StatusNotFound)
		})
	}

	// wrong owner
	for testName, projectURL := range map[string]string{
		"User, wrong owner":         fmt.Sprintf("/%s/-/projects/%d/0", org.Name, userProject.ID),
		"Organization, wrong owner": fmt.Sprintf("/%s/-/projects/%d/0", user.Name, orgProject.ID),
		"Repository, wrong owner":   fmt.Sprintf("/%s/-/projects/%d/0", user.Name, repoProject.ID),
	} {
		t.Run(testName, func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			session := loginUser(t, user.Name)
			session.MakeRequest(t, NewRequest(t, "DELETE", projectURL), http.StatusNotFound)
		})
	}

	// invalid column
	for testName, projectURL := range map[string]string{
		"User, invalid column":         fmt.Sprintf("/%s/-/projects/%d/0", user.Name, userProject.ID),
		"Organization, invalid column": fmt.Sprintf("/%s/-/projects/%d/0", org.Name, orgProject.ID),
		"Repository, invalid column":   fmt.Sprintf("/%s/%s/projects/%d/0", user.Name, repo.Name, repoProject.ID),
	} {
		t.Run(testName, func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			defer test.MockVariableValue(&setting.IsProd, false)()
			session := loginUser(t, user.Name)
			resp := session.MakeRequest(t, NewRequest(t, "DELETE", projectURL), http.StatusInternalServerError)

			// template: templates/status/500.tmpl
			// template lines:
			// <div role="main" class="page-content status-page-500">
			// [...]
			// 	<div class="ui container tw-my-8">
			// 		{{if .ErrorMsg}}
			// 			<p>{{ctx.Locale.Tr "error.occurred"}}:</p>
			// 			<pre class="tw-whitespace-pre-wrap tw-break-all">{{.ErrorMsg}}</pre>
			// 		{{end}}
			// [...]
			// 	</div>
			// </div>
			doc := NewHTMLParser(t, resp.Body)
			assert.Contains(t,
				doc.Find(".page-content.status-page-500 .ui.container.tw-my-8 p").Text(),
				translation.NewLocale("en-US").Tr("error.occurred"),
			)
			assert.Contains(t,
				doc.Find(".page-content.status-page-500 .ui.container.tw-my-8 pre.tw-whitespace-pre-wrap.tw-break-all").Text(),
				"column ID must not be empty",
			)
		})
	}

	// no error
	for _, tt := range []struct {
		name      string
		url       string
		projectID int64
	}{
		{"User", fmt.Sprintf("/%s/-/projects", user.Name), userProject.ID},
		{"Organization", fmt.Sprintf("/%s/-/projects", org.Name), orgProject.ID},
		{"Repository", fmt.Sprintf("/%s/%s/projects", user.Name, repo.Name), repoProject.ID},
	} {
		t.Run(tt.name, func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			session := loginUser(t, user.Name)

			// create test columns
			columns := []*project_model.Column{}
			for i := range 2 {
				column := &project_model.Column{
					Title:     fmt.Sprintf("New %s Project Column %d", tt.name, i),
					ProjectID: tt.projectID,
				}
				require.NoError(t, project_model.CreateColumn(t.Context(), column))
				columns = append(columns, column)
			}

			// check column exists
			unittest.AssertExistsIf(t, true, &project_model.Column{
				ID: columns[1].ID, ProjectID: tt.projectID,
			})

			// delete column
			url := fmt.Sprintf("%s/%d/%d", tt.url, tt.projectID, columns[1].ID)
			session.MakeRequest(t, NewRequest(t, "DELETE", url), http.StatusOK)

			// check column does not exist
			unittest.AssertNotExistsBean(t, &project_model.Column{
				ID: columns[1].ID, ProjectID: tt.projectID,
			})
		})
	}
}

func TestProjectWebCreateColumnInProject(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	// create test user, organization, repository and projects
	user := forgery.CreateUser(t, nil)
	org := forgery.CreateOrganisation(t, user)
	repo := forgery.CreateRepository(t, user, nil)
	userProject := forgery.CreateProject(t, user, nil)
	orgProject := forgery.CreateProject(t, org, nil)
	repoProject := forgery.CreateProject(t, repo, nil)

	createOpts := forms_service.EditProjectColumnForm{
		Title:   "TestProjectWebCreateColumnInProject Column1",
		Sorting: 0,
		Color:   "#ab1099",
	}

	// invalid project
	for testName, projectURL := range map[string]string{
		"User, invalid project":         fmt.Sprintf("/%s/-/projects/1234567890", user.Name),
		"Organization, invalid project": fmt.Sprintf("/%s/-/projects/1234567890", org.Name),
		"Repository, invalid project":   fmt.Sprintf("/%s/%s/projects/1234567890", user.Name, repo.Name),
	} {
		t.Run(testName, func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			session := loginUser(t, user.Name)
			session.MakeRequest(t, NewRequestWithJSON(t, "POST", projectURL, &createOpts), http.StatusNotFound)
		})
	}

	// wrong owner
	for testName, projectURL := range map[string]string{
		"User, wrong owner":         fmt.Sprintf("/%s/-/projects/%d", org.Name, userProject.ID),
		"Organization, wrong owner": fmt.Sprintf("/%s/-/projects/%d", user.Name, orgProject.ID),
		"Repository, wrong owner":   fmt.Sprintf("/%s/-/projects/%d", user.Name, repoProject.ID),
	} {
		t.Run(testName, func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			session := loginUser(t, user.Name)
			session.MakeRequest(t, NewRequestWithJSON(t, "POST", projectURL, &createOpts), http.StatusNotFound)
		})
	}

	// bad color
	createOptsBad := forms_service.EditProjectColumnForm{
		Title:   "Col1",
		Sorting: 0,
		Color:   "bad color",
	}
	for testName, projectURL := range map[string]string{
		"User, bad color":         fmt.Sprintf("/%s/-/projects/%d", user.Name, userProject.ID),
		"Organization, bad color": fmt.Sprintf("/%s/-/projects/%d", org.Name, orgProject.ID),
		"Repository, bad color":   fmt.Sprintf("/%s/%s/projects/%d/", user.Name, repo.Name, repoProject.ID),
	} {
		t.Run(testName, func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			defer test.MockVariableValue(&setting.IsProd, false)()
			session := loginUser(t, user.Name)
			resp := session.MakeRequest(t, NewRequestWithJSON(t, "POST", projectURL, &createOptsBad), http.StatusInternalServerError)

			// template: templates/status/500.tmpl
			// template lines:
			// <div role="main" class="page-content status-page-500">
			// [...]
			// 	<div class="ui container tw-my-8">
			// 		{{if .ErrorMsg}}
			// 			<p>{{ctx.Locale.Tr "error.occurred"}}:</p>
			// 			<pre class="tw-whitespace-pre-wrap tw-break-all">{{.ErrorMsg}}</pre>
			// 		{{end}}
			// [...]
			// 	</div>
			// </div>
			doc := NewHTMLParser(t, resp.Body)
			assert.Contains(t,
				doc.Find(".page-content.status-page-500 .ui.container.tw-my-8 p").Text(),
				translation.NewLocale("en-US").Tr("error.occurred"),
			)
			assert.Contains(t,
				doc.Find(".page-content.status-page-500 .ui.container.tw-my-8 pre.tw-whitespace-pre-wrap.tw-break-all").Text(),
				"bad color code: bad color",
			)
		})
	}

	// no error
	for _, tt := range []struct {
		name      string
		url       string
		projectID int64
	}{
		{"User", fmt.Sprintf("/%s/-/projects", user.Name), userProject.ID},
		{"Organization", fmt.Sprintf("/%s/-/projects", org.Name), orgProject.ID},
		{"Repository", fmt.Sprintf("/%s/%s/projects", user.Name, repo.Name), repoProject.ID},
	} {
		t.Run(tt.name, func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			session := loginUser(t, user.Name)

			containFunc := func(columns []*project_model.Column) bool {
				// check if columns contain column identified
				// by createOpts
				for _, c := range columns {
					if c.Title == createOpts.Title &&
						c.Color == createOpts.Color {
						return true
					}
				}
				return false
			}

			// check current columns
			preCols, _, err := project_service.ListProjectColumns(t.Context(), tt.projectID, db.ListOptionsAll)
			require.NoError(t, err)
			assert.Condition(t, func() bool {
				return !containFunc(preCols)
			}, "column list should not contain column")

			// create new column
			url := fmt.Sprintf("%s/%d", tt.url, tt.projectID)
			session.MakeRequest(t, NewRequestWithJSON(t, "POST", url, &createOpts), http.StatusOK)

			// check if column was created
			postCols, _, err := project_service.ListProjectColumns(t.Context(), tt.projectID, db.ListOptionsAll)
			require.NoError(t, err)
			assert.NotEqual(t, preCols, postCols)
			assert.Condition(t, func() bool {
				return containFunc(postCols)
			}, "column list should contain column")
		})
	}
}

func TestProjectWebEditProjectColumn(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	// create test user, organization, repository and projects
	user := forgery.CreateUser(t, nil)
	org := forgery.CreateOrganisation(t, user)
	repo := forgery.CreateRepository(t, user, nil)
	userProject := forgery.CreateProject(t, user, nil)
	orgProject := forgery.CreateProject(t, org, nil)
	repoProject := forgery.CreateProject(t, repo, nil)

	// create test columns
	userColumn := &project_model.Column{
		Title:     "New User Project Column 1",
		ProjectID: userProject.ID,
	}
	require.NoError(t, project_model.CreateColumn(t.Context(), userColumn))
	orgColumn := &project_model.Column{
		Title:     "New Organization Project Column 1",
		ProjectID: orgProject.ID,
	}
	require.NoError(t, project_model.CreateColumn(t.Context(), orgColumn))
	repoColumn := &project_model.Column{
		Title:     "New Repository Project Column 1",
		ProjectID: repoProject.ID,
	}
	require.NoError(t, project_model.CreateColumn(t.Context(), repoColumn))

	// common edit options for tests
	editOpts := forms_service.EditProjectColumnForm{
		Title:   "TestProjectWebEditProjectColumn Column1",
		Sorting: 0,
		Color:   "#ab1099",
	}

	// invalid project
	for testName, projectURL := range map[string]string{
		"User, invalid project":         fmt.Sprintf("/%s/-/projects/1234567890/0", user.Name),
		"Organization, invalid project": fmt.Sprintf("/%s/-/projects/1234567890/0", org.Name),
		"Repository, invalid project":   fmt.Sprintf("/%s/%s/projects/1234567890/0", user.Name, repo.Name),
	} {
		t.Run(testName, func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			session := loginUser(t, user.Name)
			session.MakeRequest(t, NewRequestWithJSON(t, "PUT", projectURL, &editOpts), http.StatusNotFound)
		})
	}

	// wrong owner
	for testName, projectURL := range map[string]string{
		"User, wrong owner":         fmt.Sprintf("/%s/-/projects/%d/0", org.Name, userProject.ID),
		"Organization, wrong owner": fmt.Sprintf("/%s/-/projects/%d/0", user.Name, orgProject.ID),
		"Repository, wrong owner":   fmt.Sprintf("/%s/-/projects/%d/0", user.Name, repoProject.ID),
	} {
		t.Run(testName, func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			session := loginUser(t, user.Name)
			session.MakeRequest(t, NewRequestWithJSON(t, "PUT", projectURL, &editOpts), http.StatusNotFound)
		})
	}

	// invalid column
	for testName, projectURL := range map[string]string{
		"User, invalid column":         fmt.Sprintf("/%s/-/projects/%d/0", user.Name, userProject.ID),
		"Organization, invalid column": fmt.Sprintf("/%s/-/projects/%d/0", org.Name, orgProject.ID),
		"Repository, invalid column":   fmt.Sprintf("/%s/%s/projects/%d/0", user.Name, repo.Name, repoProject.ID),
	} {
		t.Run(testName, func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			defer test.MockVariableValue(&setting.IsProd, false)()
			session := loginUser(t, user.Name)
			resp := session.MakeRequest(t, NewRequestWithJSON(t, "PUT", projectURL, &editOpts), http.StatusInternalServerError)

			// template: templates/status/500.tmpl
			// template lines:
			// <div role="main" class="page-content status-page-500">
			// [...]
			// 	<div class="ui container tw-my-8">
			// 		{{if .ErrorMsg}}
			// 			<p>{{ctx.Locale.Tr "error.occurred"}}:</p>
			// 			<pre class="tw-whitespace-pre-wrap tw-break-all">{{.ErrorMsg}}</pre>
			// 		{{end}}
			// [...]
			// 	</div>
			// </div>
			doc := NewHTMLParser(t, resp.Body)
			assert.Contains(t,
				doc.Find(".page-content.status-page-500 .ui.container.tw-my-8 p").Text(),
				translation.NewLocale("en-US").Tr("error.occurred"),
			)
			assert.Contains(t,
				doc.Find(".page-content.status-page-500 .ui.container.tw-my-8 pre.tw-whitespace-pre-wrap.tw-break-all").Text(),
				"column ID must not be empty",
			)
		})
	}

	// bad color
	editOptsBad := forms_service.EditProjectColumnForm{
		Title:   "Col1",
		Sorting: 0,
		Color:   "bad color",
	}
	for testName, projectURL := range map[string]string{
		"User, bad color":         fmt.Sprintf("/%s/-/projects/%d/%d", user.Name, userProject.ID, userColumn.ID),
		"Organization, bad color": fmt.Sprintf("/%s/-/projects/%d/%d", org.Name, orgProject.ID, orgColumn.ID),
		"Repository, bad color":   fmt.Sprintf("/%s/%s/projects/%d/%d", user.Name, repo.Name, repoProject.ID, repoColumn.ID),
	} {
		t.Run(testName, func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			defer test.MockVariableValue(&setting.IsProd, false)()
			session := loginUser(t, user.Name)
			resp := session.MakeRequest(t, NewRequestWithJSON(t, "PUT", projectURL, &editOptsBad), http.StatusInternalServerError)

			// template: templates/status/500.tmpl
			// template lines:
			// <div role="main" class="page-content status-page-500">
			// [...]
			// 	<div class="ui container tw-my-8">
			// 		{{if .ErrorMsg}}
			// 			<p>{{ctx.Locale.Tr "error.occurred"}}:</p>
			// 			<pre class="tw-whitespace-pre-wrap tw-break-all">{{.ErrorMsg}}</pre>
			// 		{{end}}
			// [...]
			// 	</div>
			// </div>
			doc := NewHTMLParser(t, resp.Body)
			assert.Contains(t,
				doc.Find(".page-content.status-page-500 .ui.container.tw-my-8 p").Text(),
				translation.NewLocale("en-US").Tr("error.occurred"),
			)
			assert.Contains(t,
				doc.Find(".page-content.status-page-500 .ui.container.tw-my-8 pre.tw-whitespace-pre-wrap.tw-break-all").Text(),
				"bad color code: bad color",
			)
		})
	}

	// no error
	for _, tt := range []struct {
		name      string
		url       string
		projectID int64
		columnID  int64
	}{
		{"User", fmt.Sprintf("/%s/-/projects", user.Name), userProject.ID, userColumn.ID},
		{"Organization", fmt.Sprintf("/%s/-/projects", org.Name), orgProject.ID, orgColumn.ID},
		{"Repository", fmt.Sprintf("/%s/%s/projects", user.Name, repo.Name), repoProject.ID, repoColumn.ID},
	} {
		t.Run(tt.name, func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			session := loginUser(t, user.Name)

			// check that column settings differ
			column := unittest.AssertExistsAndLoadBean(t, &project_model.Column{
				ID: tt.columnID, ProjectID: tt.projectID,
			})
			assert.NotEqual(t, editOpts.Title, column.Title)
			assert.NotEqual(t, editOpts.Color, column.Color)

			// change column settings
			url := fmt.Sprintf("%s/%d/%d", tt.url, tt.projectID, tt.columnID)
			session.MakeRequest(t, NewRequestWithJSON(t, "PUT", url, &editOpts), http.StatusOK)

			// check that column settings were changed
			column = unittest.AssertExistsAndLoadBean(t, &project_model.Column{
				ID: tt.columnID, ProjectID: tt.projectID,
			})
			assert.Equal(t, editOpts.Title, column.Title)
			assert.Equal(t, editOpts.Color, column.Color)
		})
	}
}

func TestProjectWebSetDefaultProjectColumn(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	// create test user, organization, repository and projects
	user := forgery.CreateUser(t, nil)
	org := forgery.CreateOrganisation(t, user)
	repo := forgery.CreateRepository(t, user, nil)
	userProject := forgery.CreateProject(t, user, nil)
	orgProject := forgery.CreateProject(t, org, nil)
	repoProject := forgery.CreateProject(t, repo, nil)

	// invalid project
	for testName, projectURL := range map[string]string{
		"User, invalid project":         fmt.Sprintf("/%s/-/projects/1234567890/0/default", user.Name),
		"Organization, invalid project": fmt.Sprintf("/%s/-/projects/1234567890/0/default", org.Name),
		"Repository, invalid project":   fmt.Sprintf("/%s/%s/projects/1234567890/0/default", user.Name, repo.Name),
	} {
		t.Run(testName, func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			session := loginUser(t, user.Name)
			session.MakeRequest(t, NewRequest(t, "POST", projectURL), http.StatusNotFound)
		})
	}

	// wrong owner
	for testName, projectURL := range map[string]string{
		"User, wrong owner":         fmt.Sprintf("/%s/-/projects/%d/0/default", org.Name, userProject.ID),
		"Organization, wrong owner": fmt.Sprintf("/%s/-/projects/%d/0/default", user.Name, orgProject.ID),
		"Repository, wrong owner":   fmt.Sprintf("/%s/-/projects/%d/0/default", user.Name, repoProject.ID),
	} {
		t.Run(testName, func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			session := loginUser(t, user.Name)
			session.MakeRequest(t, NewRequest(t, "POST", projectURL), http.StatusNotFound)
		})
	}

	// invalid column
	for testName, projectURL := range map[string]string{
		"User, invalid column":         fmt.Sprintf("/%s/-/projects/%d/0/default", user.Name, userProject.ID),
		"Organization, invalid column": fmt.Sprintf("/%s/-/projects/%d/0/default", org.Name, orgProject.ID),
		"Repository, invalid column":   fmt.Sprintf("/%s/%s/projects/%d/0/default", user.Name, repo.Name, repoProject.ID),
	} {
		t.Run(testName, func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			defer test.MockVariableValue(&setting.IsProd, false)()
			session := loginUser(t, user.Name)
			resp := session.MakeRequest(t, NewRequest(t, "POST", projectURL), http.StatusInternalServerError)

			// template: templates/status/500.tmpl
			// template lines:
			// <div role="main" class="page-content status-page-500">
			// [...]
			// 	<div class="ui container tw-my-8">
			// 		{{if .ErrorMsg}}
			// 			<p>{{ctx.Locale.Tr "error.occurred"}}:</p>
			// 			<pre class="tw-whitespace-pre-wrap tw-break-all">{{.ErrorMsg}}</pre>
			// 		{{end}}
			// [...]
			// 	</div>
			// </div>
			doc := NewHTMLParser(t, resp.Body)
			assert.Contains(t,
				doc.Find(".page-content.status-page-500 .ui.container.tw-my-8 p").Text(),
				translation.NewLocale("en-US").Tr("error.occurred"),
			)
			assert.Contains(t,
				doc.Find(".page-content.status-page-500 .ui.container.tw-my-8 pre.tw-whitespace-pre-wrap.tw-break-all").Text(),
				"column ID must not be empty",
			)
		})
	}

	// no error
	for _, tt := range []struct {
		name      string
		url       string
		projectID int64
	}{
		{"User", fmt.Sprintf("/%s/-/projects", user.Name), userProject.ID},
		{"Organization", fmt.Sprintf("/%s/-/projects", org.Name), orgProject.ID},
		{"Repository", fmt.Sprintf("/%s/%s/projects", user.Name, repo.Name), repoProject.ID},
	} {
		t.Run(tt.name, func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			session := loginUser(t, user.Name)

			// create test columns
			columns := []*project_model.Column{}
			for i := range 2 {
				column := &project_model.Column{
					Title:     fmt.Sprintf("New %s Project Column %d", tt.name, i),
					ProjectID: tt.projectID,
				}
				require.NoError(t, project_model.CreateColumn(t.Context(), column))
				columns = append(columns, column)
			}

			// check that column is not default
			column := unittest.AssertExistsAndLoadBean(t, &project_model.Column{
				ID: columns[1].ID, ProjectID: tt.projectID,
			})
			assert.False(t, column.Default)

			// set default column
			url := fmt.Sprintf("%s/%d/%d/default", tt.url, tt.projectID, columns[1].ID)
			session.MakeRequest(t, NewRequest(t, "POST", url), http.StatusOK)

			// check that column is default now
			column = unittest.AssertExistsAndLoadBean(t, &project_model.Column{
				ID: columns[1].ID, ProjectID: tt.projectID,
			})
			assert.True(t, column.Default)
		})
	}
}

func TestProjectWebMoveIssues(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	// create test user, organization, repository and projects
	user := forgery.CreateUser(t, nil)
	org := forgery.CreateOrganisation(t, user)
	repo := forgery.CreateRepository(t, user, nil)
	userProject := forgery.CreateProject(t, user, nil)
	orgProject := forgery.CreateProject(t, org, nil)
	repoProject := forgery.CreateProject(t, repo, nil)
	orgRepo := forgery.CreateRepository(t, org.AsUser(), nil)

	moveOpts := &project_structs.MovedIssuesOption{}

	// invalid project
	for testName, projectURL := range map[string]string{
		"User, invalid project":         fmt.Sprintf("/%s/-/projects/1234567890/0/move", user.Name),
		"Organization, invalid project": fmt.Sprintf("/%s/-/projects/1234567890/0/move", org.Name),
		"Repository, invalid project":   fmt.Sprintf("/%s/%s/projects/1234567890/0/move", user.Name, repo.Name),
	} {
		t.Run(testName, func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			session := loginUser(t, user.Name)
			session.MakeRequest(t, NewRequestWithJSON(t, "POST", projectURL, &moveOpts), http.StatusNotFound)
		})
	}

	// wrong owner
	for testName, projectURL := range map[string]string{
		"User, wrong owner":         fmt.Sprintf("/%s/-/projects/%d/0/move", org.Name, userProject.ID),
		"Organization, wrong owner": fmt.Sprintf("/%s/-/projects/%d/0/move", user.Name, orgProject.ID),
		"Repository, wrong owner":   fmt.Sprintf("/%s/-/projects/%d/0/move", user.Name, repoProject.ID),
	} {
		t.Run(testName, func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			session := loginUser(t, user.Name)
			session.MakeRequest(t, NewRequestWithJSON(t, "POST", projectURL, &moveOpts), http.StatusNotFound)
		})
	}

	// invalid column
	for testName, projectURL := range map[string]string{
		"User, invalid column":         fmt.Sprintf("/%s/-/projects/%d/0/move", user.Name, userProject.ID),
		"Organization, invalid column": fmt.Sprintf("/%s/-/projects/%d/0/move", org.Name, orgProject.ID),
		"Repository, invalid column":   fmt.Sprintf("/%s/%s/projects/%d/0/move", user.Name, repo.Name, repoProject.ID),
	} {
		t.Run(testName, func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			defer test.MockVariableValue(&setting.IsProd, false)()
			session := loginUser(t, user.Name)
			resp := session.MakeRequest(t, NewRequestWithJSON(t, "POST", projectURL, &moveOpts), http.StatusInternalServerError)

			// template: templates/status/500.tmpl
			// template lines:
			// <div role="main" class="page-content status-page-500">
			// [...]
			// 	<div class="ui container tw-my-8">
			// 		{{if .ErrorMsg}}
			// 			<p>{{ctx.Locale.Tr "error.occurred"}}:</p>
			// 			<pre class="tw-whitespace-pre-wrap tw-break-all">{{.ErrorMsg}}</pre>
			// 		{{end}}
			// [...]
			// 	</div>
			// </div>
			doc := NewHTMLParser(t, resp.Body)
			assert.Contains(t,
				doc.Find(".page-content.status-page-500 .ui.container.tw-my-8 p").Text(),
				translation.NewLocale("en-US").Tr("error.occurred"),
			)
			assert.Contains(t,
				doc.Find(".page-content.status-page-500 .ui.container.tw-my-8 pre.tw-whitespace-pre-wrap.tw-break-all").Text(),
				"column ID must not be empty",
			)
		})
	}

	// no error
	for _, tt := range []struct {
		name      string
		url       string
		projectID int64
		repo      *repo_model.Repository
	}{
		{"User", fmt.Sprintf("/%s/-/projects", user.Name), userProject.ID, repo},
		{"Organization", fmt.Sprintf("/%s/-/projects", org.Name), orgProject.ID, orgRepo},
		{"Repository", fmt.Sprintf("/%s/%s/projects", user.Name, repo.Name), repoProject.ID, repo},
	} {
		t.Run(tt.name, func(t *testing.T) {
			defer tests.PrintCurrentTest(t)()
			session := loginUser(t, user.Name)

			// create test column
			column := &project_model.Column{
				Title:     fmt.Sprintf("New %s Project Column 1", tt.name),
				ProjectID: tt.projectID,
			}
			require.NoError(t, project_model.CreateColumn(t.Context(), column))

			// create test issues
			for i := range 2 {
				issue := &issues_model.Issue{
					Title:  fmt.Sprintf("test issue %d", i),
					RepoID: tt.repo.ID,
				}
				require.NoError(t, issues_model.NewIssue(t.Context(), tt.repo, issue, nil, nil))
				require.NoError(t, issues_model.IssueAssignOrRemoveProject(
					t.Context(), issue, user, tt.projectID, column.ID,
				))
			}

			// get project issues in column
			preIssues, count, err := column.GetIssues(t.Context(), db.ListOptionsAll)
			require.NoError(t, err)
			assert.Equal(t, int64(2), count)

			// set new sorting in moveOpts
			moveOpts.ProjectIssues = []struct {
				IssueID int64 `json:"issueID"`
				Sorting int64 `json:"sorting"`
			}{
				{preIssues[0].IssueID, preIssues[1].Sorting},
				{preIssues[1].IssueID, preIssues[0].Sorting},
			}

			// change sorting
			url := fmt.Sprintf("%s/%d/%d/move", tt.url, tt.projectID, column.ID)
			session.MakeRequest(t, NewRequestWithJSON(t, "POST", url, &moveOpts), http.StatusOK)

			// check sorting has changed
			postIssues, count, err := column.GetIssues(t.Context(), db.ListOptionsAll)
			require.NoError(t, err)
			assert.Equal(t, int64(2), count)
			assert.NotEqual(t, preIssues, postIssues)
		})
	}
}

// Test creation/getting/updating/deleting project for user
func TestProjectWebCRUD(t *testing.T) {
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
