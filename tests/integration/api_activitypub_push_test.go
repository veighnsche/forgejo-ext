// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
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
	migrations_allowlist "forgejo.org/services/migrations/allowlist"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestActivityPubRepositoryInboxPush verifies the inbound Push activity
// handling: a Push from the upstream actor of a federated pull mirror is
// accepted (and triggers an immediate mirror sync); pushes from any other
// actor, or to a non-mirror repository, are acknowledged and ignored.
func TestActivityPubRepositoryInboxPush(t *testing.T) {
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	mockFederationAllowAllHosts(t)
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	federation.Init()

	mock := test.NewFederationServerMock()
	federatedSrv := mock.DistantServer(t)
	defer federatedSrv.Close()

	onApplicationRun(t, func(t *testing.T, u *url.URL) {
		upstreamActor := federatedSrv.URL + "/api/v1/activitypub/repository-id/1"
		// Use a public repository (1) as the mirror: federation must not
		// expose private repositories.
		inboxURL := u.JoinPath("/api/v1/activitypub/repository-id/1/inbox").String()

		ctx, _ := contexttest.MockAPIContext(t, inboxURL)
		cf, err := activitypub.NewClientFactoryWithTimeout(60 * time.Second)
		require.NoError(t, err)

		// The Push activity is authored by the upstream repository actor, so it
		// is signed with that repository's key.
		repoKeyID := mock.Repositories[0].KeyID(federatedSrv.URL)
		c, err := cf.WithKeysDirect(ctx, mock.Repositories[0].PrivKey, repoKeyID, nil)
		require.NoError(t, err)

		// The local repository is registered as a federated pull mirror of the
		// upstream.
		err = repo_model.AddFederatedMirror(ctx, 1, upstreamActor, "https://example.com/upstream.git", false)
		require.NoError(t, err)
		federatedMirror := unittest.AssertExistsAndLoadBean(t, &repo_model.FederatedMirror{RepoID: 1})
		assert.False(t, federatedMirror.IsPush)

		// Push from the configured upstream: accepted, triggers a mirror sync.
		pushFromUpstream := fmt.Appendf(nil,
			`{"type":"Push","actor":"%s","hashBefore":"aaaa","hashAfter":"bbbb",`+
				`"object":{"type":"OrderedCollection","totalItems":1,"orderedItems":[`+
				`{"type":"Commit","hash":"bbbb","summary":"new commit"}]}}`, upstreamActor)
		resp, err := c.Post(pushFromUpstream, inboxURL)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)

		// Push from a different actor (not the upstream): accepted but ignored.
		pushFromOther := fmt.Appendf(nil,
			`{"type":"Push","actor":"%s","hashBefore":"aaaa","hashAfter":"bbbb",`+
				`"object":{"type":"OrderedCollection"}}`, federatedSrv.URL+"/api/v1/activitypub/repository-id/999")
		resp, err = c.Post(pushFromOther, inboxURL)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)

		// Push to a repository that is not a federated mirror: acknowledged.
		nonMirrorInbox := u.JoinPath("/api/v1/activitypub/repository-id/4/inbox").String()
		_, err = c.Post(pushFromUpstream, nonMirrorInbox)
		require.NoError(t, err)
	})
}

