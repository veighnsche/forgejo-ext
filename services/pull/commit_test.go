package pull

import (
	"testing"

	"forgejo.org/models"
	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/repository"

	"github.com/stretchr/testify/require"
)

func TestMergePullCommit(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	pushCommits := []*repository.PushCommit{
		{
			Sha1:           "abcdef1",
			CommitterEmail: "user2@example.com",
			CommitterName:  "User Two",
			AuthorEmail:    "user4@example.com",
			AuthorName:     "User Four",
			Message:        "start working on #FST-1, #1",
		},
		{
			Sha1:           "abcdef2",
			CommitterEmail: "user2@example.com",
			CommitterName:  "User Two",
			AuthorEmail:    "user2@example.com",
			AuthorName:     "User Two",
			Message:        "a plain message",
		},
		{
			Sha1:           "abcdef2",
			CommitterEmail: "user2@example.com",
			CommitterName:  "User Two",
			AuthorEmail:    "user2@example.com",
			AuthorName:     "User Two",
			Message:        "merge #3",
		},
	}

	user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2}, unittest.Cond("has_merged = ?", false))
	repo.Owner = user
	unit, err := repo.GetUnit(db.DefaultContext, unit.TypePullRequests)
	require.NoError(t, err)

	unit.Config = &repo_model.PullRequestsConfig{
		AllowManualMerge: true,
	}
	require.NoError(t, repo_model.UpdateRepoUnit(t.Context(), unit))

	require.NoError(t, repo.GetBaseRepo(db.DefaultContext))

	require.NoError(t, MergePullCommit(db.DefaultContext, user, repo, pushCommits, repo.DefaultBranch))
	unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2}, unittest.Cond("has_merged = ?", true))
}

func TestMergePullCommit_WrongBaseBranch(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	pushCommits := []*repository.PushCommit{
		{
			Sha1:           "abcdef2",
			CommitterEmail: "user2@example.com",
			CommitterName:  "User Two",
			AuthorEmail:    "user2@example.com",
			AuthorName:     "User Two",
			Message:        "merge #5",
		},
	}

	user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 5}, unittest.Cond("has_merged = ?", false))
	repo.Owner = user
	unit, err := repo.GetUnit(db.DefaultContext, unit.TypePullRequests)
	require.NoError(t, err)

	unit.Config = &repo_model.PullRequestsConfig{
		AllowManualMerge: true,
	}

	require.NoError(t, repo_model.UpdateRepoUnit(t.Context(), unit))
	require.NoError(t, repo.GetBaseRepo(db.DefaultContext))

	require.NoError(t, MergePullCommit(db.DefaultContext, user, repo, pushCommits, repo.DefaultBranch))
	unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2}, unittest.Cond("has_merged = ?", false))
}

func TestMergePullCommit_HasMerged(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	pushCommits := []*repository.PushCommit{
		{
			Sha1:           "abcdef2",
			CommitterEmail: "user2@example.com",
			CommitterName:  "User Two",
			AuthorEmail:    "user2@example.com",
			AuthorName:     "User Two",
			Message:        "merge #2",
		},
	}

	user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 1}, unittest.Cond("has_merged = ?", true))
	repo.Owner = user
	unit, err := repo.GetUnit(db.DefaultContext, unit.TypePullRequests)
	require.NoError(t, err)

	unit.Config = &repo_model.PullRequestsConfig{
		AllowManualMerge: true,
	}

	require.NoError(t, repo_model.UpdateRepoUnit(t.Context(), unit))
	require.NoError(t, repo.GetBaseRepo(db.DefaultContext))

	require.NoError(t, MergePullCommit(db.DefaultContext, user, repo, pushCommits, repo.DefaultBranch))
	unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 1}, unittest.Cond("has_merged = ?", true))
}

func TestMergePullCommit_ManualMergeNotAllowed(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	pushCommits := []*repository.PushCommit{
		{
			Sha1:           "abcdef2",
			CommitterEmail: "user2@example.com",
			CommitterName:  "User Two",
			AuthorEmail:    "user2@example.com",
			AuthorName:     "User Two",
			Message:        "merge #3; should not be allowed",
		},
	}
	user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2}, unittest.Cond("has_merged = ?", false))
	repo.Owner = user
	unit, err := repo.GetUnit(db.DefaultContext, unit.TypePullRequests)
	require.NoError(t, err)

	require.NoError(t, repo_model.UpdateRepoUnit(t.Context(), unit))

	require.NoError(t, repo.GetBaseRepo(db.DefaultContext))

	unit.Config = &repo_model.PullRequestsConfig{
		AllowManualMerge: false,
	}

	require.ErrorIs(t, MergePullCommit(db.DefaultContext, user, repo, pushCommits, repo.DefaultBranch), models.ErrInvalidMergeStyle{ID: repo.ID, Style: repo_model.MergeStyleManuallyMerged})
}
