// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package context

import (
	go_ctx "context"
	"errors"
	"fmt"
	"net/http"

	"forgejo.org/models/organization"
	"forgejo.org/models/perm"
	project_model "forgejo.org/models/project"
	repo_model "forgejo.org/models/repo"
	unit_model "forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	project_module "forgejo.org/modules/project"
	project_service "forgejo.org/services/project"
)

// ReqProjectIDAssignableToIssueAndSetData expects a repository to be set in context
func ReqProjectIDAssignableToIssueAndSetData(ctx *Context, projectID int64) {
	project := getProjectAndEnsureOwner(ctx, projectID)
	if ctx.Written() {
		return
	}
	reqPermissionToAssignProjectToIssue(ctx, project.Type)
	if ctx.Written() {
		return
	}
	ctx.Data["Project"] = project
	ctx.Data["project_id"] = project.ID
}

// ReqProjectIDAssignableToIssue expects a repository to be set in context
func ReqProjectIDAssignableToIssue(ctx *Context, projectID int64) {
	project := getProjectAndEnsureOwner(ctx, projectID)
	if ctx.Written() {
		return
	}
	reqPermissionToAssignProjectToIssue(ctx, project.Type)
}

// getProjectAndEnsureOwner relies on the fact, that ctx.Repo.Repository is always set
func getProjectAndEnsureOwner(ctx *Context, projectID int64) *project_model.Project {
	project, err := project_model.GetProjectByID(ctx, projectID)
	if err != nil {
		if project_model.IsErrProjectNotExist(err) {
			ctx.NotFound(fmt.Sprintf("project %d is not found", projectID), nil)
			return nil
		}
		log.Error("project_model.GetProjectByID(%d): %v", projectID, err)
		ctx.ServerError(fmt.Sprintf("project_model.GetProjectByID(%d)", projectID), err)
		return nil
	}
	ownerID := ctx.Repo.Repository.OwnerID
	if project.Type == project_module.TypeRepository {
		ownerID = ctx.Repo.Repository.ID
	}
	err = project_service.EnsureProjectOwnedBy(project, ownerID)
	if err != nil {
		log.Error("project_model.GetProjectByID(%d): %v", projectID, err)
		ctx.NotFound(fmt.Sprintf("project %d did not belong to owner %d", projectID, ownerID), nil)
		return nil
	}
	return project
}

func reqPermissionToAssignProjectToIssue(ctx *Context, ownerType project_module.OwnerType) {
	switch ownerType {
	case project_module.TypeRepository:
		if !ctx.Repo.Repository.UnitEnabled(ctx, unit_model.TypeProjects) {
			ctx.NotFound("repository projects are disabled", nil)
			return
		}
		if !ctx.Repo.CanRead(unit_model.TypeProjects) {
			ctx.Error(http.StatusForbidden, "doesn't have permissions to read repository projects")
			return
		}
		if !ctx.Repo.CanWrite(unit_model.TypeIssues) {
			ctx.Error(http.StatusForbidden, "doesn't have permissions to write repository issues")
			return
		}
	case project_module.TypeOrganization:
		if !ctx.Org.CanReadUnit(ctx, unit_model.TypeProjects) {
			ctx.Error(http.StatusForbidden, "doesn't have permissions to read the owner projects")
			return
		}
		if !ctx.Org.CanWriteUnit(ctx, unit_model.TypeIssues) {
			ctx.Error(http.StatusForbidden, "doesn't have permissions to write the repository issues and set the project")
			return
		}
	case project_module.TypeIndividual:
		if !ctx.Repo.CanWrite(unit_model.TypeIssues) {
			ctx.Error(http.StatusForbidden, "doesn't have permissions to write the repository issues and set the project")
			return
		}
	default:
		ctx.ServerError(fmt.Sprintf("unexpected project type %v", ownerType), nil)
	}
}

type Project struct {
	ProjectID       int64
	ProjectColumnID int64
	ProjectIssueID  int64
}

func GetOwnerType(isOrg, isRepo bool) project_module.OwnerType {
	var t project_module.OwnerType
	if isOrg {
		t = project_module.TypeOrganization
	} else if isRepo {
		t = project_module.TypeRepository
	} else {
		t = project_module.TypeIndividual
	}
	return t
}

