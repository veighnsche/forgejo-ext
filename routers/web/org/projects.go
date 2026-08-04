// Copyright 2022 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package org

import (
	"fmt"
	"net/http"
	"strings"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	project_model "forgejo.org/models/project"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/modules/base"
	"forgejo.org/modules/json"
	"forgejo.org/modules/log"
	"forgejo.org/modules/optional"
	project_module "forgejo.org/modules/project"
	"forgejo.org/modules/setting"
	project_structs "forgejo.org/modules/structs"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/validation"
	"forgejo.org/modules/web"
	shared_user "forgejo.org/routers/web/shared/user"
	"forgejo.org/services/context"
	"forgejo.org/services/forms"
	project_service "forgejo.org/services/project"
)

const (
	tplProjects     base.TplName = "org/projects/list"
	tplProjectsNew  base.TplName = "org/projects/new"
	tplProjectsView base.TplName = "org/projects/view"
)

func getAndCheckProjectByID(ctx *context.Context, projectID int64) *project_model.Project {
	project, err := project_service.GetValidProjectByID(ctx, projectID, ctx.ContextUser.ID)
	if err != nil {
		if validation.IsErrNotValid(err) {
			ctx.NotFound("getAndCheckProjectByID", fmt.Errorf("Could not find project for Owner with ID %d", ctx.ContextUser.ID))
			return nil
		}
		ctx.NotFoundOrServerError("GetProjectByID", project_model.IsErrProjectNotExist, err)
		return nil
	}
	return project
}

// MustEnableProjects check if projects are enabled in settings
func MustEnableProjects(ctx *context.Context) {
	if unit.TypeProjects.UnitGlobalDisabled() {
		ctx.NotFound("EnableProjects", nil)
		return
	}
}

// Projects renders the home page of projects
func Projects(ctx *context.Context) {
	shared_user.PrepareContextForProfileBigAvatar(ctx)
	ctx.Data["Title"] = ctx.Tr("repo.projects")

	sortType := ctx.FormTrim("sort")

	showClosed := strings.EqualFold(ctx.FormTrim("state"), "closed")
	keyword := ctx.FormTrim("q")
	page := max(ctx.FormInt("page"), 1)

	projectType := project_service.GetAPIProjectType(ctx.ContextUser.IsOrganization(), false)
	opts := project_service.GetSearchOpts(
		ctx.ContextUser.ID,
		showClosed,
		sortType,
		keyword,
		projectType,
		page,
		setting.UI.IssuePagingNum)
	log.Debug("Got OwnerSearch Opts for user %v and project type %v", ctx.ContextUser.Name, projectType)
	projects, err := project_service.ListProjectsByOptions(*ctx, opts)
	if err != nil {
		ctx.ServerError("FindProjects", err)
		return
	}
	log.Debug("Found %v projects", len(projects))
	total, err := project_service.CountProjectsByOptions(*ctx, opts)
	if err != nil {
		ctx.ServerError("CountProjects", err)
		return
	}
	log.Debug("Counted %v projects", total)
	countOpts := project_service.GetSearchOpts(ctx.ContextUser.ID, !showClosed, "", "", projectType)
	opTotal, err := project_service.CountProjectsByOptions(*ctx, countOpts)
	if err != nil {
		ctx.ServerError("CountProjects", err)
		return
	}

	if showClosed {
		ctx.Data["OpenCount"] = opTotal
		ctx.Data["ClosedCount"] = total
		ctx.Data["State"] = "closed"
	} else {
		ctx.Data["OpenCount"] = total
		ctx.Data["ClosedCount"] = opTotal
		ctx.Data["State"] = "open"
	}

	ctx.Data["Projects"] = projects
	shared_user.RenderUserHeader(ctx)

	for _, project := range projects {
		project.RenderedContent = templates.RenderMarkdownToHtml(ctx, project.Description)
	}

	err = shared_user.LoadHeaderCount(ctx)
	if err != nil {
		ctx.ServerError("LoadHeaderCount", err)
		return
	}

	numPages := 0
	if total > 0 {
		numPages = (int(total) - 1/setting.UI.IssuePagingNum)
	}

	pager := context.NewPagination(int(total), setting.UI.IssuePagingNum, page, numPages)
	pager.AddParam(ctx, "state", "State")
	ctx.Data["Page"] = pager

	ctx.Data["CanWriteProjects"] = canWriteProjects(ctx)
	ctx.Data["IsShowClosed"] = showClosed
	ctx.Data["PageIsViewProjects"] = true
	ctx.Data["SortType"] = sortType

	numOpenIssues, err := issues_model.NumIssuesInProjects(ctx, projects, ctx.Doer, ctx.Org.Organization, optional.Some(false))
	if err != nil {
		ctx.ServerError("NumIssuesInProjects", err)
		return
	}
	numClosedIssues, err := issues_model.NumIssuesInProjects(ctx, projects, ctx.Doer, ctx.Org.Organization, optional.Some(true))
	if err != nil {
		ctx.ServerError("NumIssuesInProjects", err)
		return
	}
	ctx.Data["NumOpenIssuesInProject"] = numOpenIssues
	ctx.Data["NumClosedIssuesInProject"] = numClosedIssues

	ctx.HTML(http.StatusOK, tplProjects)
}

