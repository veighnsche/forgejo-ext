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
	// Subtree route: per-PR activity streams, empty here.
	mux.HandleFunc("/rest/api/1.0/projects/PROJ/repos/myrepo/pull-requests/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"isLastPage": true, "values": []}`)
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

func TestBitbucketDataCenterGetCommentsAndReviews(t *testing.T) {
	defer test.MockVariableValueWithReset(&setting.Migrations.AllowLocalNetworks, true, func() { require.NoError(t, allowlist.Init()) })()

	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/1.0/projects/PROJ/repos/myrepo/pull-requests", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{
			"isLastPage": true,
			"values": [
				{
					"id": 5,
					"title": "A reviewed pull request",
					"state": "OPEN",
					"createdDate": 1704067200000,
					"updatedDate": 1704153600000,
					"fromRef": {
						"displayId": "feature",
						"latestCommit": "0123456789012345678901234567890123456789",
						"repository": {"slug": "myrepo", "project": {"key": "PROJ"}}
					},
					"toRef": {
						"displayId": "main",
						"latestCommit": "aaaa456789012345678901234567890123456789",
						"repository": {"slug": "myrepo", "project": {"key": "PROJ"}}
					},
					"author": {"user": {"id": 9, "name": "alice", "emailAddress": "alice@example.com"}}
				}
			]
		}`)
	})
	// Activities are served newest first, like the real API.
	mux.HandleFunc("/rest/api/1.0/projects/PROJ/repos/myrepo/pull-requests/5/activities", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("start") {
		case "0":
			_, _ = io.WriteString(w, `{
				"isLastPage": false,
				"nextPageStart": 42,
				"values": [
					{
						"id": 106,
						"action": "UNAPPROVED",
						"createdDate": 1704270000000,
						"user": {"id": 10, "name": "dave", "emailAddress": "dave@example.com"}
					},
					{
						"id": 105,
						"action": "COMMENTED",
						"commentAction": "ADDED",
						"createdDate": 1704260000000,
						"user": {"id": 7, "name": "bob"},
						"comment": {
							"id": 21,
							"text": "Answered offline",
							"author": {"id": 7, "name": "bob", "emailAddress": "bob@example.com"},
							"createdDate": 1704260000000,
							"updatedDate": 1704260000000
						}
					},
					{
						"id": 104,
						"action": "COMMENTED",
						"commentAction": "ADDED",
						"createdDate": 1704250000000,
						"user": {"id": 9, "name": "alice"},
						"comment": {
							"id": 20,
							"text": "General question about the approach",
							"author": {"id": 9, "name": "alice", "emailAddress": "alice@example.com"},
							"createdDate": 1704250000000,
							"updatedDate": 1704250000000,
							"comments": [
								{
									"id": 21,
									"text": "Answered offline",
									"author": {"id": 7, "name": "bob", "emailAddress": "bob@example.com"},
									"createdDate": 1704260000000,
									"updatedDate": 1704260000000
								}
							]
						}
					},
					{
						"id": 103,
						"action": "REVIEWED",
						"createdDate": 1704240000000,
						"user": {"id": 8, "name": "carol", "emailAddress": "carol@example.com"}
					}
				]
			}`)
		case "42":
			_, _ = io.WriteString(w, `{
				"isLastPage": true,
				"values": [
					{
						"id": 102,
						"action": "COMMENTED",
						"commentAction": "ADDED",
						"createdDate": 1704230000000,
						"user": {"id": 7, "name": "bob"},
						"comment": {
							"id": 32,
							"text": "This used to work before",
							"author": {"id": 7, "name": "bob", "emailAddress": "bob@example.com"},
							"createdDate": 1704230000000,
							"updatedDate": 1704230000000
						},
						"commentAnchor": {"path": "old.go", "line": 3, "fileType": "FROM", "toHash": "0123456789012345678901234567890123456789"}
					},
					{
						"id": 101,
						"action": "COMMENTED",
						"commentAction": "ADDED",
						"createdDate": 1704210000000,
						"user": {"id": 8, "name": "carol"},
						"comment": {
							"id": 30,
							"text": "This range looks wrong",
							"author": {"id": 8, "name": "carol", "emailAddress": "carol@example.com"},
							"createdDate": 1704210000000,
							"updatedDate": 1704210000000,
							"comments": [
								{
									"id": 31,
									"text": "Fixed in the next commit",
									"author": {"id": 9, "name": "alice", "emailAddress": "alice@example.com"},
									"createdDate": 1704220000000,
									"updatedDate": 1704220000000
								}
							]
						},
						"commentAnchor": {"path": "src/main.go", "line": 12, "fileType": "TO", "toHash": "0123456789012345678901234567890123456789", "multilineMarker": {"startLine": 10}}
					},
					{
						"id": 100,
						"action": "APPROVED",
						"createdDate": 1704200000000,
						"user": {"id": 7, "name": "bob", "emailAddress": "bob@example.com"}
					},
					{
						"id": 99,
						"action": "APPROVED",
						"createdDate": 1704195000000,
						"user": {"id": 10, "name": "dave", "emailAddress": "dave@example.com"}
					}
				]
			}`)
		default:
			t.Errorf("unexpected start parameter: %s", r.URL.Query().Get("start"))
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	factory := &BitbucketDataCenterDownloaderFactory{}
	downloader, err := factory.New(t.Context(), base.MigrateOptions{
		CloneAddr: server.URL + "/scm/PROJ/myrepo.git",
		AuthToken: "sometoken",
	})
	require.NoError(t, err)

	prs, isEnd, err := downloader.GetPullRequests(1, 10)
	require.NoError(t, err)
	assert.True(t, isEnd)
	require.Len(t, prs, 1)

	// The comment with id 21 appears both nested in its thread and as its own activity: kept
	// once, and comments come out chronologically despite the newest-first activity stream.
	comments, _, err := downloader.GetComments(prs[0])
	require.NoError(t, err)
	require.Len(t, comments, 2)
	assert.Equal(t, int64(5), comments[0].IssueIndex)
	assert.Equal(t, "General question about the approach", comments[0].Content)
	assert.Equal(t, int64(9), comments[0].PosterID)
	assert.Equal(t, "alice", comments[0].PosterName)
	assert.Equal(t, int64(1704250000000), comments[0].Created.UnixMilli())
	assert.Equal(t, "Answered offline", comments[1].Content)
	assert.Equal(t, "bob", comments[1].PosterName)

	reviews, err := downloader.GetReviews(prs[0])
	require.NoError(t, err)
	require.Len(t, reviews, 4)

	// dave approved then withdrew his approval: no review of his remains.
	for _, r := range reviews {
		assert.NotEqual(t, "dave", r.ReviewerName)
	}

	approved := reviews[0]
	assert.Equal(t, base.ReviewStateApproved, approved.State)
	assert.Equal(t, int64(7), approved.ReviewerID)
	assert.Equal(t, "bob", approved.ReviewerName)
	assert.Equal(t, int64(1704200000000), approved.CreatedAt.UnixMilli())

	// A whole inline thread becomes a single review; each comment keeps its own author.
	inline := reviews[1]
	assert.Equal(t, base.ReviewStateCommented, inline.State)
	assert.Equal(t, "carol", inline.ReviewerName)
	require.Len(t, inline.Comments, 2)
	assert.Equal(t, "This range looks wrong", inline.Comments[0].Content)
	assert.Equal(t, "carol", inline.Comments[0].PosterName)
	assert.Equal(t, int64(8), inline.Comments[0].PosterID)
	assert.Equal(t, "src/main.go", inline.Comments[0].TreePath)
	assert.Equal(t, "0123456789012345678901234567890123456789", inline.Comments[0].CommitID)
	// The Bitbucket range 10-12 is anchored on its first line with 2 extra lines.
	assert.Equal(t, 10, inline.Comments[0].Line)
	assert.Equal(t, int64(2), inline.Comments[0].ExtraLinesCount)
	assert.Equal(t, "@@ -10 +10 @@", inline.Comments[0].DiffHunk)
	assert.Equal(t, "Fixed in the next commit", inline.Comments[1].Content)
	assert.Equal(t, "alice", inline.Comments[1].PosterName)
	assert.Equal(t, int64(9), inline.Comments[1].PosterID)

	// A comment on the old side of the diff maps to a negative line.
	oldSide := reviews[2]
	require.Len(t, oldSide.Comments, 1)
	assert.Equal(t, "bob", oldSide.ReviewerName)
	assert.Equal(t, "old.go", oldSide.Comments[0].TreePath)
	assert.Equal(t, -3, oldSide.Comments[0].Line)
	assert.Equal(t, int64(0), oldSide.Comments[0].ExtraLinesCount)
	assert.Equal(t, "@@ -3 +3 @@", oldSide.Comments[0].DiffHunk)

	needsWork := reviews[3]
	assert.Equal(t, base.ReviewStateChangesRequested, needsWork.State)
	assert.Equal(t, "carol", needsWork.ReviewerName)
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
