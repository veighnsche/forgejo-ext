// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package migrations

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	base "forgejo.org/modules/migration"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/migrations/allowlist"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseBitbucketDataCenterURL(t *testing.T) {
	cases := []struct {
		url     string
		base    string
		project string
		repo    string
	}{
		{"https://bitbucket.example.com/scm/PROJ/myrepo.git", "https://bitbucket.example.com", "PROJ", "myrepo"},
		{"https://bitbucket.example.com/context/scm/PROJ/myrepo.git", "https://bitbucket.example.com/context", "PROJ", "myrepo"},
	}
	for _, c := range cases {
		u, err := url.Parse(c.url)
		require.NoError(t, err)
		baseURL, project, repo, err := parseBitbucketDataCenterURL(u)
		require.NoError(t, err, c.url)
		assert.Equal(t, c.base, baseURL.String(), c.url)
		assert.Equal(t, c.project, project, c.url)
		assert.Equal(t, c.repo, repo, c.url)
	}

	for _, invalid := range []string{
		"https://bitbucket.example.com/unexpected/path",
		"https://bitbucket.example.com/projects/PROJ/repos/myrepo/browse",
		"https://bitbucket.example.com/scm/onlyproject",
	} {
		u, err := url.Parse(invalid)
		require.NoError(t, err)
		_, _, _, err = parseBitbucketDataCenterURL(u)
		require.Error(t, err, invalid)
	}
}

func TestBitbucketDataCenterFormatCloneURL(t *testing.T) {
	d := &BitbucketDataCenterDownloader{}

	got, err := d.FormatCloneURL(base.MigrateOptions{AuthToken: "sometoken"}, "https://bitbucket.example.com/scm/PROJ/myrepo.git")
	require.NoError(t, err)
	assert.Equal(t, "https://x-token-auth:sometoken@bitbucket.example.com/scm/PROJ/myrepo.git", got)

	got, err = d.FormatCloneURL(base.MigrateOptions{AuthUsername: "jdoe", AuthToken: "sometoken"}, "https://bitbucket.example.com/scm/PROJ/myrepo.git")
	require.NoError(t, err)
	assert.Equal(t, "https://jdoe:sometoken@bitbucket.example.com/scm/PROJ/myrepo.git", got)

	got, err = d.FormatCloneURL(base.MigrateOptions{}, "https://bitbucket.example.com/scm/PROJ/myrepo.git")
	require.NoError(t, err)
	assert.Equal(t, "https://bitbucket.example.com/scm/PROJ/myrepo.git", got)
}

func TestBitbucketDataCenterDownloaderBlocksLocalhost(t *testing.T) {
	defer test.MockVariableValueWithReset(&setting.Migrations.AllowLocalNetworks, false, func() { require.NoError(t, allowlist.Init()) })()

	factory := &BitbucketDataCenterDownloaderFactory{}
	downloader, err := factory.New(t.Context(), base.MigrateOptions{CloneAddr: "http://localhost/scm/PROJ/myrepo.git"})
	require.NoError(t, err)

	_, err = downloader.GetRepoInfo()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "can only call allowed HTTP servers")
}

func TestBitbucketDataCenterGetRepoInfo(t *testing.T) {
	defer test.MockVariableValueWithReset(&setting.Migrations.AllowLocalNetworks, true, func() { require.NoError(t, allowlist.Init()) })()

	mux := http.NewServeMux()
	var serverURL string
	mux.HandleFunc("/rest/api/1.0/projects/PROJ/repos/myrepo", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer sometoken", r.Header.Get("Authorization"))
		_, _ = io.WriteString(w, fmt.Sprintf(`{
			"slug": "myrepo",
			"name": "myrepo",
			"description": "My repository",
			"public": false,
			"links": {
				"clone": [
					{"href": "ssh://git@bitbucket.example.com/PROJ/myrepo.git", "name": "ssh"},
					{"href": "%s/scm/PROJ/myrepo.git", "name": "http"}
				],
				"self": [{"href": "%s/projects/PROJ/repos/myrepo/browse"}]
			}
		}`, serverURL, serverURL))
	})
	mux.HandleFunc("/rest/api/1.0/projects/PROJ/repos/myrepo/branches/default", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id": "refs/heads/main", "displayId": "main"}`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	serverURL = server.URL

	factory := &BitbucketDataCenterDownloaderFactory{}
	downloader, err := factory.New(t.Context(), base.MigrateOptions{
		CloneAddr: server.URL + "/scm/PROJ/myrepo.git",
		AuthToken: "sometoken",
	})
	require.NoError(t, err)

	repo, err := downloader.GetRepoInfo()
	require.NoError(t, err)
	assert.Equal(t, "myrepo", repo.Name)
	assert.Equal(t, "PROJ", repo.Owner)
	assert.Equal(t, "My repository", repo.Description)
	assert.True(t, repo.IsPrivate)
	assert.Equal(t, "main", repo.DefaultBranch)
	assert.Equal(t, server.URL+"/scm/PROJ/myrepo.git", repo.CloneURL)
	assert.Equal(t, server.URL+"/projects/PROJ/repos/myrepo/browse", repo.OriginalURL)
}

