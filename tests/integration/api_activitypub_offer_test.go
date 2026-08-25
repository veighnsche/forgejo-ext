// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/activitypub"
	"forgejo.org/modules/git"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/routers"
	"forgejo.org/services/contexttest"
	"forgejo.org/services/federation"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestActivityPubRepositoryInboxOffer verifies the federated pull request
// flow: a remote user offers a MergeRequest against a local repository; the
// proposed branch is fetched via git and a local pull request is created with
// the remote user as author.
func TestActivityPubRepositoryInboxOffer(t *testing.T) {
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	mockFederationAllowAllHosts(t)
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	federation.Init()

	mock := test.NewFederationServerMock()
	federatedSrv := mock.DistantServer(t)
	defer federatedSrv.Close()

	onApplicationRun(t, func(t *testing.T, u *url.URL) {
		repositoryID := int64(1) // public repository: federation must not expose private ones
		localRepo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: repositoryID})
		localRepoActor := u.JoinPath(fmt.Sprintf("/api/v1/activitypub/repository-id/%d", repositoryID)).String()
		localRepoInbox := u.JoinPath(fmt.Sprintf("/api/v1/activitypub/repository-id/%d/inbox", repositoryID)).String()

		// Create the "source" branch in the local repo. The fixture repos are
		// bare, so the branch is created with update-ref.
		{
			_, _, gerr := git.NewCommand(git.DefaultContext, "update-ref").AddDynamicArguments("refs/heads/feature-x", "refs/heads/master").RunStdString(&git.RunOpts{Dir: localRepo.RepoPath()})
			require.NoError(t, gerr)
			// Refresh the dumb HTTP server's ref advertisement.
			_, _, gerr = git.NewCommand(git.DefaultContext, "update-server-info").RunStdString(&git.RunOpts{Dir: localRepo.RepoPath()})
			require.NoError(t, gerr)
		}

		// Serve the local repo over dumb HTTP so the receiving side can fetch
		// the proposed branch anonymously (the Forgejo test server requires
		// git HTTP auth).
		gitServer := httptest.NewServer(http.FileServer(http.Dir(localRepo.RepoPath())))
		defer gitServer.Close()
		gitURL := gitServer.URL

		ctx, _ := contexttest.MockAPIContext(t, localRepoInbox)
		cf, err := activitypub.NewClientFactoryWithTimeout(60 * time.Second)
		require.NoError(t, err)

		c, err := cf.WithKeysDirect(ctx, mock.Persons[0].PrivKey,
			mock.Persons[0].KeyID(federatedSrv.URL), nil)
		require.NoError(t, err)

		// Remote user 15 offers a MergeRequest targeting the local repo.
		offerActivity := fmt.Appendf(nil,
			`{"type":"Offer",`+
				`"actor":"%s/api/v1/activitypub/user-id/15",`+
				`"target":"%s",`+
				`"object":{"type":"MergeRequest",`+
				`"id":"%s/merge-requests/1",`+
				`"source":"%s",`+
				`"sourceGitURL":"%s",`+
				`"sourceBranch":"feature-x",`+
				`"ref":"master",`+
				`"name":"Federated pull request",`+
				`"content":"Proposed from a distant instance"}}`,
			federatedSrv.URL, localRepoActor, federatedSrv.URL, localRepoActor, gitURL)
		resp, err := c.Post(offerActivity, localRepoInbox)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)

		// The remote author is materialised.
		distantFederatedUser := unittest.AssertExistsAndLoadBean(t, &user_model.FederatedUser{ExternalID: "15"})

		// A pull request must exist: an IsPull issue in repo 2, posted by the
		// remote user, with the federated head branch against master.
		issue := &issues_model.Issue{
			RepoID:   repositoryID,
			PosterID: distantFederatedUser.UserID,
			IsPull:   true,
		}
		has, err := db.GetEngine(ctx).Get(issue)
		require.NoError(t, err)
		require.True(t, has, "expected a federated pull request to be created")
		assert.Equal(t, "Federated pull request", issue.Title)

		pull := &issues_model.PullRequest{IssueID: issue.ID, BaseRepoID: repositoryID}
		has, err = db.GetEngine(ctx).Get(pull)
		require.NoError(t, err)
		require.True(t, has, "expected a pull request record")
		assert.Equal(t, "master", pull.BaseBranch)
		assert.Greater(t, len(pull.HeadBranch), len("federated/"), "head branch must be the fetched federated branch")
	})
}