func hasProjectPermission(ctx go_ctx.Context, doer, contextUser *user_model.User, write bool) (bool, error) {
	if write {
		return hasWriteProjectPermission(ctx, doer, contextUser)
	}
	return hasReadProjectPermission(ctx, doer, contextUser)
}

func hasRepoWriteProjectPermission(ctx go_ctx.Context, repo *repo_model.Repository, repoWriter, repoAdmin bool) (bool, error) {
	if repo.UnitEnabled(ctx, unit_model.TypeProjects) {
		if !repoWriter && !repoAdmin {
			return false, nil
		}
	} else {
		return false, errors.New("HasRepoProjectPermission, Projects not enabled")
	}
	return true, nil
}

func hasRepoReadProjectPermission(ctx go_ctx.Context, repo *repo_model.Repository, repoReader, repoAdmin bool) (bool, error) {
	if repo.UnitEnabled(ctx, unit_model.TypeProjects) {
		if !repoReader && !repoAdmin {
			return false, nil
		}
	} else {
		return false, errors.New("HasRepoProjectPermission, Projects not enabled")
	}
	return true, nil
}

// hasWriteProjectPermission checks if the doer has permission to write
func hasWriteProjectPermission(ctx go_ctx.Context, doer, contextUser *user_model.User) (bool, error) {
	ownerType := GetOwnerType(contextUser.IsOrganization(), false)
	if doer.IsAdmin {
		// All perms granted
		return true, nil
	}
	switch ownerType {
	// If creation target is org, doer must be org owner, org admin, team member with valid team perms or site admin
	case project_module.TypeOrganization:
		var validTeamPermission bool
		isOwner, err := organization.IsOrganizationOwner(ctx, contextUser.ID, doer.ID)
		if err != nil {
			return false, err
		}
		isOrgAdmin, err := organization.IsOrganizationAdmin(ctx, contextUser.ID, doer.ID)
		if err != nil {
			return false, err
		}
		teams, err := organization.GetUserOrgTeams(ctx, contextUser.ID, doer.ID)
		if err != nil {
			return false, err
		}
		for _, team := range teams {
			for _, unit := range team.Units {
				if unit.AccessMode >= perm.AccessModeWrite && unit.Type == unit_model.TypeProjects {
					validTeamPermission = true
					break
				}
			}
			if validTeamPermission {
				break
			}
		}
		if !isOwner && !isOrgAdmin && !validTeamPermission {
			return false, nil
		}
	// If creation target is user, doer and context user must be identical or site admin
	case project_module.TypeIndividual:
		if contextUser.ID != doer.ID {
			return false, nil
		}
	default:
		return false, nil
	}
	return true, nil
}

// hasReadProjectPermission checks if the doer has permission to read
func hasReadProjectPermission(ctx go_ctx.Context, doer, contextUser *user_model.User) (bool, error) {
	ownerType := GetOwnerType(contextUser.IsOrganization(), false)
	if doer.IsAdmin {
		// All perms granted
		return true, nil
	}
	switch ownerType {
	// If read target is org, doer must be at least org member
	case project_module.TypeOrganization:
		isMember, err := organization.IsOrganizationMember(ctx, contextUser.ID, doer.ID)
		if err != nil {
			return false, err
		}
		if !isMember {
			return false, nil
		}
	// If read target is user, doer and context user must be identical or site admin
	case project_module.TypeIndividual:
		if contextUser.ID != doer.ID && !contextUser.Visibility.IsPublic() {
			return false, nil
		}
	default:
		return false, nil
	}
	return true, nil
}