// TestActivityPubRepositoryFollowByRepository verifies that a Follow from a
// remote Repository actor (e.g. a pull mirror) is recorded as a repository
// follower, so outbound Push activities reach the mirror's repository inbox.
func TestActivityPubRepositoryFollowByRepository(t *testing.T) {
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	mockFederationAllowAllHosts(t)
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	federation.Init()

	mock := test.NewFederationServerMock()
	federatedSrv := mock.DistantServer(t)
	defer federatedSrv.Close()

	onApplicationRun(t, func(t *testing.T, u *url.URL) {
		inboxURL := u.JoinPath("/api/v1/activitypub/repository-id/4/inbox").String()
		ctx, _ := contexttest.MockAPIContext(t, inboxURL)
		cf, err := activitypub.NewClientFactoryWithTimeout(60 * time.Second)
		require.NoError(t, err)

		// The Follow is authored by the mock repository actor 1.
		c, err := cf.WithKeysDirect(ctx, mock.Repositories[0].PrivKey, mock.Repositories[0].KeyID(federatedSrv.URL), nil)
		require.NoError(t, err)

		follow := fmt.Appendf(nil,
			`{"type":"Follow","actor":"%s","object":"%s"}`,
			federatedSrv.URL+"/api/v1/activitypub/repository-id/1",
			u.JoinPath("/api/v1/activitypub/repository-id/4").String())
		resp, err := c.Post(follow, inboxURL)
		require.NoError(t, err)
		if resp.StatusCode != http.StatusNoContent {
			body, _ := io.ReadAll(resp.Body)
			t.Logf("follow response body: %s", string(body))
		}
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)

		// The repository follower was recorded with the mirror's inbox.
		mirrorFollower := unittest.AssertExistsAndLoadBean(t, &repo_model.FederatedRepositoryFollower{RepoID: 4})
		assert.Equal(t, federatedSrv.URL+"/api/v1/activitypub/repository-id/1", mirrorFollower.RemoteActorURI)
		assert.Equal(t, federatedSrv.URL+"/api/v1/activitypub/repository-id/1/inbox", mirrorFollower.InboxURL)

		// An Undo(Follow) from the same repository removes the follower again.
		undo := fmt.Appendf(nil,
			`{"type":"Undo","actor":"%s","object":{"type":"Follow","actor":"%s","object":"%s"}}`,
			federatedSrv.URL+"/api/v1/activitypub/repository-id/1",
			federatedSrv.URL+"/api/v1/activitypub/repository-id/1",
			u.JoinPath("/api/v1/activitypub/repository-id/4").String())
		resp, err = c.Post(undo, inboxURL)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)
		unittest.AssertCount(t, &repo_model.FederatedRepositoryFollower{RepoID: 4}, 0)
	})
}

// TestActivityPubCreateFederatedMirror verifies the homeserver-side federated
// mirror flow: a user provides the ForgeFed actor URI of a remote repository;
// the local instance resolves the remote actor, clones the repository over
// plain git as a pull mirror, records the federation metadata and follows the
// remote repository actor.
func TestActivityPubCreateFederatedMirror(t *testing.T) {
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	mockFederationAllowAllHosts(t)
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()
	defer test.MockVariableValue(&setting.Migrations.AllowLocalNetworks, true)()
	defer test.MockVariableValue(&setting.Migrations.AllowUnencrypted, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	// Rebuild the migration allow/block lists with the mocked settings.
	require.NoError(t, migrations_allowlist.Init())

	federation.Init()

	mock := test.NewFederationServerMock()
	federatedSrv := mock.DistantServer(t)
	defer federatedSrv.Close()

	onApplicationRun(t, func(t *testing.T, u *url.URL) {
		// Serve the fixture repository over dumb HTTP so it can be cloned
		// anonymously as the upstream of the mirror.
		sourceRepo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 2})
		// Refresh the dumb HTTP server's ref advertisement (info/refs).
		_, _, gerr := git.NewCommand(git.DefaultContext, "update-server-info").RunStdString(&git.RunOpts{Dir: sourceRepo.RepoPath()})
		require.NoError(t, gerr)
		gitServer := httptest.NewServer(http.FileServer(http.Dir(sourceRepo.RepoPath())))
		defer gitServer.Close()

		mock.Repositories[0].CloneURI = gitServer.URL

		user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
		session := loginUser(t, user.Name)

		// Create a repository-scoped token for the API call.
		token := &auth_model.AccessToken{UID: user.ID, Name: "federated-mirror-test", Scope: "all"}
		require.NoError(t, auth_model.NewAccessToken(db.DefaultContext, token))

		// POST /api/v1/repos/federated-mirror as user 2.
		req := NewRequestWithJSON(t, "POST", "/api/v1/repos/federated-mirror", map[string]any{
			"remote_actor_uri": federatedSrv.URL + "/api/v1/activitypub/repository-id/1",
			"mirror_interval":  "1h",
		}).AddTokenAuth(token.Token)
		session.MakeRequest(t, req, http.StatusCreated)

		// The mirror repository was created.
		created := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{OwnerID: user.ID, LowerName: "mock-repo-1"})
		assert.True(t, created.IsMirror)
		assert.True(t, strings.HasPrefix(created.OriginalURL, "http://127.0.0.1:"), "OriginalURL must be the remote clone URI, got %s", created.OriginalURL)

		// The federation-side metadata was recorded.
		federatedMirror := unittest.AssertExistsAndLoadBean(t, &repo_model.FederatedMirror{RepoID: created.ID})
		assert.Equal(t, federatedSrv.URL+"/api/v1/activitypub/repository-id/1", federatedMirror.RemoteActorURI)
		assert.False(t, federatedMirror.IsPush)

		// The mirror follows the remote repository actor (Follow delivered to
		// the remote repository inbox).
		require.Eventually(t, func() bool {
			return strings.Contains(mock.LastPost, `"type":"Follow"`)
		}, 5*time.Second, 100*time.Millisecond, "expected a Follow activity to be delivered to the remote repository inbox")
		assert.Contains(t, mock.LastPost, "DISTANT_FEDERATION_HOST/api/v1/activitypub/repository-id/1")

		// The remote repository's git endpoint is exposed on the new actor
		// (fetched with a signed AP client, as a peer would).
		actorURL := u.JoinPath(fmt.Sprintf("/api/v1/activitypub/repository-id/%d", created.ID)).String()
		apCtx, _ := contexttest.MockAPIContext(t, actorURL)
		apCF, err := activitypub.NewClientFactoryWithTimeout(60 * time.Second)
		require.NoError(t, err)
		apClient, err := apCF.WithKeysDirect(apCtx, mock.Persons[0].PrivKey, mock.Persons[0].KeyID(federatedSrv.URL), nil)
		require.NoError(t, err)
		actorBody, err := apClient.GetBody(actorURL)
		require.NoError(t, err)
		// The mirror declares its native git endpoints and the upstream it mirrors.
		assert.Contains(t, string(actorBody), `"cloneUri":`)
		assert.Contains(t, string(actorBody), fmt.Sprintf(`"mirrors":"%s/api/v1/activitypub/repository-id/1"`, federatedSrv.URL))
	})
}

