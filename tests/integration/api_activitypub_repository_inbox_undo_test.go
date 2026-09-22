// Copyright 2024, 2025, 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/activitypub"
	"forgejo.org/modules/json"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/routers"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActivityPubRepositoryInboxUndoLike(t *testing.T) {
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	mock := test.NewFederationServerMock()
	federatedSrv := mock.DistantServer(t)
	defer federatedSrv.Close()

	onApplicationRun(t, func(t *testing.T, u *url.URL) {
		repositoryID := 2
		timeNow := time.Now().UTC()
		localRepo2 := u.JoinPath(fmt.Sprintf("/api/v1/activitypub/repository-id/%d", repositoryID)).String()
		localRepo2Inbox := fmt.Sprintf("%s/inbox", localRepo2)

		ctx, _ := contexttest.MockAPIContext(t, localRepo2Inbox)
		cf, err := activitypub.NewClientFactoryWithTimeout(60 * time.Second)
		require.NoError(t, err)
		c, err := cf.WithKeysDirect(ctx, mock.Persons[0].PrivKey,
			mock.Persons[0].KeyID(federatedSrv.URL), nil)
		require.NoError(t, err)

		// The user id 15 sends like activity for repo id 2
		distantActorUser15 := fmt.Sprintf("%s/api/v1/activitypub/user-id/15", federatedSrv.URL)
		activityUser15LikesRepo2, err := json.Marshal(map[string]any{
			"type":      "Like",
			"startTime": timeNow.Format(time.RFC3339),
			"actor":     distantActorUser15,
			"object":    localRepo2,
		})
		if err != nil {
			require.Errorf(t, err, "failed to marshal: activityUser15LikesRepo2")
		}
		t.Logf("activity: %s", activityUser15LikesRepo2)
		resp, err := c.Post(activityUser15LikesRepo2, localRepo2Inbox)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)
		repo2 := unittest.AssertExistsAndLoadBean(t, &repo.Repository{ID: int64(repositoryID)})
		assert.Equal(t, 2, repo2.NumStars)

		// The user id 15 sends undo like activity for repo id 2
		activityUser15UndoLikesRepo2, err := json.Marshal(map[string]any{
			"type":      "Undo",
			"startTime": timeNow.Add(time.Second * 2).Format(time.RFC3339),
			"actor":     distantActorUser15,
			"object": map[string]any{
				"type":   "Like",
				"actor":  distantActorUser15,
				"object": localRepo2,
			},
		})
		if err != nil {
			require.Errorf(t, err, "failed to marshal: activityUser15UndoLikesRepo2")
		}
		// test it
		resp, err = c.Post(activityUser15UndoLikesRepo2, localRepo2Inbox)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		repo2 = unittest.AssertExistsAndLoadBean(t, &repo.Repository{ID: int64(repositoryID)})
		assert.Equal(t, 1, repo2.NumStars)

		// replay undo like activityUser15UndoLikesRepo2
		resp, err = c.Post(activityUser15UndoLikesRepo2, localRepo2Inbox)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNotAcceptable, resp.StatusCode)

		// second undo should result in error
		secondActivityUser15LikesRepo2, err := json.Marshal(map[string]any{
			"type":      "Like",
			"startTime": timeNow.Format(time.RFC3339),
			"actor":     distantActorUser15,
			"object":    localRepo2,
		})
		if err != nil {
			require.Errorf(t, err, "failed to marshal: secondActivityUser15LikesRepo2")
		}
		resp, err = c.Post(secondActivityUser15LikesRepo2, localRepo2Inbox)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNotAcceptable, resp.StatusCode)

		// The user id 30 will fail, as there is no prior like
		activityUser30UndoLikesRepo2, err := json.Marshal(map[string]any{
			"type":      "Undo",
			"startTime": timeNow.Add(time.Second * 2).Format(time.RFC3339),
			"actor":     federatedSrv.URL + "/api/v1/activitypub/user-id/30",
			"object": map[string]any{
				"type":   "Like",
				"actor":  federatedSrv.URL + "/api/v1/activitypub/user-id/30",
				"object": localRepo2Inbox,
			},
		})
		if err != nil {
			require.Errorf(t, err, "failed to marshal: activityUser30UndoLikesRepo2")
		}
		t.Logf("activity: %s", activityUser30UndoLikesRepo2)
		resp, err = c.Post(activityUser30UndoLikesRepo2, localRepo2Inbox)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNotAcceptable, resp.StatusCode)
	})
}
