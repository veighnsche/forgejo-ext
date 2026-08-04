// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package project

import (
	"context"

	"forgejo.org/models/db"
	project_model "forgejo.org/models/project"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/optional"
	project_module "forgejo.org/modules/project"
	project_structs "forgejo.org/modules/structs"
	validation "forgejo.org/modules/validation"
)

func getBasicSearchOpts(isShowClosed bool, sortType, keyword string, projectType project_module.APIOwnerType, pageOpts ...int) *project_model.SearchOptions {
	opts := &project_model.SearchOptions{
		IsClosed: optional.Some(isShowClosed),
		Type:     projectType.ToOwnerType(),
	}
	opts.OrderBy = project_model.GetSearchOrderBySortType(sortType)
	if keyword != "" {
		opts.Title = keyword
	}
	if len(pageOpts) > 1 {
		opts.ListOptions = db.ListOptions{
			Page:     pageOpts[0],
			PageSize: pageOpts[1],
		}
	}
	return opts
}

func GetAPIProjectType(isOrg, isRepo bool) project_module.APIOwnerType {
	var t project_module.APIOwnerType
	if isOrg {
		t = project_module.APIOwnerTypeOrganization
	} else if isRepo {
		t = project_module.APIOwnerTypeRepository
	} else {
		t = project_module.APIOwnerTypeIndividual
	}
	return t
}

// Returns a valid project or fails with validation error if invalid
func NewProject(
	form *project_structs.CreateProjectOptions,
	owner *user_model.User,
	repo *repo_model.Repository,
	projectType project_module.APIOwnerType,
) (*project_model.Project, error) {
	var err error

	projectTemplateType := project_module.APITemplateType(form.TemplateType)
	valid, err := validation.IsValid(projectTemplateType)
	if !valid {
		return nil, err
	}

	projectCardType := project_module.APICardType(form.CardType)
	valid, err = validation.IsValid(projectCardType)
	if !valid {
		return nil, err
	}

	valid, err = validation.IsValid(projectType)
	if !valid {
		return nil, err
	}

	res := &project_model.Project{
		Title:        form.Title,
		Description:  form.Description,
		Owner:        owner,
		OwnerID:      owner.ID,
		TemplateType: projectTemplateType.ToTemplateType(),
		CardType:     projectCardType.ToCardType(),
		Type:         projectType.ToOwnerType(),
	}

	errNotValid := validation.ErrNotValid{}
	switch projectType {
	case project_module.APIOwnerTypeIndividual:
		if owner.IsOrganization() {
			errNotValid.Message = "Type was TypeIndividual, but owner was org"
		} else if repo != nil {
			errNotValid.Message = "Type was TypeIndividual, repo was given"
		}
	case project_module.APIOwnerTypeOrganization:
		if owner.IsIndividual() {
			errNotValid.Message = "Type was TypeOrganization, but owner was individual"
		} else if repo != nil {
			errNotValid.Message = "Type was TypeOrganization, repo was given"
		}
	case project_module.APIOwnerTypeRepository:
		if repo != nil {
			res.Repo = repo
			res.RepoID = repo.ID
		} else {
			errNotValid.Message = "Repo type given, but repo struct was empty"
		}
	}

	if errNotValid.Message != "" {
		return nil, errNotValid
	}

	return res, nil
}

// GetSearchOpts returns search options for user, org or repo depending on the projectType
func GetSearchOpts(id int64, isShowClosed bool, sortType, keyword string, projectType project_module.APIOwnerType, pageOpts ...int) *project_model.SearchOptions {
	opts := getBasicSearchOpts(isShowClosed, sortType, keyword, projectType, pageOpts...)
	if projectType == project_module.APIOwnerTypeRepository {
		opts.RepoID = id
	} else {
		opts.OwnerID = id
	}
	return opts
}

func GetValidProjectByID(ctx context.Context, projectID, ownerID int64) (*project_model.Project, error) {
	project, err := project_model.GetProjectByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	errNotValid := validation.ErrNotValid{Message: "Project did not belong to given owner"}
	switch project.Type {
	case project_module.TypeIndividual:
		if project.OwnerID != ownerID {
			return nil, errNotValid
		}
	case project_module.TypeOrganization:
		if project.OwnerID != ownerID {
			return nil, errNotValid
		}
	case project_module.TypeRepository:
		if project.RepoID != ownerID {
			return nil, errNotValid
		}
	}
	return project, nil
}

func ListProjectsByOptions(ctx context.Context, opts *project_model.SearchOptions) ([]*project_model.Project, error) {
	projects, err := db.Find[project_model.Project](ctx, opts)
	return projects, err
}

func CountProjectsByOptions(ctx context.Context, opts *project_model.SearchOptions) (int64, error) {
	count, err := db.Count[project_model.Project](ctx, opts)
	return count, err
}

// Write
// CreateProject Expects a valid project and creates it in DB
func CreateProject(ctx context.Context, project *project_model.Project) error {
	err := project_model.CreateProject(ctx, project)
	if err != nil {
		return err
	}
	return nil
}

// UpdateProject Update Project in DB
func UpdateProject(ctx context.Context, project *project_model.Project, updated *project_structs.CreateProjectOptions) error {
	if updated.Title != "" {
		project.Title = updated.Title
	}

	if updated.Description != "" {
		project.Description = updated.Description
	}

	if updated.CardType != "" {
		projectCardType := project_module.APICardType(updated.CardType)
		valid, err := validation.IsValid(projectCardType)
		if !valid {
			return err
		}
		project.CardType = projectCardType.ToCardType()
	}

	if err := project_model.UpdateProject(ctx, project); err != nil {
		return err
	}

	if updated.Status != "" {
		projectStatus := project_module.APIStatus(updated.Status)
		valid, err := validation.IsValid(projectStatus)
		if !valid {
			return err
		}
		if project.IsClosed != projectStatus.IsClosed() {
			if err := ChangeProjectStatus(ctx, project, projectStatus.IsClosed()); err != nil {
				return err
			}
		}
	}
	return nil
}

// DeleteProjectByID Delete Project from DB
func DeleteProjectByID(ctx context.Context, projectID int64, repoID optional.Option[int64]) error {
	err := project_model.DeleteProjectByID(ctx, projectID, repoID)
	if err != nil {
		return err
	}
	return nil
}

// ChangeProjectStatus Change status to closed or open
func ChangeProjectStatus(ctx context.Context, project *project_model.Project, close bool) error {
	return project_model.ChangeProjectStatus(ctx, project, close)
}