func canWriteProjects(ctx *context.Context) bool {
	if ctx.ContextUser.IsOrganization() {
		return ctx.Org.CanWriteUnit(ctx, unit.TypeProjects)
	}
	return ctx.Doer != nil && ctx.ContextUser.ID == ctx.Doer.ID
}

// RenderNewProject render creating a project page
func RenderNewProject(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("repo.projects.new")
	ctx.Data["TemplateConfigs"] = project_module.GetAPITemplateConfigs()
	ctx.Data["CardTypes"] = project_module.GetAPICardConfig()
	ctx.Data["CanWriteProjects"] = canWriteProjects(ctx)
	ctx.Data["PageIsViewProjects"] = true
	ctx.Data["HomeLink"] = ctx.ContextUser.HomeLink()
	ctx.Data["CancelLink"] = ctx.ContextUser.HomeLink() + "/-/projects"
	shared_user.RenderUserHeader(ctx)

	err := shared_user.LoadHeaderCount(ctx)
	if err != nil {
		ctx.ServerError("LoadHeaderCount", err)
		return
	}

	ctx.HTML(http.StatusOK, tplProjectsNew)
}

// CreateProject creates a new project
func CreateProject(ctx *context.Context) {
	form := web.GetForm(ctx).(*forms.CreateProjectForm)
	ctx.Data["Title"] = ctx.Tr("repo.projects.new")
	shared_user.RenderUserHeader(ctx)

	if ctx.HasError() {
		RenderNewProject(ctx)
		return
	}

	projectType := project_service.GetAPIProjectType(ctx.ContextUser.IsOrganization(), false)
	log.Debug("Got project type %v", projectType)

	opt := &project_structs.CreateProjectOptions{
		Title:        form.Title,
		Description:  form.Content,
		TemplateType: form.TemplateType,
		CardType:     form.CardType,
		Status:       "open",
	}
	project, err := project_service.NewProject(opt, ctx.ContextUser, nil, projectType)
	if err != nil {
		log.Error("Could not create project %v", form.Title)
		ctx.ServerError("NewProject", err)
		return
	}

	if err := project_service.CreateProject(ctx, project); err != nil {
		log.Error("Failed to create project.")
		ctx.ServerError("NewProject", err)
		return
	}
	log.Debug("Created project with name %v", form.Title)

	ctx.Flash.Success(ctx.Tr("repo.projects.create_success", form.Title))
	ctx.Redirect(ctx.ContextUser.HomeLink() + "/-/projects")
}

// ChangeProjectStatus updates the status of a project between "open" and "close"
func ChangeProjectStatus(ctx *context.Context) {
	var toClose bool
	switch ctx.Params(":action") {
	case "open":
		toClose = false
	case "close":
		toClose = true
	default:
		ctx.JSONRedirect(ctx.ContextUser.HomeLink() + "/-/projects")
		return
	}
	project := getAndCheckProjectByID(ctx, ctx.ParamsInt64(":id"))
	if ctx.Written() {
		return
	}
	if err := project_service.ChangeProjectStatus(ctx, project, toClose); err != nil {
		ctx.ServerError("ChangeProjectStatus", err)
		return
	}
	ctx.JSONRedirect(project_module.ProjectLinkForOrg(ctx.ContextUser.HomeLink(), ctx.ParamsInt64(":id")))
}

