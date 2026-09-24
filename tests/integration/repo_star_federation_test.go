// Copyright 2024 The Forgejo Authors c/o Codeberg e.V.. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"forgejo.org/models/forgefed"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	fm "forgejo.org/modules/forgefed"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/modules/validation"
	"forgejo.org/tests"

	ap "github.com/go-ap/activitypub"
	"github.com/stretchr/testify/require"
)

func TestActivityPubRepoFollowing(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()

	mock := test.NewFederationServerMock()
	federatedSrv := mock.DistantServer(t)
	defer federatedSrv.Close()

	user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1, OwnerID: user.ID})
	session := loginUser(t, user.Name)

	t.Run("Add a following repo", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		link := fmt.Sprintf("/%s/settings", repo.FullName())

		req := NewRequestWithValues(t, "POST", link, map[string]string{
			"action":          "federation",
			"following_repos": fmt.Sprintf("%s/api/v1/activitypub/repository-id/1", federatedSrv.URL),
		})
		session.MakeRequest(t, req, http.StatusSeeOther)

		// Verify it was added.
		federationHost := unittest.AssertExistsAndLoadBean(t, &forgefed.FederationHost{HostFqdn: "127.0.0.1"})
		unittest.AssertExistsAndLoadBean(t, &repo_model.FollowingRepo{
			ExternalID:       "1",
			FederationHostID: federationHost.ID,
		})
	})

	t.Run("Star a repo having a following repo", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		repoLink := fmt.Sprintf("/%s", repo.FullName())
		link := fmt.Sprintf("%s/action/star", repoLink)
		req := NewRequest(t, "POST", link)

		session.MakeRequest(t, req, http.StatusOK)

		// Verify distant server received a like activity
		like := fm.ForgeLike{}
		err := like.UnmarshalJSON([]byte(mock.LastPost))
		require.NoErrorf(t, err, "Error unmarshalling ForgeLike: %q", err)

		isValid, err := validation.IsValid(like)
		require.NoError(t, err, "ForgeLike is not valid")
		require.True(t, isValid, "ForgeLike is not valid")
		require.Equal(t, ap.LikeType, like.Type, "Activity is not a like for this repo")

		iri := like.Object.GetLink().String()
		isCorrectObject := strings.HasSuffix(iri, "/api/v1/activitypub/repository-id/1")
		require.True(t, isCorrectObject, "Activity is not a Like for this repo")
	})

	t.Run("Unstar a repo having a following staring repo", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		repoLink := fmt.Sprintf("/%s", repo.FullName())
		link := fmt.Sprintf("%s/action/unstar", repoLink)
		req := NewRequest(t, "POST", link)

		session.MakeRequest(t, req, http.StatusOK)

		// Verify distant server received a undoLike activity
		activity := ap.Activity{}
		err := activity.UnmarshalJSON([]byte(mock.LastPost))
		if err != nil {
			t.Errorf("Error unmarshalling Activity: %q", err)
		}

		// Verify distant server received a undoLike activity
		undoLike, err := fm.NewForgeUndoLikeFromActivity(&activity)
		require.NoErrorf(t, err, "Error converting ForgeUndoLike from activity: %q", err)

		isValid, err := validation.IsValid(undoLike)
		require.NoErrorf(t, err, "ForgeUndoLike is not valid")
		require.Truef(t, isValid, "ForgeUndoLike is not valid: %q", err)

		like := undoLike.Object.(ap.Activity)

		iri := like.Object.GetLink().String()
		isCorrectObject := strings.HasSuffix(iri, "/api/v1/activitypub/repository-id/1")
		require.True(t, isCorrectObject, "Activity is not a UndoLike for this repo")
	})
}
