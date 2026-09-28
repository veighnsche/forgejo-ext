// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"forgejo.org/routers/api/v1/projects"
	"forgejo.org/services/context"
)

// CreateProject creates a project for the user/org/repo
func (Project) CreateProject(ctx *context.APIContext) {
	// swagger:operation POST /repos/{username}/{reponame}/projects repository repoCreateProject
	// ---
	// summary: Create a project for a user
	// consumes:
	// - application/json
	// produces:
	// - application/json
	// parameters:
	// - name: username
	//   in: path
	//   description: name of the user
	//   type: string
	//   required: true
	// - name: reponame
	//   in: path
	//   description: name of the repository
	//   type: string
	//   required: true
	// - name: body
	//   in: body
	//   required: true
	//   schema:
	// 		 "$ref": "#/definitions/CreateOrUpdateProjectOptions"
	// responses:
	//   "201":
	//     "$ref": "#/responses/Project"

	projects.CreateProject(ctx)
}

// ListProjects gets a list of projects according to query parameters
func (Project) ListProjects(ctx *context.APIContext) {
	// swagger:operation GET /repos/{username}/{reponame}/projects repository repoListProjects
	// ---
	// summary: Get a list of projects
	// produces:
	// - application/json
	// parameters:
	// - name: username
	//   in: path
	//   description: name of the user
	//   type: string
	//   required: true
	// - name: reponame
	//   in: path
	//   description: name of the repository
	//   type: string
	//   required: true
	// responses:
	//   "200":
	//     "$ref": "#/responses/ProjectList"
	//   "404":
	//     "$ref": "#/responses/notFound"

	projects.ListProjects(ctx)
}

// GetProject gets a project by id
func (Project) GetProject(ctx *context.APIContext) {
	// swagger:operation GET /repos/{username}/{reponame}/projects/{project_id} repository repoGetProject
	// ---
	// summary: Get a project
	// produces:
	// - application/json
	// parameters:
	// - name: username
	//   in: path
	//   description: name of the user
	//   type: string
	//   required: true
	// - name: reponame
	//   in: path
	//   description: name of the repository
	//   type: string
	//   required: true
	// - name: project_id
	//   in: path
	//   description: the project id
	//   type: string
	//   required: true
	// responses:
	//   "200":
	//     "$ref": "#/responses/Project"
	//   "404":
	//     "$ref": "#/responses/notFound"

	projects.GetProject(ctx)
}

// UpdateProject updates a project by id
func (Project) UpdateProject(ctx *context.APIContext) {
	// swagger:operation PATCH /repos/{username}/{reponame}/projects/{project_id} repository repoUpdateProject
	// ---
	// summary: Update a project
	// consumes:
	// - application/json
	// parameters:
	// - name: username
	//   in: path
	//   description: name of the user
	//   type: string
	//   required: true
	// - name: reponame
	//   in: path
	//   description: name of the repository
	//   type: string
	//   required: true
	// - name: project_id
	//   in: path
	//   description: the project id
	//   type: string
	//   required: true
	// - name: project
	//   in: body
	//   required: true
	//   schema:
	// 		"$ref": "#/definitions/CreateOrUpdateProjectOptions"
	// responses:
	//   "200":
	//     "description": "Successfull update returns 200 OK"
	//   "404":
	//     "$ref": "#/responses/notFound"

	projects.UpdateProject(ctx)
}

// DeleteProject deletes a project by ID
func (Project) DeleteProject(ctx *context.APIContext) {
	// swagger:operation DELETE /repos/{username}/{reponame}/projects/{project_id} repository repoDeleteProject
	// ---
	// summary: Delete a project
	// parameters:
	// - name: username
	//   in: path
	//   description: name of the user
	//   type: string
	//   required: true
	// - name: reponame
	//   in: path
	//   description: name of the repository
	//   type: string
	//   required: true
	// - name: project_id
	//   in: path
	//   description: the project id
	//   type: string
	//   required: true
	// responses:
	//   "200":
	//     "description": "Successfull delete returns 200 OK"
	//   "404":
	//     "$ref": "#/responses/notFound"

	projects.DeleteProject(ctx)
}

