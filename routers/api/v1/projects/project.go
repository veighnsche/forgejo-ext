// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package projects

import (
	"errors"
	"net/http"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	access_model "forgejo.org/models/perm/access"
	project_model "forgejo.org/models/project"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/optional"
	project_module "forgejo.org/modules/project"
	api "forgejo.org/modules/structs"
	"forgejo.org/modules/validation"
	"forgejo.org/modules/web"
	"forgejo.org/routers/api/v1/utils"
	"forgejo.org/services/context"
	"forgejo.org/services/convert"
	project_service "forgejo.org/services/project"
)

// getOwnerTypeAndOwnerAndRepoFromData returns the project type, owner and
// repository from data in the context.
func getOwnerTypeAndOwnerAndRepoFromData(ctx *context.APIContext) (
	project_module.APIOwnerType, *user_model.User, *repo_model.Repository,
) {
	var ownerType project_module.APIOwnerType
	var owner *user_model.User
	var repo *repo_model.Repository

	switch ctx.Data["OwnerType"] {
	case project_module.APIOwnerTypeIndividual:
		ownerType = project_module.APIOwnerTypeIndividual
		owner = ctx.Doer()
	case project_module.APIOwnerTypeOrganization:
		ownerType = project_module.APIOwnerTypeOrganization
		owner = ctx.User()
	case project_module.APIOwnerTypeRepository:
		ownerType = project_module.APIOwnerTypeRepository
		repo = ctx.Repository()
	}

	return ownerType, owner, repo
}

func getValidColumnOrIssue(ctx *context.APIContext, getColumn, getIssue bool) (*project_model.Column, *project_model.ProjectIssue) {
	if ctx.Project.ProjectID == 0 {
		ctx.Error(http.StatusBadRequest, "Invalid Arguments", "Project ID must not be zero")
		return nil, nil
	}
	var err error
	var projectColumn *project_model.Column
	var projectIssue *project_model.ProjectIssue

	// Did we get the right columnID?
	if getColumn {
		projectColumn, err = project_service.GetValidProjectColumnByID(ctx, ctx.Project.ProjectID, ctx.Project.ProjectColumnID)
		if err != nil {
			if errors.Is(err, validation.ErrNotValid{}) {
				ctx.Error(http.StatusBadRequest, "Invalid Arguments", err)
			} else if errors.Is(err, project_model.ErrProjectColumnNotExist{}) {
				ctx.NotFound("getValidColumnAndIssue", err)
			} else {
				ctx.ServerError("getValidColumnAndIssue", err)
			}
			return nil, nil
		}
	}

	// Did we get the right issueID?
	if getIssue {
		projectIssue, err = project_service.GetValidProjectIssueByID(ctx, ctx.Project.ProjectID, ctx.Project.ProjectColumnID, ctx.Project.ProjectIssueID)
		if err != nil {
			if errors.Is(err, validation.ErrNotValid{}) {
				ctx.Error(http.StatusBadRequest, "Invalid Arguments", err)
			} else if errors.Is(err, project_model.ErrProjectIssueNotExist{}) {
				ctx.NotFound("getValidColumnAndIssue", err)
			} else {
				ctx.ServerError("getValidColumnAndIssue", err)
			}
			return nil, nil
		}
	}

	return projectColumn, projectIssue
}

// CreateProject creates a project for the user/org/repo
func CreateProject(ctx *context.APIContext) {
	form := web.GetForm(ctx).(*api.CreateOrUpdateProjectOptions)
	ownerType, owner, repo := getOwnerTypeAndOwnerAndRepoFromData(ctx)

	// create the project
	p, err := project_service.NewProject(form, owner, repo, ownerType)
	if err != nil {
		ctx.ServerError("Create Project", err)
		return
	}

	if err := project_service.CreateProject(ctx, p); err != nil {
		ctx.ServerError("Create Project", err)
		return
	}

	ctx.JSON(http.StatusCreated, convert.ToAPIProject(p))
}

