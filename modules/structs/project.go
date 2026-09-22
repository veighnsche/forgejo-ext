// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package structs

type ProjectIssue struct {
	ID              int64 `json:"id"`
	IssueID         int64 `json:"issue_id"`
	ProjectID       int64 `json:"project_id"`
	ProjectColumnID int64 `json:"project_column_id"`
	Sorting         int64 `json:"sorting"`
}

type ProjectColumn struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Default   bool   `json:"default"`
	Sorting   int8   `json:"sorting"`
	Color     string `json:"color"`
	ProjectID int64  `json:"project_id"`
}

type CreateOrUpdateProjectOptions struct {
	Title        string `json:"title"`
	Description  string `json:"description"`
	TemplateType string `json:"template_type"`
	CardType     string `json:"card_type"`
	Status       string `json:"status"`
}

type CreateProjectColumnOptions struct {
	Title   string `json:"title"`
	Default bool   `json:"default"`
	Sorting int8   `json:"sorting"`
	Color   string `json:"color"`
}

type CreateProjectIssueOptions struct {
	IssueID int64 `json:"issue_id"`
}

type UpdateProjectColumnIssueOptions struct {
	ProjectColumnID int64 `json:"project_column_id"`
	Sorting         int64 `json:"sorting"`
}

type Project struct {
	ID           int64  `json:"id"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	OwnerName    string `json:"owner_name"`
	RepoName     string `json:"repo_name"`
	Status       string `json:"status"`
	OwnerType    string `json:"project_type"`
	TemplateType string `json:"template_type"`
	CardType     string `json:"card_type"`
}

type MovedProjectIssue struct {
	IssueID int64 `json:"issueID"`
	Sorting int64 `json:"sorting"`
}

type MovedIssuesOption struct {
	ProjectIssues []MovedProjectIssue `json:"issues"`
}

func (m *MovedIssuesOption) GetIssueIDs() []int64 {
	issueIDs := make([]int64, 0, len(m.ProjectIssues))
	for _, issue := range m.ProjectIssues {
		issueIDs = append(issueIDs, issue.IssueID)
	}
	return issueIDs
}

func (m *MovedIssuesOption) GetSortingsMap() map[int64]int64 {
	sortedIssueIDs := make(map[int64]int64)
	for _, issue := range m.ProjectIssues {
		sortedIssueIDs[issue.Sorting] = issue.IssueID
	}
	return sortedIssueIDs
}