// DeleteProject delete a project
func DeleteProject(ctx *context.Context) {
	project := getAndCheckProjectByID(ctx, ctx.ParamsInt64(":id"))
	if ctx.Written() {
		return
	}

	if err := project_service.DeleteProjectByID(ctx, project.ID, optional.None[int64]()); err != nil {
		ctx.ServerError("DeleteProjectByID", err)
		return
	}
	ctx.Flash.Success(ctx.Tr("repo.projects.deletion_success"))

	ctx.JSONRedirect(ctx.ContextUser.HomeLink() + "/-/projects")
}

// RenderEditProject allows a project to be edited
func RenderEditProject(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("repo.projects.edit")
	ctx.Data["PageIsEditProjects"] = true
	ctx.Data["PageIsViewProjects"] = true
	ctx.Data["CanWriteProjects"] = canWriteProjects(ctx)
	ctx.Data["CardTypes"] = project_module.GetAPICardConfig()

	shared_user.RenderUserHeader(ctx)

	project := getAndCheckProjectByID(ctx, ctx.ParamsInt64(":id"))
	if ctx.Written() {
		return
	}

	ctx.Data["projectID"] = project.ID
	ctx.Data["title"] = project.Title
	ctx.Data["content"] = project.Description
	ctx.Data["redirect"] = ctx.FormString("redirect")
	ctx.Data["HomeLink"] = ctx.ContextUser.HomeLink()
	ctx.Data["card_type"] = project.CardType.ToAPICardType()
	ctx.Data["CancelLink"] = project_module.ProjectLinkForOrg(ctx.ContextUser.HomeLink(), project.ID)

	ctx.HTML(http.StatusOK, tplProjectsNew)
}

// EditProjectPost response for editing a project
func EditProjectPost(ctx *context.Context) {
	form := web.GetForm(ctx).(*forms.CreateProjectForm)
	projectID := ctx.ParamsInt64(":id")
	ctx.Data["Title"] = ctx.Tr("repo.projects.edit")
	ctx.Data["PageIsEditProjects"] = true
	ctx.Data["PageIsViewProjects"] = true
	ctx.Data["CanWriteProjects"] = canWriteProjects(ctx)
	ctx.Data["CardTypes"] = project_module.GetAPICardConfig()
	ctx.Data["CancelLink"] = project_module.ProjectLinkForOrg(ctx.ContextUser.HomeLink(), projectID)

	shared_user.RenderUserHeader(ctx)

	err := shared_user.LoadHeaderCount(ctx)
	if err != nil {
		ctx.ServerError("LoadHeaderCount", err)
		return
	}

	if ctx.HasError() {
		ctx.HTML(http.StatusOK, tplProjectsNew)
		return
	}

	project := getAndCheckProjectByID(ctx, ctx.ParamsInt64(":id"))
	if ctx.Written() {
		return
	}

	updated := &project_structs.CreateProjectOptions{
		Title:        form.Title,
		Description:  form.Content,
		TemplateType: form.TemplateType,
		CardType:     form.CardType,
		Status:       "open",
	}
	if err = project_service.UpdateProject(ctx, project, updated); err != nil {
		ctx.ServerError("UpdateProjects", err)
		return
	}

	ctx.Flash.Success(ctx.Tr("repo.projects.edit_success", project.Title))
	if ctx.FormString("redirect") == "project" {
		ctx.Redirect(project.Link(ctx))
	} else {
		ctx.Redirect(ctx.ContextUser.HomeLink() + "/-/projects")
	}
}