// API interface for projects
type ProjectAPI interface {
	// CreateProject creates a project for the user/org/repo
	CreateProject(ctx *APIContext)
	// ListProjects gets a list of projects according to query parameters
	ListProjects(ctx *APIContext)
	// GetProject gets a project by id
	GetProject(ctx *APIContext)
	// UpdateProject updates a project by id
	UpdateProject(ctx *APIContext)
	// DeleteProject deletes a project by ID
	DeleteProject(ctx *APIContext)
	// ListProjectIssues lists project issues
	ListProjectIssues(ctx *APIContext)
	// CreateProjectIssue creates an issue in the default column of the project
	CreateProjectIssue(ctx *APIContext)
	// ListProjectColumns lists columns of the project
	ListProjectColumns(ctx *APIContext)
	// CreateProjectColumn creates a column in the project
	CreateProjectColumn(ctx *APIContext)
	// GetProjectColumn gets a column from the project
	GetProjectColumn(ctx *APIContext)
	// UpdateProjectColumn updates a column in the project
	UpdateProjectColumn(ctx *APIContext)
	// DeleteProjectColumn deletes a column in the project
	DeleteProjectColumn(ctx *APIContext)
	// ListProjectColumnIssues lists the issues in a column of a project.
	ListProjectColumnIssues(ctx *APIContext)
	// CreateProjectColumnIssue creates an issue in a column of a project.
	CreateProjectColumnIssue(ctx *APIContext)
	// GetProjectColumnIssue gets an issue in a column in a project.
	GetProjectColumnIssue(ctx *APIContext)
	// UpdateProjectColumnIssue updates an issue in a column in a project.
	UpdateProjectColumnIssue(ctx *APIContext)
	// DeleteProjectColumnIssue deletes an issue in a column in a project.
	DeleteProjectColumnIssue(ctx *APIContext)
}

func hasPerms(ctx *APIContext, write bool) (bool, error) {
	var err error
	var hasPermission bool
	switch ctx.Data["OwnerType"] {
	case project_module.APIOwnerTypeIndividual:
		hasPermission, err = hasProjectPermission(ctx, ctx.Doer(), ctx.User(), write)
	case project_module.APIOwnerTypeOrganization:
		hasPermission, err = hasProjectPermission(ctx, ctx.Doer(), ctx.User(), write)
	case project_module.APIOwnerTypeRepository:
		repoWriter := ctx.Repository().IsPrivate
		if write {
			repoWriter = ctx.IsUserRepoWriter([]unit_model.Type{unit_model.TypeProjects})
			isAdmin := ctx.IsUserRepoAdmin() || ctx.IsUserSiteAdmin() || ctx.IsUserRepoWriter([]unit_model.Type{unit_model.TypeProjects})
			hasPermission, err = hasRepoWriteProjectPermission(ctx, ctx.Repository(), repoWriter, isAdmin)
		} else {
			repoReader := ctx.Repository().IsPrivate
			admin := ctx.IsUserRepoAdmin() || ctx.IsUserSiteAdmin() || ctx.IsUserRepoWriter([]unit_model.Type{unit_model.TypeProjects})
			hasPermission, err = hasRepoReadProjectPermission(ctx, ctx.Repo().Repository, repoReader, admin)
		}
	}
	if err != nil {
		return false, err
	}
	if !hasPermission {
		return false, nil
	}
	return true, nil
}

// ProjectAssignment returns a middleware to handle project assignment
func ProjectAssignment(ctx *APIContext) {
	if _, repoAssignmentOnce := ctx.Data["projectAssignmentExecuted"]; repoAssignmentOnce {
		log.Trace("ProjectAssignment was exec already, skipping second call ...")
		return
	}
	ctx.Data["projectAssignmentExecuted"] = true

	// get path parameters
	projectID := ctx.ParamsInt64("project_id")
	if projectID == 0 {
		ctx.NotFound("invalid project ID", nil)
		return
	}
	columnID := ctx.ParamsInt64("column_id")
	issueID := ctx.ParamsInt64("issue_id")

	ctx.SetProject(&Project{
		ProjectID:       projectID,
		ProjectColumnID: columnID,
		ProjectIssueID:  issueID,
	})
}

func ReqProjectReadPermissions(ctx *APIContext) {
	hasRead, err := hasPerms(ctx, false)
	if err != nil {
		ctx.ServerError("hasReadPerms", err)
		return
	}
	if !hasRead {
		// forbidden
		ctx.Error(http.StatusForbidden, "ProjectHasRead", "The user did not have sufficient permissions")
		return
	}
}

func ReqProjectWritePermissions(ctx *APIContext) {
	hasWrite, err := hasPerms(ctx, true)
	if err != nil {
		ctx.ServerError("hasWritePerms", err)
		return
	}
	if !hasWrite {
		// forbidden
		ctx.Error(http.StatusForbidden, "ProjectHasWrite", "The user did not have sufficient permissions")
		return
	}
}