// ListProjectIssues lists project issues
func (Project) ListProjectIssues(ctx *context.APIContext) {
	// swagger:operation GET /repos/{username}/{reponame}/projects/{project_id}/issues repository repoListProjectIssues
	// ---
	// summary: List ProjectIssues
	// produces:
	// - application/json
	// parameters:
	// - name: username
	//   in: path
	//   description: name of the user
	//   type: string
	//   required: true
	// - name: reponame
	//   in: path
	//   description: name of the repository
	//   type: string
	//   required: true
	// - name: project_id
	//   in: path
	//   description: the project id
	//   type: string
	//   required: true
	// responses:
	//   "200":
	//     "$ref": "#/responses/ProjectIssueList"
	//   "404":
	//     "$ref": "#/responses/notFound"

	projects.ListProjectIssues(ctx)
}

// CreateProjectIssue creates an issue in the default column of the project
func (Project) CreateProjectIssue(ctx *context.APIContext) {
	// swagger:operation POST /repos/{username}/{reponame}/projects/{project_id}/issues repository repoCreateProjectIssue
	// ---
	// summary: Create a ProjectIssue for project
	// consumes:
	// - application/json
	// produces:
	// - application/json
	// parameters:
	// - name: username
	//   in: path
	//   description: name of the user
	//   type: string
	//   required: true
	// - name: reponame
	//   in: path
	//   description: name of the repository
	//   type: string
	//   required: true
	// - name: project_id
	//   in: path
	//   description: the project id
	//   type: string
	//   required: true
	// - name: projectIssue
	//   in: body
	//   required: true
	//   schema:
	// 		"$ref": "#/definitions/CreateProjectIssueOptions"
	// responses:
	//   "200":
	//     "$ref": "#/responses/ProjectIssue"
	//   "404":
	//     "$ref": "#/responses/notFound"

	projects.CreateProjectIssue(ctx)
}

// ListProjectColumns lists columns of the project
func (Project) ListProjectColumns(ctx *context.APIContext) {
	// swagger:operation GET /repos/{username}/{reponame}/projects/{project_id}/columns repository repoListProjectColumns
	// ---
	// summary: List ProjectColumns
	// produces:
	// - application/json
	// parameters:
	// - name: username
	//   in: path
	//   description: name of the user
	//   type: string
	//   required: true
	// - name: reponame
	//   in: path
	//   description: name of the repository
	//   type: string
	//   required: true
	// - name: project_id
	//   in: path
	//   description: the project id
	//   type: string
	//   required: true
	// responses:
	//   "200":
	//     "$ref": "#/responses/ProjectColumnList"
	//   "404":
	//     "$ref": "#/responses/notFound"

	projects.ListProjectColumns(ctx)
}

// CreateProjectColumn creates a column in the project
func (Project) CreateProjectColumn(ctx *context.APIContext) {
	// swagger:operation POST /repos/{username}/{reponame}/projects/{project_id}/columns repository repoCreateProjectColumn
	// ---
	// summary: Create a ProjectColumn
	// consumes:
	// - application/json
	// produces:
	// - application/json
	// parameters:
	// - name: username
	//   in: path
	//   description: name of the user
	//   type: string
	//   required: true
	// - name: reponame
	//   in: path
	//   description: name of the repository
	//   type: string
	//   required: true
	// - name: project_id
	//   in: path
	//   description: the project id
	//   type: string
	//   required: true
	// - name: projectColumn
	//   in: body
	//   required: true
	//   schema:
	// 		"$ref": "#/definitions/CreateOrUpdateProjectColumnOptions"
	// responses:
	//   "201":
	//     "$ref": "#/responses/ProjectColumn"
	//   "404":
	//     "$ref": "#/responses/notFound"

	projects.CreateProjectColumn(ctx)
}

// GetProjectColumn gets a column from the project
func (Project) GetProjectColumn(ctx *context.APIContext) {
	// swagger:operation GET /repos/{username}/{reponame}/projects/{project_id}/column/{column_id} repository repoGetProjectColumn
	// ---
	// summary: Get a ProjectColumn
	// produces:
	// - application/json
	// parameters:
	// - name: username
	//   in: path
	//   description: name of the user
	//   type: string
	//   required: true
	// - name: reponame
	//   in: path
	//   description: name of the repository
	//   type: string
	//   required: true
	// - name: project_id
	//   in: path
	//   description: the project id
	//   type: string
	//   required: true
	// - name: column_id
	//   in: path
	//   description: the column id
	//   type: string
	//   required: true
	// responses:
	//   "200":
	//     "$ref": "#/responses/ProjectColumn"
	//   "404":
	//     "$ref": "#/responses/notFound"

	projects.GetProjectColumn(ctx)
}

