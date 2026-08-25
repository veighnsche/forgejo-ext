// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"forgejo.org/models/db"
	"forgejo.org/models/moderation"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/activitypub"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/routers"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestActivityPubPersonInboxFlag verifies that an inbound Flag activity from a
// remote instance is mapped to an abuse report that admins can review: the
// remote reporter is materialised, the flagged local user actor is resolved to
// the local user, and a Report with the Flag's remarks is stored.
func TestActivityPubPersonInboxFlag(t *testing.T) {
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	mockFederationAllowAllHosts(t)
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	mock := test.NewFederationServerMock()
	federatedSrv := mock.DistantServer(t)
	defer federatedSrv.Close()

	onApplicationRun(t, func(t *testing.T, u *url.URL) {
		localUserID := 2
		localUser2Actor := u.JoinPath(fmt.Sprintf("/api/v1/activitypub/user-id/%d", localUserID)).String()
		localUser2Inbox := u.JoinPath(fmt.Sprintf("/api/v1/activitypub/user-id/%d/inbox", localUserID)).String()

		ctx, _ := contexttest.MockAPIContext(t, localUser2Inbox)
		cf, err := activitypub.NewClientFactoryWithTimeout(60 * time.Second)
		require.NoError(t, err)

		c, err := cf.WithKeysDirect(ctx, mock.Persons[0].PrivKey,
			mock.Persons[0].KeyID(federatedSrv.URL), nil)
		require.NoError(t, err)

		flagActivity := fmt.Appendf(nil,
			`{"type":"Flag",`+
				`"actor":"%s/api/v1/activitypub/user-id/15",`+
				`"object":["%s"],`+
				`"content":"Reported as spam"}`,
			federatedSrv.URL, localUser2Actor)
		resp, err := c.Post(flagActivity, localUser2Inbox)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)

		// The remote reporter is materialised as a federated user.
		distantFederatedUser := unittest.AssertExistsAndLoadBean(t, &user_model.FederatedUser{ExternalID: "15"})

		// An abuse report must exist: remote user reported local user 2 as spam.
		report := moderation.AbuseReport{
			ReporterID:  distantFederatedUser.UserID,
			ContentType: moderation.ReportedContentTypeUser,
			ContentID:   int64(localUserID),
		}
		has, err := db.GetEngine(ctx).Get(&report)
		require.NoError(t, err)
		require.True(t, has, "expected an abuse report for the flagged local user")
		assert.Equal(t, moderation.AbuseCategoryTypeOther, report.Category)
		assert.Equal(t, "Reported as spam", report.Remarks)
		assert.Equal(t, moderation.ReportStatusTypeOpen, report.Status)
	})
}

// TestActivityPubRepositoryInboxFlag verifies Flag activities delivered to a
// repository inbox map to repository abuse reports.
func TestActivityPubRepositoryInboxFlag(t *testing.T) {
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	mockFederationAllowAllHosts(t)
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	mock := test.NewFederationServerMock()
	federatedSrv := mock.DistantServer(t)
	defer federatedSrv.Close()

	onApplicationRun(t, func(t *testing.T, u *url.URL) {
		repositoryID := 1 // public repository: federation must not expose private ones
		localRepoActor := u.JoinPath(fmt.Sprintf("/api/v1/activitypub/repository-id/%d", repositoryID)).String()
		localRepoInbox := u.JoinPath(fmt.Sprintf("/api/v1/activitypub/repository-id/%d/inbox", repositoryID)).String()

		ctx, _ := contexttest.MockAPIContext(t, localRepoInbox)
		cf, err := activitypub.NewClientFactoryWithTimeout(60 * time.Second)
		require.NoError(t, err)

		c, err := cf.WithKeysDirect(ctx, mock.Persons[0].PrivKey,
			mock.Persons[0].KeyID(federatedSrv.URL), nil)
		require.NoError(t, err)

		flagActivity := fmt.Appendf(nil,
			`{"type":"Flag",`+
				`"actor":"%s/api/v1/activitypub/user-id/15",`+
				`"object":["%s"],`+
				`"content":"Malware in repository"}`,
			federatedSrv.URL, localRepoActor)
		resp, err := c.Post(flagActivity, localRepoInbox)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)

		distantFederatedUser := unittest.AssertExistsAndLoadBean(t, &user_model.FederatedUser{ExternalID: "15"})

		report := moderation.AbuseReport{
			ReporterID:  distantFederatedUser.UserID,
			ContentType: moderation.ReportedContentTypeRepository,
			ContentID:   int64(repositoryID),
		}
		has, err := db.GetEngine(ctx).Get(&report)
		require.NoError(t, err)
		require.True(t, has, "expected an abuse report for the flagged local repository")
		assert.Equal(t, "Malware in repository", report.Remarks)
	})
}