// ViewProject renders the project with board view for a project
func ViewProject(ctx *context.Context) {
	project := getAndCheckProjectByID(ctx, ctx.ParamsInt64(":id"))
	if ctx.Written() {
		return
	}

	columns, _, err := project_model.GetColumns(ctx, project.ID, db.ListOptionsAll)
	if err != nil {
		ctx.ServerError("GetProjectColumns", err)
		return
	}

	issuesMap, err := issues_model.LoadIssuesFromColumnList(ctx, columns, ctx.Doer, ctx.Org.Organization, optional.None[bool]())
	if err != nil {
		ctx.ServerError("LoadIssuesOfColumns", err)
		return
	}

	if project.CardType != project_module.CardTypeTextOnly {
		issuesAttachmentMap := make(map[int64][]*repo_model.Attachment)
		for _, issuesList := range issuesMap {
			for _, issue := range issuesList {
				if issueAttachment, err := repo_model.GetAttachmentsByIssueIDImagesLatest(ctx, issue.ID); err == nil {
					issuesAttachmentMap[issue.ID] = issueAttachment
				}
			}
		}
		ctx.Data["issuesAttachmentMap"] = issuesAttachmentMap
	}

	linkedPrsMap := make(map[int64][]*issues_model.Issue)
	for _, issuesList := range issuesMap {
		for _, issue := range issuesList {
			var referencedIDs []int64
			for _, comment := range issue.Comments {
				if comment.RefIssueID != 0 && comment.RefIsPull {
					referencedIDs = append(referencedIDs, comment.RefIssueID)
				}
			}

			if len(referencedIDs) > 0 {
				if linkedPrs, err := issues_model.Issues(ctx, &issues_model.IssuesOptions{
					IssueIDs:  referencedIDs,
					IsPull:    optional.Some(true),
					User:      ctx.Doer,
					AllPublic: !ctx.IsSigned,
				}); err == nil {
					linkedPrsMap[issue.ID] = linkedPrs
				}
			}
		}
	}

	project.RenderedContent = templates.RenderMarkdownToHtml(ctx, project.Description)
	ctx.Data["Title"] = project.Title
	ctx.Data["LinkedPRs"] = linkedPrsMap
	ctx.Data["PageIsViewProjects"] = true
	ctx.Data["CanWriteProjects"] = canWriteProjects(ctx)
	ctx.Data["Project"] = project
	ctx.Data["IssuesMap"] = issuesMap
	ctx.Data["Columns"] = columns
	shared_user.RenderUserHeader(ctx)

	err = shared_user.LoadHeaderCount(ctx)
	if err != nil {
		ctx.ServerError("LoadHeaderCount", err)
		return
	}

	ctx.HTML(http.StatusOK, tplProjectsView)
}

// DeleteProjectColumn allows for the deletion of a project column
func DeleteProjectColumn(ctx *context.Context) {
	if ctx.Doer == nil {
		ctx.JSON(http.StatusForbidden, map[string]string{
			"message": "Only signed in users are allowed to perform this action.",
		})
		return
	}

	project := getAndCheckProjectByID(ctx, ctx.ParamsInt64(":id"))
	if ctx.Written() {
		return
	}

	pb, err := project_service.GetColumnByID(ctx, ctx.ParamsInt64(":columnID"))
	if err != nil {
		ctx.ServerError("GetProjectColumn", err)
		return
	}
	if pb.ProjectID != ctx.ParamsInt64(":id") {
		ctx.JSON(http.StatusUnprocessableEntity, map[string]string{
			"message": fmt.Sprintf("ProjectColumn[%d] is not in Project[%d] as expected", pb.ID, project.ID),
		})
		return
	}

	if project.OwnerID != ctx.ContextUser.ID {
		ctx.JSON(http.StatusUnprocessableEntity, map[string]string{
			"message": fmt.Sprintf("ProjectColumn[%d] is not in Owner[%d] as expected", pb.ID, ctx.ContextUser.ID),
		})
		return
	}

	if err := project_service.DeleteColumnInProject(ctx, ctx.ParamsInt64(":columnID")); err != nil {
		ctx.ServerError("DeleteProjectColumnByID", err)
		return
	}

	ctx.JSONOK()
}

// CreateColumnInProject allows a new column to be added to a project.
func CreateColumnInProject(ctx *context.Context) {
	form := web.GetForm(ctx).(*forms.EditProjectColumnForm)

	project := getAndCheckProjectByID(ctx, ctx.ParamsInt64(":id"))
	if ctx.Written() {
		return
	}

	if err := project_service.CreateColumnInProject(ctx, &project_model.Column{
		ProjectID: project.ID,
		Title:     form.Title,
		Color:     form.Color,
		CreatorID: ctx.Doer.ID,
	}); err != nil {
		ctx.ServerError("CreateColumnInProject", err)
		return
	}

	ctx.JSONOK()
}