// ListProjects gets a list of projects according to query parameters
func ListProjects(ctx *context.APIContext) {
	ownerType, owner, repo := getOwnerTypeAndOwnerAndRepoFromData(ctx)

	// Get the projects
	listOptions := utils.GetListOptions(ctx)
	projects, total, err := project_service.ListProjects(ctx,
		owner, repo, ownerType, listOptions)
	if err != nil {
		ctx.ServerError("Get Projects", err)
		return
	}

	ctx.SetLinkHeader(int(total), listOptions.PageSize)
	ctx.SetTotalCountHeader(total)
	ctx.JSON(http.StatusOK, convert.ToAPIProjectList(projects))
}

// GetProject gets a project by id
func GetProject(ctx *context.APIContext) {
	_, owner, repo := getOwnerTypeAndOwnerAndRepoFromData(ctx)
	var err error
	var project *project_model.Project
	if repo != nil {
		project, err = project_service.GetProjectByIDForOwner(ctx, ctx.Project.ProjectID, repo.ID)
	} else {
		project, err = project_service.GetProjectByIDForOwner(ctx, ctx.Project.ProjectID, owner.ID)
	}
	project_service.SetProjectOwnerAndRepo(project, owner, repo)
	if err != nil {
		if errors.Is(err, project_model.ErrProjectNotExist{}) {
			ctx.NotFound("Get Project", err)
		} else if errors.Is(err, validation.ErrNotValid{}) {
			ctx.Error(http.StatusBadRequest, "Invalid Arguments", err)
		} else {
			ctx.ServerError("Get Project", err)
		}
		return
	}

	ctx.JSON(http.StatusOK, convert.ToAPIProject(project))
}

// UpdateProject updates a project by id
func UpdateProject(ctx *context.APIContext) {
	form := web.GetForm(ctx).(*api.CreateOrUpdateProjectOptions)
	_, owner, repo := getOwnerTypeAndOwnerAndRepoFromData(ctx)
	var err error
	var project *project_model.Project
	if repo != nil {
		project, err = project_service.GetProjectByIDForOwner(ctx, ctx.Project.ProjectID, repo.ID)
	} else {
		project, err = project_service.GetProjectByIDForOwner(ctx, ctx.Project.ProjectID, owner.ID)
	}
	if err != nil {
		if errors.Is(err, project_model.ErrProjectNotExist{}) {
			ctx.NotFound("Update Project", err)
		} else if errors.Is(err, validation.ErrNotValid{}) {
			ctx.Error(http.StatusBadRequest, "Invalid Arguments", err)
		} else {
			ctx.ServerError("Update Project", err)
		}
		return
	}
	if err := project_service.UpdateProject(ctx, project, form); err != nil {
		ctx.ServerError("Update Project", err)
		return
	}
	ctx.Status(http.StatusOK)
}

// DeleteProject deletes a project by ID
func DeleteProject(ctx *context.APIContext) {
	err := project_service.DeleteProjectByID(ctx, ctx.Project.ProjectID, optional.None[int64]())
	if err != nil {
		ctx.ServerError("Delete Project", err)
		return
	}

	ctx.Status(http.StatusOK)
}

// ListProjectIssues lists project issues
func ListProjectIssues(ctx *context.APIContext) {
	// get project issues
	listOptions := utils.GetListOptions(ctx)
	projectIssues, total, err := project_service.ListProjectIssues(ctx, ctx.Project.ProjectID, listOptions)
	if err != nil {
		ctx.ServerError("Delete Project", err)
		return
	}

	ctx.SetLinkHeader(int(total), listOptions.PageSize)
	ctx.SetTotalCountHeader(total)
	ctx.JSON(http.StatusOK, convert.ToProjectIssueList(projectIssues))
}

// CreateProjectIssue creates an issue in the default column of the project
func CreateProjectIssue(ctx *context.APIContext) {
	form := web.GetForm(ctx).(*api.CreateProjectIssueOptions)

	issue, err := issues_model.GetIssueByID(ctx, form.IssueID)
	if err != nil {
		ctx.ServerError("Create Project Issue", err)
		return
	}

	projIssue, err := project_service.CreateIssueInProject(ctx, issue, ctx.Doer(), ctx.Project.ProjectID, 0)
	if err != nil {
		ctx.ServerError("CreateProjectIssue", err)
		return
	}

	ctx.JSON(http.StatusCreated, convert.ToProjectIssue(projIssue))
}

