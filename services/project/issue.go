// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package project

import (
	"context"
	"errors"
	"fmt"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	project_model "forgejo.org/models/project"
	user_model "forgejo.org/models/user"
	project_types "forgejo.org/modules/structs"
	"forgejo.org/modules/validation"
)

// ValidIssueID checks if the IDs of the given issue list are valid
func ValidIssueID(ctx context.Context, ownerID int64, issues issues_model.IssueList) error {
	if _, err := issues.LoadRepositories(ctx); err != nil {
		return fmt.Errorf("Got database error: %v", err.Error())
	}
	for _, issue := range issues {
		if issue.Repo.OwnerID != ownerID {
			return errors.New("some issue's ownerID is not equal to project's ownerID")
		}
	}
	return nil
}

// getProjectIssueByID Gets a single ProjectIssue by its ID
func getProjectIssueByID(ctx context.Context, issueID int64) (*project_model.ProjectIssue, error) {
	issue, err := project_model.GetProjectIssue(ctx, issueID)
	if err != nil {
		return nil, fmt.Errorf("Got database error: %v", err.Error())
	}
	return issue, nil
}

// GetValidProjectIssueByID Gets a single ProjectIssue by its ID
// And makes sure the ID is not zero and the ProjectID and ColumnID match for that issue
func GetValidProjectIssueByID(ctx context.Context, projectID, columnID, issueID int64) (*project_model.ProjectIssue, error) {
	if issueID == int64(0) {
		return nil, validation.ErrNotValid{
			Message: "IssueID must not be empty",
		}
	}
	i, err := getProjectIssueByID(ctx, issueID)
	if err != nil {
		return nil, err
	}
	if i.ProjectID != projectID || i.ProjectColumnID != columnID {
		return nil, validation.ErrNotValid{
			Message: fmt.Sprintf("Issue with ID %v did not belong to Project with ID %v or Column with ID %v",
				issueID, projectID, columnID),
		}
	}
	return i, nil
}

// ListProjectIssues Gets a list of ProjectIssues for a projectID, also returns the total count in that list
func ListProjectIssues(ctx context.Context, projectID int64, listOptions db.ListOptions) ([]*project_model.ProjectIssue, int64, error) {
	issues, total, err := project_model.GetProjectIssues(ctx, projectID, listOptions)
	if err != nil {
		return nil, 0, fmt.Errorf("Got database error: %v", err.Error())
	}
	return issues, total, nil
}

// CreateIssueInProject Create a ProjectIssue in a Project in the column with the given ID
// If columnID is 0, adds to the DefaultColumn
func CreateIssueInProject(ctx context.Context, issue *issues_model.Issue, doer *user_model.User, projectID, colID int64) (*project_model.ProjectIssue, error) {
	projIssue := &project_model.ProjectIssue{
		ProjectID:       projectID,
		ProjectColumnID: colID,
	}
	// CreateProjectIssue checks if the colID is 0 and then assigns to defaultCol
	if err := issues_model.CreateProjectIssue(ctx, issue, doer, projIssue); err != nil {
		return nil, fmt.Errorf("Got database error: %v", err.Error())
	}
	return projIssue, nil
}

// GetIssues Gets an issue list by IssueIDs and checks for completeness, returns false if not complete
func GetIssues(ctx context.Context, issueIDs []int64) (issues_model.IssueList, bool, error) {
	issues, err := issues_model.GetIssuesByIDs(ctx, issueIDs, true)
	if err != nil {
		return nil, false, fmt.Errorf("ListMissingIssues %v", err.Error())
	}
	complete := len(issues) == len(issueIDs)
	return issues, complete, nil
}

// MoveIssuesOnProjectColumn Allows moving Issues between Columns or to change the sorting within Columns
func MoveIssuesOnProjectColumn(ctx context.Context, column *project_model.Column, projectIssues *project_types.MovedIssuesOption) error {
	sortedIssueIDs := projectIssues.GetSortingsMap()
	err := project_model.MoveIssuesOnProjectColumn(ctx, column, sortedIssueIDs)
	if err != nil {
		return fmt.Errorf("Got database error: %v", err.Error())
	}
	return nil
}

// RemoveIssueFromProject Removes a ProjectIssue from a Project
func RemoveIssueFromProject(ctx context.Context, issue *issues_model.Issue, doer *user_model.User, columnID int64) error {
	return issues_model.IssueAssignOrRemoveProject(ctx, issue, doer, 0, columnID)
}