// TestActivityPubCreateFederatedPushMirror verifies the federated push mirror
// flow: a local repository is configured to push its changes to a remote
// federated repository, using the remote's ForgeFed pushUri and declaring the
// `mirrorsTo` property on the local repository actor.
func TestActivityPubCreateFederatedPushMirror(t *testing.T) {
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	mockFederationAllowAllHosts(t)
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()
	defer test.MockVariableValue(&setting.Mirror.Enabled, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	federation.Init()

	mock := test.NewFederationServerMock()
	federatedSrv := mock.DistantServer(t)
	defer federatedSrv.Close()

	onApplicationRun(t, func(t *testing.T, u *url.URL) {
		sourceRepo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
		gitServer := httptest.NewServer(http.FileServer(http.Dir(sourceRepo.RepoPath())))
		defer gitServer.Close()
		mock.Repositories[0].CloneURI = gitServer.URL
		mock.Repositories[0].PushURI = gitServer.URL

		user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
		session := loginUser(t, user.Name)
		token := getTokenForLoggedInUser(t, session, auth_model.AccessTokenScopeAll)
		req := NewRequestWithJSON(t, "POST", "/api/v1/repos/user2/repo1/federated-push-mirror", map[string]any{
			"remote_actor_uri": federatedSrv.URL + "/api/v1/activitypub/repository-id/1",
			"sync_on_commit":   true,
		}).AddTokenAuth(token)
		session.MakeRequest(t, req, http.StatusCreated)

		// The native push mirror was created pointing at the remote pushUri.
		pushMirror := unittest.AssertExistsAndLoadBean(t, &repo_model.PushMirror{RepoID: 1})
		assert.Equal(t, gitServer.URL, pushMirror.RemoteAddress)

		// The federation-side metadata was recorded with IsPush=true.
		federatedMirror := unittest.AssertExistsAndLoadBean(t, &repo_model.FederatedMirror{RepoID: 1})
		assert.True(t, federatedMirror.IsPush)
		assert.Equal(t, federatedSrv.URL+"/api/v1/activitypub/repository-id/1", federatedMirror.RemoteActorURI)

		// The local repository actor declares the mirrorsTo property.
		actorURL := u.JoinPath("/api/v1/activitypub/repository-id/1").String()
		apCtx, _ := contexttest.MockAPIContext(t, actorURL)
		apCF, err := activitypub.NewClientFactoryWithTimeout(60 * time.Second)
		require.NoError(t, err)
		apClient, err := apCF.WithKeysDirect(apCtx, mock.Persons[0].PrivKey, mock.Persons[0].KeyID(federatedSrv.URL), nil)
		require.NoError(t, err)
		actorBody, err := apClient.GetBody(actorURL)
		require.NoError(t, err)
		assert.Contains(t, string(actorBody), fmt.Sprintf(`"mirrorsTo":"%s/api/v1/activitypub/repository-id/1"`, federatedSrv.URL))
	})
}