func TestBitbucketDataCenterGetPullRequests(t *testing.T) {
	defer test.MockVariableValueWithReset(&setting.Migrations.AllowLocalNetworks, true, func() { require.NoError(t, allowlist.Init()) })()

	mux := http.NewServeMux()
	var serverURL string
	mux.HandleFunc("/rest/api/1.0/projects/PROJ/repos/myrepo/pull-requests", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "ALL", r.URL.Query().Get("state"))
		assert.Equal(t, "2", r.URL.Query().Get("limit"))
		switch r.URL.Query().Get("start") {
		case "0":
			_, _ = io.WriteString(w, fmt.Sprintf(`{
				"isLastPage": false,
				"nextPageStart": 17,
				"values": [
					{
						"id": 1,
						"title": "An open pull request",
						"description": "The description",
						"state": "OPEN",
						"draft": true,
						"locked": true,
						"createdDate": 1704067200000,
						"updatedDate": 1704153600000,
						"fromRef": {
							"displayId": "feature/change",
							"latestCommit": "0123456789012345678901234567890123456789",
							"repository": {"slug": "myrepo", "project": {"key": "PROJ"}}
						},
						"toRef": {
							"displayId": "main",
							"latestCommit": "aaaa456789012345678901234567890123456789",
							"repository": {"slug": "myrepo", "project": {"key": "PROJ"}}
						},
						"author": {"user": {"id": 42, "name": "jdoe", "emailAddress": "jdoe@example.com"}}
					},
					{
						"id": 2,
						"title": "A merged fork pull request",
						"state": "MERGED",
						"createdDate": 1704067200000,
						"updatedDate": 1704153600000,
						"closedDate": 1704240000000,
						"fromRef": {
							"displayId": "fix",
							"latestCommit": "bbbb456789012345678901234567890123456789",
							"repository": {
								"slug": "myrepo-fork",
								"project": {"key": "FORK"},
								"links": {"clone": [{"href": "%s/scm/FORK/myrepo-fork.git", "name": "http"}]}
							}
						},
						"toRef": {
							"displayId": "main",
							"latestCommit": "aaaa456789012345678901234567890123456789",
							"repository": {"slug": "myrepo", "project": {"key": "PROJ"}}
						},
						"author": {"user": {"id": 43, "name": "asmith", "emailAddress": "asmith@example.com"}},
						"properties": {"mergeCommit": {"id": "cccc456789012345678901234567890123456789"}}
					}
				]
			}`, serverURL))
		case "17":
			_, _ = io.WriteString(w, `{
				"isLastPage": true,
				"values": [
					{
						"id": 3,
						"title": "A declined pull request",
						"state": "DECLINED",
						"createdDate": 1704067200000,
						"updatedDate": 1704153600000,
						"fromRef": {
							"displayId": "abandoned",
							"latestCommit": "dddd456789012345678901234567890123456789",
							"repository": {"slug": "myrepo", "project": {"key": "PROJ"}}
						},
						"toRef": {
							"displayId": "main",
							"latestCommit": "aaaa456789012345678901234567890123456789",
							"repository": {"slug": "myrepo", "project": {"key": "PROJ"}}
						},
						"author": {"user": {"id": 42, "name": "jdoe", "emailAddress": "jdoe@example.com"}}
					}
				]
			}`)
		default:
			t.Errorf("unexpected start parameter: %s", r.URL.Query().Get("start"))
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	serverURL = server.URL

	factory := &BitbucketDataCenterDownloaderFactory{}
	downloader, err := factory.New(t.Context(), base.MigrateOptions{
		CloneAddr: server.URL + "/scm/PROJ/myrepo.git",
		AuthToken: "sometoken",
	})
	require.NoError(t, err)

	prs, isEnd, err := downloader.GetPullRequests(1, 2)
	require.NoError(t, err)
	assert.False(t, isEnd)
	require.Len(t, prs, 2)

	open := prs[0]
	assert.Equal(t, int64(1), open.Number)
	assert.Equal(t, "An open pull request", open.Title)
	assert.Equal(t, "The description", open.Content)
	assert.Equal(t, "open", open.State)
	assert.True(t, open.IsDraft)
	assert.True(t, open.IsLocked)
	assert.False(t, open.Merged)
	assert.Nil(t, open.Closed)
	assert.Equal(t, int64(42), open.PosterID)
	assert.Equal(t, "jdoe", open.PosterName)
	assert.Equal(t, "jdoe@example.com", open.PosterEmail)
	assert.Equal(t, int64(1704067200000), open.Created.UnixMilli())
	assert.Equal(t, int64(1704153600000), open.Updated.UnixMilli())
	assert.Equal(t, "feature/change", open.Head.Ref)
	assert.Equal(t, "0123456789012345678901234567890123456789", open.Head.SHA)
	assert.Empty(t, open.Head.CloneURL)
	assert.Equal(t, "main", open.Base.Ref)
	assert.False(t, open.IsForkPullRequest())
	assert.True(t, open.EnsuredSafe)

	merged := prs[1]
	assert.Equal(t, int64(2), merged.Number)
	assert.Equal(t, "closed", merged.State)
	assert.True(t, merged.Merged)
	require.NotNil(t, merged.MergedTime)
	assert.Equal(t, int64(1704240000000), merged.MergedTime.UnixMilli())
	require.NotNil(t, merged.Closed)
	assert.Equal(t, "cccc456789012345678901234567890123456789", merged.MergeCommitSHA)
	assert.Equal(t, server.URL+"/scm/FORK/myrepo-fork.git", merged.Head.CloneURL)
	assert.Equal(t, "FORK/myrepo-fork", merged.Head.RepoFullName())
	assert.True(t, merged.IsForkPullRequest())
	assert.True(t, merged.EnsuredSafe)

	prs, isEnd, err = downloader.GetPullRequests(2, 2)
	require.NoError(t, err)
	assert.True(t, isEnd)
	require.Len(t, prs, 1)

	declined := prs[0]
	assert.Equal(t, int64(3), declined.Number)
	assert.Equal(t, "closed", declined.State)
	assert.False(t, declined.Merged)
	assert.Nil(t, declined.MergedTime)
	require.NotNil(t, declined.Closed)
	// No closedDate on old Bitbucket versions: the last update is used instead.
	assert.Equal(t, int64(1704153600000), declined.Closed.UnixMilli())
}

func TestBitbucketDataCenterGetRepoInfoWithoutHTTPCloneLink(t *testing.T) {
	defer test.MockVariableValueWithReset(&setting.Migrations.AllowLocalNetworks, true, func() { require.NoError(t, allowlist.Init()) })()

	// When HTTP(S) SCM hosting is disabled, the API only exposes the ssh clone link.
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/1.0/projects/PROJ/repos/myrepo", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{
			"slug": "myrepo",
			"links": {"clone": [{"href": "ssh://git@bitbucket.example.com:7999/PROJ/myrepo.git", "name": "ssh"}]}
		}`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	factory := &BitbucketDataCenterDownloaderFactory{}
	downloader, err := factory.New(t.Context(), base.MigrateOptions{
		CloneAddr: server.URL + "/scm/PROJ/myrepo.git",
	})
	require.NoError(t, err)

	_, err = downloader.GetRepoInfo()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP(S) SCM hosting")
}
