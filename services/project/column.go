// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package project

import (
	"context"

	"forgejo.org/models/db"
	project_model "forgejo.org/models/project"
	"forgejo.org/modules/validation"
)

// ListProjectColumns Fetches a list of ProjectColumns and also returns their total count
func ListProjectColumns(ctx context.Context, projectID int64, listOptions db.ListOptions) ([]*project_model.Column, int64, error) {
	columns, total, err := project_model.GetColumns(ctx, projectID, listOptions)
	if err != nil {
		return nil, 0, err
	}
	return columns, total, nil
}

// getColumnByID Get a ProjectColumn by its ID
func getColumnByID(ctx context.Context, columnID int64) (*project_model.Column, error) {
	column, err := project_model.GetColumn(ctx, columnID)
	if err != nil {
		return nil, err
	}
	return column, nil
}

// GetColumnByID Get a Column by its ID
func GetValidProjectColumnByID(ctx context.Context, projectID, columnID int64) (*project_model.Column, error) {
	if columnID == int64(0) {
		return nil, validation.ErrNotValid{
			Message: "Column ID must not be empty",
		}
	}
	c, err := getColumnByID(ctx, columnID)
	if err != nil {
		return nil, err
	}
	if c.ProjectID != projectID {
		return nil, project_model.ErrProjectColumnNotExist{ColumnID: c.ID}
	}
	return c, nil
}

// CreateColumnInProject Create a column in a project
func CreateColumnInProject(ctx context.Context, col *project_model.Column) error {
	err := project_model.CreateColumn(ctx, col)
	if err != nil {
		return err
	}
	return nil
}

// EditColumnInProject Update title or color in a project
func EditColumnInProject(ctx context.Context, col *project_model.Column) error {
	err := project_model.UpdateColumn(ctx, col)
	if err != nil {
		return err
	}
	return nil
}

// SetDefaultColumn Set the default column of a project, other columns will then be set non default
func SetDefaultColumn(ctx context.Context, projectID, columnID int64) error {
	return project_model.SetDefaultColumn(ctx, projectID, columnID)
}

// DeleteColumnInProject Delete Column from a project
func DeleteColumnInProject(ctx context.Context, columnID int64) error {
	err := project_model.DeleteColumnByID(ctx, columnID)
	if err != nil {
		return err
	}
	return nil
}