// UpdateProjectColumn updates a column in the project
func (Project) UpdateProjectColumn(ctx *context.APIContext) {
	// swagger:operation PATCH /repos/{username}/{reponame}/projects/{project_id}/column/{column_id} repository repoUpdateProjectColumn
	// ---
	// summary: Update a ProjectColumn
	// consumes:
	// - application/json
	// parameters:
	// - name: username
	//   in: path
	//   description: name of the user
	//   type: string
	//   required: true
	// - name: reponame
	//   in: path
	//   description: name of the repository
	//   type: string
	//   required: true
	// - name: project_id
	//   in: path
	//   description: the project id
	//   type: string
	//   required: true
	// - name: column_id
	//   in: path
	//   description: the column id
	//   type: string
	//   required: true
	// - name: projectColumn
	//   in: body
	//   required: true
	//   schema:
	// 		"$ref": "#/definitions/CreateOrUpdateProjectColumnOptions"
	// responses:
	//   "200":
	//     "description": "Successfull update returns 200 OK"
	//   "404":
	//     "$ref": "#/responses/notFound"

	projects.UpdateProjectColumn(ctx)
}

// DeleteProjectColumn deletes a column in the project
func (Project) DeleteProjectColumn(ctx *context.APIContext) {
	// swagger:operation DELETE /repos/{username}/{reponame}/projects/{project_id}/column/{column_id} repository repoDeleteProjectColumn
	// ---
	// summary: Delete a ProjectColumn
	// parameters:
	// - name: username
	//   in: path
	//   description: name of the user
	//   type: string
	//   required: true
	// - name: reponame
	//   in: path
	//   description: name of the repository
	//   type: string
	//   required: true
	// - name: project_id
	//   in: path
	//   description: the project id
	//   type: string
	//   required: true
	// - name: column_id
	//   in: path
	//   description: the column id
	//   type: string
	//   required: true
	// responses:
	//   "200":
	//     "description": "Successfull delete returns 200 OK"
	//   "404":
	//     "$ref": "#/responses/notFound"

	projects.DeleteProjectColumn(ctx)
}

// ListProjectColumnIssues lists the issues in a column of a project.
func (Project) ListProjectColumnIssues(ctx *context.APIContext) {
	// swagger:operation DELETE /repos/{username}/{reponame}/projects/{project_id}/column/{column_id}/issues repository repoListProjectColumnIssues
	// ---
	// summary: List ProjectIssues of a ProjectColumn
	// produces:
	// - application/json
	// parameters:
	// - name: username
	//   in: path
	//   description: name of the user
	//   type: string
	//   required: true
	// - name: reponame
	//   in: path
	//   description: name of the repository
	//   type: string
	//   required: true
	// - name: project_id
	//   in: path
	//   description: the project id
	//   type: string
	//   required: true
	// - name: column_id
	//   in: path
	//   description: the column id
	//   type: string
	//   required: true
	// responses:
	//   "200":
	//     "$ref": "#/responses/ProjectIssueList"
	//   "404":
	//     "$ref": "#/responses/notFound"

	projects.ListProjectColumnIssues(ctx)
}

// CreateProjectColumnIssue creates an issue in a column of a project.
func (Project) CreateProjectColumnIssue(ctx *context.APIContext) {
	// swagger:operation POST /repos/{username}/{reponame}/projects/{project_id}/column/{column_id}/issues repository repoCreateProjectColumnIssue
	// ---
	// summary: Create ProjectIssue in a ProjectColumn
	// consumes:
	// - application/json
	// produces:
	// - application/json
	// parameters:
	// - name: username
	//   in: path
	//   description: name of the user
	//   type: string
	//   required: true
	// - name: reponame
	//   in: path
	//   description: name of the repository
	//   type: string
	//   required: true
	// - name: project_id
	//   in: path
	//   description: the project id
	//   type: string
	//   required: true
	// - name: column_id
	//   in: path
	//   description: the column id
	//   type: string
	//   required: true
	// - name: projectIssue
	//   in: body
	//   required: true
	//   schema:
	// 		"$ref": "#/definitions/CreateProjectIssueOptions"
	// responses:
	//   "201":
	//     "$ref": "#/responses/ProjectIssue"
	//   "404":
	//     "$ref": "#/responses/notFound"

	projects.CreateProjectColumnIssue(ctx)
}

