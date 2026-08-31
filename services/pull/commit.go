package pull

import (
	"context"
	"slices"

	issues_model "forgejo.org/models/issues"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/container"
	"forgejo.org/modules/git"
	"forgejo.org/modules/references"
	repo_module "forgejo.org/modules/repository"
)

// MergePullCommit checks if pull requests are closed by commit message with `merges #ID`
func MergePullCommit(ctx context.Context, doer *user_model.User, repo *repo_model.Repository, commits []*repo_module.PushCommit, branchName string) error {
	for _, c := range slices.Backward(commits) {
		type markKey struct {
			ID     int64
			Action references.XRefAction
		}

		refMarked := make(container.Set[markKey])
		var pr *issues_model.PullRequest
		var err error
		for _, ref := range references.FindAllIssueReferences(c.Message) {
			if pr, err = getPullFromRef(ctx, repo, ref.Index); err != nil {
				if issues_model.IsErrPullRequestNotExist(err) {
					continue
				}
				return err
			}

			key := markKey{ID: pr.ID, Action: ref.Action}
			if !refMarked.Add(key) {
				continue
			}

			perm, err := access_model.GetUserRepoPermission(ctx, repo, doer)
			if err != nil {
				return err
			}

			canmerge := perm.IsAdmin() || perm.IsOwner() || !perm.CanWriteIssuesOrPulls(true)

			if !canmerge {
				continue
			}

			if ref.Action != references.XRefActionMerges {
				continue
			}

			baseGitRepo, err := git.OpenRepository(ctx, repo.RepoPath())
			if err != nil {
				return err
			}
			if err := MergedManually(ctx, pr, doer, baseGitRepo, c.Sha1); err != nil {
				return err
			}
		}
	}

	return nil
}

// getPullFromRef returns the pull request referenced by a ref.
func getPullFromRef(ctx context.Context, repo *repo_model.Repository, index int64) (*issues_model.PullRequest, error) {
	pr, err := issues_model.GetPullRequestByIndex(ctx, repo.ID, index)
	if err != nil {
		return nil, err
	}
	return pr, nil
}