// ListProjectColumns lists columns of the project
func ListProjectColumns(ctx *context.APIContext) {
	listOptions := utils.GetListOptions(ctx)
	cols, total, err := db.FindAndCount[project_model.Column](ctx, project_model.FindColumnOptions{
		ListOptions: listOptions, ProjectID: ctx.Project.ProjectID,
	})
	if err != nil {
		ctx.ServerError("ListColumns", err)
		return
	}

	ctx.SetLinkHeader(int(total), listOptions.PageSize)
	ctx.SetTotalCountHeader(total)
	ctx.JSON(http.StatusOK, convert.ToProjectColumnList(cols))
}

// CreateProjectColumn creates a column in the project
func CreateProjectColumn(ctx *context.APIContext) {
	form := web.GetForm(ctx).(*api.CreateProjectColumnOptions)

	col := project_service.NewColumn(form, ctx.Project.ProjectID)
	err := project_service.CreateColumnInProject(ctx, col)
	if err != nil {
		ctx.ServerError("CreateColumn", err)
		return
	}
	ctx.JSON(http.StatusCreated, convert.ToProjectColumn(col))
}

// GetProjectColumn gets a column from the project
func GetProjectColumn(ctx *context.APIContext) {
	projectColumn, _ := getValidColumnOrIssue(ctx, true, false)
	if ctx.Written() {
		return
	}

	ctx.JSON(http.StatusOK, convert.ToProjectColumn(projectColumn))
}

// UpdateProjectColumn updates a column in the project
func UpdateProjectColumn(ctx *context.APIContext) {
	form := web.GetForm(ctx).(*api.CreateProjectColumnOptions)

	projectColumn, _ := getValidColumnOrIssue(ctx, true, false)
	if ctx.Written() {
		return
	}

	if err := project_service.UpdateColumnInProject(ctx, projectColumn, form, ctx.Project.ProjectID, ctx.Project.ProjectColumnID); err != nil {
		ctx.ServerError("UpdateColumn", err)
		return
	}
	ctx.Status(http.StatusOK)
}

// DeleteProjectColumn deletes a column in the project
func DeleteProjectColumn(ctx *context.APIContext) {
	_, _ = getValidColumnOrIssue(ctx, true, false)
	if ctx.Written() {
		return
	}

	if err := project_service.DeleteColumnInProject(ctx, ctx.Project.ProjectColumnID); err != nil {
		ctx.ServerError("DeleteColumn", err)
		return
	}
	ctx.Status(http.StatusOK)
}

// ListProjectColumnIssues lists the issues in a column of a project.
func ListProjectColumnIssues(ctx *context.APIContext) {
	_, _ = getValidColumnOrIssue(ctx, true, false)
	if ctx.Written() {
		return
	}

	// get project issues
	listOptions := utils.GetListOptions(ctx)
	projectIssues, total, err := project_service.ListProjectIssuesByColumn(ctx, ctx.Project.ProjectColumnID, listOptions)
	if err != nil {
		ctx.ServerError("List Project Column Issues", err)
		return
	}

	ctx.SetLinkHeader(int(total), listOptions.PageSize)
	ctx.SetTotalCountHeader(total)
	ctx.JSON(http.StatusOK, convert.ToProjectIssueList(projectIssues))
}