// GetProjectColumnIssue gets an issue in a column in a project.
func (Project) GetProjectColumnIssue(ctx *context.APIContext) {
	// swagger:operation GET /repos/{username}/{reponame}/projects/{project_id}/column/{column_id}/issues/{issue_id} repository repoGetProjectColumnIssue
	// ---
	// summary: Get a ProjectIssue in a ProjectColumn
	// produces:
	// - application/json
	// parameters:
	// - name: username
	//   in: path
	//   description: name of the user
	//   type: string
	//   required: true
	// - name: reponame
	//   in: path
	//   description: name of the repository
	//   type: string
	//   required: true
	// - name: project_id
	//   in: path
	//   description: the project id
	//   type: string
	//   required: true
	// - name: column_id
	//   in: path
	//   description: the column id
	//   type: string
	//   required: true
	// - name: issue_id
	//   in: path
	//   description: the issue id
	//   type: string
	//   required: true
	// responses:
	//   "201":
	//     "$ref": "#/responses/ProjectIssue"
	//   "404":
	//     "$ref": "#/responses/notFound"

	projects.GetProjectColumnIssue(ctx)
}

// UpdateProjectColumnIssue updates an issue in a column in a project.
func (Project) UpdateProjectColumnIssue(ctx *context.APIContext) {
	// swagger:operation PATCH /repos/{username}/{reponame}/projects/{project_id}/column/{column_id}/issues/{issue_id} repository repoUpdateProjectColumnIssue
	// ---
	// summary: Update a ProjectIssue in a ProjectColumn
	// consumes:
	// - application/json
	// parameters:
	// - name: username
	//   in: path
	//   description: name of the user
	//   type: string
	//   required: true
	// - name: reponame
	//   in: path
	//   description: name of the repository
	//   type: string
	//   required: true
	// - name: project_id
	//   in: path
	//   description: the project id
	//   type: string
	//   required: true
	// - name: column_id
	//   in: path
	//   description: the column id
	//   type: string
	//   required: true
	// - name: issue_id
	//   in: path
	//   description: the issue id
	//   type: string
	//   required: true
	// - name: projectIssue
	//   in: body
	//   required: true
	//   schema:
	// 		"$ref": "#/definitions/UpdateProjectIssueOptions"
	// responses:
	//   "200":
	//     "description": "Successfull update returns 200 OK"
	//   "404":
	//     "$ref": "#/responses/notFound"

	projects.UpdateProjectColumnIssue(ctx)
}

// DeleteProjectColumnIssue deletes an issue in a column in a project.
func (Project) DeleteProjectColumnIssue(ctx *context.APIContext) {
	// swagger:operation DELETE /repos/{username}/{reponame}/projects/{project_id}/column/{column_id}/issues/{issue_id} repository repoDeleteProjectColumnIssue
	// ---
	// summary: Delete a ProjectIssue in a ProjectColumn
	// parameters:
	// - name: username
	//   in: path
	//   description: name of the user
	//   type: string
	//   required: true
	// - name: reponame
	//   in: path
	//   description: name of the repository
	//   type: string
	//   required: true
	// - name: project_id
	//   in: path
	//   description: the project id
	//   type: string
	//   required: true
	// - name: column_id
	//   in: path
	//   description: the column id
	//   type: string
	//   required: true
	// - name: issue_id
	//   in: path
	//   description: the issue id
	//   type: string
	//   required: true
	// responses:
	//   "200":
	//     "description": "Successfull delete returns 200 OK"
	//   "404":
	//     "$ref": "#/responses/notFound"

	projects.DeleteProjectColumnIssue(ctx)
}

var _ context.ProjectAPI = new(Project)

// Project implements context.ProjectAPI
type Project struct{}

// NewProject creates a new ProjectAPI service
func NewProjectAPI() context.ProjectAPI {
	return Project{}
}