// CheckProjectColumnChangePermissions check permission
func CheckProjectColumnChangePermissions(ctx *context.Context) (*project_model.Project, *project_model.Column) {
	if ctx.Doer == nil {
		ctx.JSON(http.StatusForbidden, map[string]string{
			"message": "Only signed in users are allowed to perform this action.",
		})
		return nil, nil
	}

	project := getAndCheckProjectByID(ctx, ctx.ParamsInt64(":id"))
	if ctx.Written() {
		return nil, nil
	}

	column, err := project_service.GetColumnByID(ctx, ctx.ParamsInt64(":columnID"))
	if err != nil {
		ctx.ServerError("GetProjectColumn", err)
		return nil, nil
	}
	if column.ProjectID != ctx.ParamsInt64(":id") {
		ctx.JSON(http.StatusUnprocessableEntity, map[string]string{
			"message": fmt.Sprintf("ProjectColumn[%d] is not in Project[%d] as expected", column.ID, project.ID),
		})
		return nil, nil
	}
	return project, column
}

// EditProjectColumn allows a project column's to be updated
func EditProjectColumn(ctx *context.Context) {
	form := web.GetForm(ctx).(*forms.EditProjectColumnForm)
	_, column := CheckProjectColumnChangePermissions(ctx)
	if ctx.Written() {
		return
	}

	if form.Title != "" {
		column.Title = form.Title
	}
	column.Color = form.Color
	if form.Sorting != 0 {
		column.Sorting = form.Sorting
	}

	if err := project_service.EditColumnInProject(ctx, column); err != nil {
		ctx.ServerError("UpdateProjectColumn", err)
		return
	}

	ctx.JSONOK()
}

// SetDefaultProjectColumn set default column for uncategorized issues/pulls
func SetDefaultProjectColumn(ctx *context.Context) {
	project, column := CheckProjectColumnChangePermissions(ctx)
	if ctx.Written() {
		return
	}

	if err := project_service.SetDefaultColumn(ctx, project.ID, column.ID); err != nil {
		ctx.ServerError("SetDefaultColumn", err)
		return
	}

	ctx.JSONOK()
}

// MoveIssues moves or keeps issues in a column and sorts them inside that column
func MoveIssues(ctx *context.Context) {
	if ctx.Doer == nil {
		ctx.JSON(http.StatusForbidden, map[string]string{
			"message": "Only signed in users are allowed to perform this action.",
		})
		return
	}

	project := getAndCheckProjectByID(ctx, ctx.ParamsInt64(":id"))
	if ctx.Written() {
		return
	}

	column, err := project_service.GetColumnByID(ctx, ctx.ParamsInt64(":columnID"))
	if err != nil {
		ctx.NotFoundOrServerError("GetProjectColumn", project_model.IsErrProjectColumnNotExist, err)
		return
	}

	if column.ProjectID != project.ID {
		ctx.NotFound("ColumnNotInProject", nil)
		return
	}

	form := &project_structs.MovedIssuesOption{}
	if err = json.NewDecoder(ctx.Req.Body).Decode(&form); err != nil {
		ctx.ServerError("DecodeMovedIssuesForm", err)
		return
	}

	existingIssues, complete, err := project_service.GetIssues(ctx, form.GetIssueIDs())
	if err != nil {
		ctx.NotFoundOrServerError("GetIssueByID", issues_model.IsErrIssueNotExist, err)
		return
	}

	if !complete {
		ctx.Flash.Warning(ctx.Tr("project.missing_issue_connection"), true)
	}

	if err = project_service.ValidIssueID(ctx, project.OwnerID, existingIssues); err != nil {
		ctx.ServerError("LoadRepositories", err)
		return
	}

	if err = project_service.MoveIssuesOnProjectColumn(ctx, column, form); err != nil {
		ctx.ServerError("MoveIssuesOnProjectColumn", err)
		return
	}

	ctx.JSONOK()
}
