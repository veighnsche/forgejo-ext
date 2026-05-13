// Copyright 2025 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"forgejo.org/modules/test"
	"github.com/stretchr/testify/assert"
)

func TestIssueFeeds(t *testing.T) {
	onApplicationRun(t, func(t *testing.T, u *url.URL) {
		session := loginUser(t, "user2")
		issueURL := testNewIssue(t, session, "user2", "repo1", "Title", "Description")

		t.Run("Get RSS feed", func(t *testing.T) {
			req := NewRequest(t, "GET", fmt.Sprintf("%s.rss", issueURL))
			resp := session.MakeRequest(t, req, http.StatusOK)

			println("html:")
			println(resp.Body.String())

			htmlDoc := NewHTMLParser(t, resp.Body)
			itemCount := htmlDoc.doc.Find("channel item").Length()

			assert.Equal(t, itemCount, 1)
		})

		t.Run("Get Atom feed", func(t *testing.T) {
			req := NewRequest(t, "GET", fmt.Sprintf("%s.atom", issueURL))
			resp := session.MakeRequest(t, req, http.StatusOK)

			htmlDoc := NewHTMLParser(t, resp.Body)
			itemCount := htmlDoc.doc.Find("entry").Length()

			assert.Equal(t, itemCount, 1)
		})
	})
}

func TestPullFeeds(t *testing.T) {
	onApplicationRun(t, func(t *testing.T, u *url.URL) {
		session := loginUser(t, "user1")
		testRepoFork(t, session, "user2", "repo1", "user1", "repo1")
		testEditFile(t, session, "user1", "repo1", "master", "README.md", "Hello, World (Edited)\n")

		resp := testPullCreate(t, session, "user1", "repo1", false, "master", "master", "This is a pull title")
		url := test.RedirectURL(resp)

		t.Run("Get RSS feed", func(t *testing.T) {
			req := NewRequest(t, "GET", fmt.Sprintf("%s.rss", url))
			resp := session.MakeRequest(t, req, http.StatusOK)

			htmlDoc := NewHTMLParser(t, resp.Body)
			itemCount := htmlDoc.doc.Find("channel item").Length()

			assert.Equal(t, itemCount, 1)
		})

		t.Run("Get Atom feed", func(t *testing.T) {
			req := NewRequest(t, "GET", fmt.Sprintf("%s.atom", url))
			resp := session.MakeRequest(t, req, http.StatusOK)

			htmlDoc := NewHTMLParser(t, resp.Body)
			itemCount := htmlDoc.doc.Find("entry").Length()

			assert.Equal(t, itemCount, 1)
		})
	})
}