// CreateProjectColumnIssue creates an issue in a column of a project.
func CreateProjectColumnIssue(ctx *context.APIContext) {
	// get form parameters
	form := web.GetForm(ctx).(*api.CreateProjectIssueOptions)
	issueID := form.IssueID

	getValidColumnOrIssue(ctx, true, false)
	if ctx.Written() {
		return
	}

	// get issue
	issue, err := issues_model.GetIssueByID(ctx, issueID)
	if err != nil {
		ctx.ServerError("CreateProjectColumnIssue", err)
		return
	}

	// skip pull request
	if issue.IsPull {
		ctx.ServerError("CreateProjectColumnIssue", errors.New("Issue was pull request"))
		return
	}

	// check issue permissions
	if err := issue.LoadRepo(ctx); err != nil {
		ctx.ServerError("CreateProjectColumnIssue", err)
		return
	}
	perm, err := access_model.GetUserRepoPermissionWithReducer(ctx, issue.Repo, ctx.Doer(), ctx.Reducer())
	if err != nil {
		ctx.ServerError("CreateProjectColumnIssue", err)
		return
	}
	if !perm.CanWriteIssuesOrPulls(issue.IsPull) {
		ctx.NotFound()
		return
	}

	// create project issue
	projIssue, err := project_service.CreateIssueInProject(ctx, issue, ctx.Doer(), ctx.Project.ProjectID, ctx.Project.ProjectColumnID)
	if err != nil {
		ctx.ServerError("CreateProjectColumnIssue", err)
		return
	}

	// return project issue
	ctx.JSON(http.StatusCreated, convert.ToProjectIssue(projIssue))
}

// GetProjectColumnIssue gets an issue in a column in a project.
func GetProjectColumnIssue(ctx *context.APIContext) {
	// get project issue
	_, projectIssue := getValidColumnOrIssue(ctx, false, true)
	if ctx.Written() {
		return
	}

	// get issue to check issue permissions
	issue, err := issues_model.GetIssueByID(ctx, projectIssue.IssueID)
	if err != nil {
		ctx.ServerError("Get Project Column Issue", err)
		return
	}

	// check issue permissions
	perm, err := access_model.GetUserRepoPermissionWithReducer(ctx, issue.Repo, ctx.Doer(), ctx.Reducer())
	if err != nil {
		ctx.ServerError("Get Project Column Issue", err)
		return
	}
	if !perm.CanReadIssuesOrPulls(issue.IsPull) {
		ctx.NotFound()
		return
	}

	// return project issue
	ctx.JSON(http.StatusOK, convert.ToProjectIssue(projectIssue))
}

// UpdateProjectColumnIssue updates an issue in a column in a project.
func UpdateProjectColumnIssue(ctx *context.APIContext) {
	// get form parameters
	form := web.GetForm(ctx).(*api.UpdateProjectColumnIssueOptions)
	newColumnID := form.ProjectColumnID
	newSorting := form.Sorting

	// get project issue
	_, projectIssue := getValidColumnOrIssue(ctx, false, true)
	if ctx.Written() {
		return
	}

	// get new column
	column, err := project_service.GetValidProjectColumnByID(ctx, ctx.Project.ProjectID, newColumnID)
	if err != nil {
		ctx.ServerError("Update Project Column Issue", err)
		return
	}

	// move issue
	movedIssues := &api.MovedIssuesOption{
		ProjectIssues: []api.MovedProjectIssue{
			{
				IssueID: projectIssue.IssueID,
				Sorting: newSorting,
			},
		},
	}
	if err := project_service.MoveIssuesOnProjectColumn(ctx, column, movedIssues); err != nil {
		ctx.ServerError("Update Project Column Issue", err)
		return
	}
}

// DeleteProjectColumnIssue deletes an issue in a column in a project.
func DeleteProjectColumnIssue(ctx *context.APIContext) {
	// get project issue
	_, projectIssue := getValidColumnOrIssue(ctx, false, true)
	if ctx.Written() {
		return
	}

	// get issue
	issue, err := issues_model.GetIssueByID(ctx, projectIssue.IssueID)
	if err != nil {
		ctx.ServerError("Delete Project Column Issue", err)
		return
	}

	// check issue permissions
	if err := issue.LoadRepo(ctx); err != nil {
		ctx.ServerError("Delete Project Column Issue", err)
		return
	}
	perm, err := access_model.GetUserRepoPermissionWithReducer(ctx, issue.Repo, ctx.Doer(), ctx.Reducer())
	if err != nil {
		ctx.ServerError("Delete Project Column Issue", err)
		return
	}
	if !perm.CanWriteIssuesOrPulls(issue.IsPull) {
		ctx.NotFound()
		return
	}

	// remove project issue
	if err := project_service.RemoveIssueFromProject(ctx, issue, ctx.Doer(), ctx.Project.ProjectColumnID); err != nil {
		ctx.ServerError("Delete Project Column Issue REMOVE", err)
		return
	}
}
