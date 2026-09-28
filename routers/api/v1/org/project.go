// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package org

import (
	"forgejo.org/routers/api/v1/projects"
	"forgejo.org/services/context"
)

// CreateProject creates a project for the user/org/repo
func (Project) CreateProject(ctx *context.APIContext) {
	// swagger:operation POST /orgs/{org}/projects organization orgCreateProject
	// ---
	// summary: Create a project for an organization
	// consumes:
	// - application/json
	// produces:
	// - application/json
	// parameters:
	// - name: org
	//   in: path
	//   description: name of the organization
	//   type: string
	//   required: true
	// - name: project
	//   in: body
	//   required: true
	//   schema:
	// 		"$ref": "#/definitions/CreateOrUpdateProjectOptions"
	// responses:
	//   "201":
	//     "$ref": "#/responses/Project"

	projects.CreateProject(ctx)
}

// ListProjects gets a list of projects according to query parameters
func (Project) ListProjects(ctx *context.APIContext) {
	// swagger:operation GET /orgs/{org}/projects organization orgListProjects
	// ---
	// summary: Get a list of projects
	// produces:
	// - application/json
	// parameters:
	// - name: org
	//   in: path
	//   description: name of the organization
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
	// swagger:operation GET /orgs/{org}/projects/{project_id} organization orgGetProject
	// ---
	// summary: Get a project
	// produces:
	// - application/json
	// parameters:
	// - name: org
	//   in: path
	//   description: name of the organization
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
	// swagger:operation PATCH /orgs/{org}/projects/{project_id} organization orgUpdateProject
	// ---
	// summary: Update a project
	// consumes:
	// - application/json
	// parameters:
	// - name: org
	//   in: path
	//   description: name of the organization
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
	// swagger:operation DELETE /orgs/{org}/projects/{project_id} organization orgDeleteProject
	// ---
	// summary: Delete a project
	// parameters:
	// - name: org
	//   in: path
	//   description: name of the organization
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
	// swagger:operation GET /orgs/{org}/projects/{project_id}/issues organization orgListProjectIssues
	// ---
	// summary: List ProjectIssues
	// produces:
	// - application/json
	// parameters:
	// - name: org
	//   in: path
	//   description: name of the organization
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
	// swagger:operation POST /orgs/{org}/projects/{project_id}/issues organization orgCreateProjectIssue
	// ---
	// summary: Create a ProjectIssue for project
	// consumes:
	// - application/json
	// produces:
	// - application/json
	// parameters:
	// - name: org
	//   in: path
	//   description: name of the organization
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
	// swagger:operation GET /orgs/{org}/projects/{project_id}/columns organization orgListProjectColumns
	// ---
	// summary: List ProjectColumns
	// produces:
	// - application/json
	// parameters:
	// - name: org
	//   in: path
	//   description: name of the organization
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
	// swagger:operation POST /orgs/{org}/projects/{project_id}/columns organization orgCreateProjectColumn
	// ---
	// summary: Create a ProjectColumn
	// consumes:
	// - application/json
	// produces:
	// - application/json
	// parameters:
	// - name: org
	//   in: path
	//   description: name of the organization
	//   type: string
	//   required: true
	// - name: project_id
	//   in: path
	//   description: the project id
	//   type: string
	//   required: true
	// - name: body
	//   in: body
	//   required: true
	//   schema:
	// 		"$ref": "#/definitions/CreateProjectColumnOptions"
	// responses:
	//   "201":
	//     "$ref": "#/responses/ProjectColumn"
	//   "404":
	//     "$ref": "#/responses/notFound"

	projects.CreateProjectColumn(ctx)
}

// GetProjectColumn gets a column from the project
func (Project) GetProjectColumn(ctx *context.APIContext) {
	// swagger:operation GET /orgs/{org}/projects/{project_id}/column/{column_id} organization orgGetProjectColumn
	// ---
	// summary: Get a ProjectColumn
	// produces:
	// - application/json
	// parameters:
	// - name: org
	//   in: path
	//   description: name of the organization
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
	// swagger:operation PATCH /orgs/{org}/projects/{project_id}/column/{column_id} organization orgUpdateProjectColumn
	// ---
	// summary: Update a ProjectColumn
	// consumes:
	// - application/json
	// parameters:
	// - name: org
	//   in: path
	//   description: name of the organization
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
	// - name: body
	//   in: body
	//   required: true
	//   schema:
	// 		"$ref": "#/definitions/CreateProjectColumnOptions"
	// responses:
	//   "200":
	//     "description": "Successfull update returns 200 OK"
	//   "404":
	//     "$ref": "#/responses/notFound"

	projects.UpdateProjectColumn(ctx)
}

// DeleteProjectColumn deletes a column in the project
func (Project) DeleteProjectColumn(ctx *context.APIContext) {
	// swagger:operation DELETE /orgs/{org}/projects/{project_id}/column/{column_id} organization orgDeleteProjectColumn
	// ---
	// summary: Delete a ProjectColumn
	// parameters:
	// - name: org
	//   in: path
	//   description: name of the organization
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
	// swagger:operation DELETE /orgs/{org}/projects/{project_id}/column/{column_id}/issues organization orgListProjectColumnIssues
	// ---
	// summary: List ProjectIssues of a ProjectColumn
	// produces:
	// - application/json
	// parameters:
	// - name: org
	//   in: path
	//   description: name of the organization
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
	// swagger:operation POST /orgs/{org}/projects/{project_id}/column/{column_id}/issues organization orgCreateProjectColumnIssue
	// ---
	// summary: Create ProjectIssue in a ProjectColumn
	// consumes:
	// - application/json
	// produces:
	// - application/json
	// parameters:
	// - name: org
	//   in: path
	//   description: name of the organization
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
	// - name: body
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
	// swagger:operation GET /orgs/{org}/projects/{project_id}/column/{column_id}/issues/{issue_id} organization orgGetProjectColumnIssue
	// ---
	// summary: Get a ProjectIssue in a ProjectColumn
	// produces:
	// - application/json
	// parameters:
	// - name: org
	//   in: path
	//   description: name of the organization
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
	// swagger:operation PATCH /orgs/{org}/projects/{project_id}/column/{column_id}/issues/{issue_id} organization orgUpdateProjectColumnIssue
	// ---
	// summary: Update a ProjectIssue in a ProjectColumn
	// consumes:
	// - application/json
	// parameters:
	// - name: org
	//   in: path
	//   description: name of the organization
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
	// swagger:operation DELETE /orgs/{org}/projects/{project_id}/column/{column_id}/issues/{issue_id} organization orgDeleteProjectColumnIssue
	// ---
	// summary: Delete a ProjectIssue in a ProjectColumn
	// parameters:
	// - name: org
	//   in: path
	//   description: name of the organization
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
