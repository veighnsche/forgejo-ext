// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package project

import (
	"context"
	"fmt"

	"forgejo.org/models/db"
	project_model "forgejo.org/models/project"
	project_module "forgejo.org/modules/project"
	project_structs "forgejo.org/modules/structs"
	"forgejo.org/modules/validation"
)

func NewColumn(form *project_structs.CreateProjectColumnOptions, projectID int64) *project_model.Column {
	return &project_model.Column{
		Title:     form.Title,
		Default:   form.Default,
		Sorting:   form.Sorting,
		Color:     form.Color,
		ProjectID: projectID,
	}
}

func getColumnByID(ctx context.Context, columnID int64) (*project_model.Column, error) {
	column, err := project_model.GetColumn(ctx, columnID)
	if err != nil {
		return nil, fmt.Errorf("failed to get column %d: %w", columnID, err)
	}
	return column, nil
}

// GetValidProjectColumnByID Get a Column by its ID, validate ID != 0 and check if projectIDs match
func GetValidProjectColumnByID(ctx context.Context, projectID, columnID int64) (*project_model.Column, error) {
	if columnID == int64(0) {
		return nil, validation.ErrNotValid{
			Message: "column ID must not be empty",
		}
	}
	c, err := getColumnByID(ctx, columnID)
	if err != nil {
		return nil, fmt.Errorf("could not get column for project %d: %w", projectID, err)
	}
	if c.ProjectID != projectID {
		return nil, project_module.ErrMismatchedID{
			Message: fmt.Sprintf("column %d did not belong to project %d", columnID, projectID),
		}
	}
	return c, nil
}

// CreateColumnInProject Create a ProjectColumn in a Project
func CreateColumnInProject(ctx context.Context, col *project_model.Column) error {
	err := project_model.CreateColumn(ctx, col)
	if err != nil {
		return fmt.Errorf("could not create column for project %d: %w", col.ProjectID, err)
	}
	return nil
}

// EditColumnInProject Update the title or color of a ProjectColumn
func EditColumnInProject(ctx context.Context, col *project_model.Column) error {
	err := project_model.UpdateColumn(ctx, col)
	if err != nil {
		return fmt.Errorf("could not edit column for project %d: %w", col.ProjectID, err)
	}
	return nil
}

// UpdateColumnInProject allow full updates of the column, including default and sorting
func UpdateColumnInProject(ctx context.Context, col *project_model.Column, form *project_structs.CreateProjectColumnOptions, projectID, columnID int64) error {
	changed := false
	if form.Title != "" && form.Title != col.Title {
		col.Title = form.Title
		changed = true
	}
	if form.Color != "" && form.Color != col.Color {
		col.Color = form.Color
		changed = true
	}
	if form.Default && !col.Default {
		if err := SetDefaultColumn(ctx, projectID, columnID); err != nil {
			return err
		}
	}
	if form.Sorting != col.Sorting {
		changed = true
		cols, err := db.Find[project_model.Column](ctx, project_model.FindColumnOptions{
			ListOptions: db.ListOptionsAll, ProjectID: projectID,
		})
		if err != nil {
			return err
		}
		sorting := make(map[int64]int64, 0)
		for _, subCol := range cols {
			if subCol.ID == col.ID {
				// move column to new sorting position
				sorting[int64(form.Sorting)] = subCol.ID
				continue
			}
			if subCol.Sorting >= form.Sorting {
				// move following columns to their new sorting positions
				sorting[int64(subCol.Sorting)+1] = subCol.ID
			}
		}
		err = project_model.MoveColumnsOnProject(ctx, projectID, sorting)
		if err != nil {
			return err
		}
		col.Sorting = form.Sorting // UpdateColumn below sets Sorting again, make sure it's up to date
	}
	if !changed {
		// no further changes necessary
		return nil
	}
	err := project_model.UpdateColumn(ctx, col)
	if err != nil {
		return fmt.Errorf("could not edit column for project %d: %w", col.ProjectID, err)
	}
	return nil
}

// SetDefaultColumn Set the default Column of a Project, other Columns will then be set non default
func SetDefaultColumn(ctx context.Context, projectID, columnID int64) error {
	return project_model.SetDefaultColumn(ctx, projectID, columnID)
}

// DeleteColumnInProject Delete a Column from a Project
func DeleteColumnInProject(ctx context.Context, columnID int64) error {
	err := project_model.DeleteColumnByID(ctx, columnID)
	if err != nil {
		return fmt.Errorf("could not delete column %d: %w", columnID, err)
	}
	return nil
}
