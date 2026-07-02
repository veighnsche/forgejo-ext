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
