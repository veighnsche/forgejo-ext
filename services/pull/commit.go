package pull

import (
	"context"
	"slices"

	"forgejo.org/models"
	issues_model "forgejo.org/models/issues"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/container"
	"forgejo.org/modules/log"
	"forgejo.org/modules/references"
	repo_module "forgejo.org/modules/repository"
	"forgejo.org/modules/timeutil"
	notify_service "forgejo.org/services/notify"
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
			if ref.Action != references.XRefActionMerges {
				continue
			}

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

			if err := pr.LoadBaseRepo(ctx); err != nil {
				return err
			}

			if pr.HasMerged {
				continue
			}

			// Check if pull is targeting the correct branch
			if pr.BaseBranch != branchName {
				continue
			}

			prUnit, err := pr.BaseRepo.GetUnit(ctx, unit.TypePullRequests)
			if err != nil {
				return err
			}
			prConfig := prUnit.PullRequestsConfig()

			// Check if merge style is correct and allowed
			if !prConfig.IsMergeStyleAllowed(repo_model.MergeStyleManuallyMerged) {
				return models.ErrInvalidMergeStyle{ID: pr.BaseRepo.ID, Style: repo_model.MergeStyleManuallyMerged}
			}

			perm, err := access_model.GetUserRepoPermission(ctx, repo, doer)
			if err != nil {
				return err
			}
			if !perm.IsAdmin() && !perm.IsOwner() && !perm.CanWriteIssuesOrPulls(true) {
				continue
			}

			pr.MergedCommitID = c.Sha1
			pr.MergedUnix = timeutil.TimeStamp(c.Timestamp.Unix())
			pr.Status = issues_model.PullRequestStatusManuallyMerged
			pr.Merger = doer
			pr.MergerID = doer.ID

			var merged bool
			if merged, err = pr.SetMerged(ctx); err != nil {
				return err
			} else if !merged {
				log.Info("manuallyMerged[%d]: Failed to mark as manually merged into %s/%s by commit id: %s", pr.ID, pr.BaseRepo.Name, pr.BaseBranch, c.Sha1)
			}

			notify_service.MergePullRequest(ctx, doer, pr)
			log.Info("manuallyMerged[%d]: Marked as manually merged into %s/%s by commit id: %s", pr.ID, pr.BaseRepo.Name, pr.BaseBranch, c.Sha1)

			return handleCloseCrossReferences(ctx, pr, doer)
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
