// Copyright 2024 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"strings"
	"testing"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/tests"
	"forgejo.org/tests/forgery"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHeadMentionCSS(t *testing.T) {
	userSession := loginUser(t, "user2")
	resp := userSession.MakeRequest(t, NewRequest(t, "GET", "/"), http.StatusOK)
	assert.Contains(t, resp.Body.String(), `.mention[href="/user2" i]`)

	guestSession := emptyTestSession(t)
	resp = guestSession.MakeRequest(t, NewRequest(t, "GET", "/"), http.StatusOK)
	assert.NotContains(t, resp.Body.String(), `.mention[href="`)
}

func TestHeadMentionJS(t *testing.T) {
	// after template escaping, name and pronouns should not contain a single-quote (') character:
	fullNames := [][3]string{
		{"normal display name", "nice user", `nice user`},
		{"display name with HTML characters", "   < U<se>r Tw<o > ><  ", `   \u003c U\u003cse\u003er Tw\u003co \u003e \u003e\u003c  `},
		{"display name with JS characters", "nice user']}],]).values()),}; alert('oops!')", `nice user\u0027]}],]).values()),}; alert(\u0027oops!\u0027)`},
	}
	pronouns := [][3]string{
		{"normal pronouns", "he/him", ` (he\/him)`},
		{"pronouns with HTML characters", "   < U<se>r Tw<o > ><  ", ` (   \u003c U\u003cse\u003er Tw\u003co \u003e \u003e\u003c  )`},
		{"pronouns with JS characters", "he/him']}],]).values()),}; alert('oops!')", ` (he\/him\u0027]}],]).values()),}; alert(\u0027oops!\u0027))`},
	}

	user := forgery.CreateUser(t, nil)
	userSession := loginUser(t, user.Name)
	token := getTokenForLoggedInUser(t, userSession, auth_model.AccessTokenScopeWriteUser)

	repo := forgery.CreateRepository(t, user, nil)
	issue := forgery.CreateIssue(t, user, repo, "test issue", "test issue content")

	for _, n := range fullNames {
		fullNameTestName := n[0]
		fullNamePref := n[1]
		escapedFullName := n[2]

		for _, p := range pronouns {
			pronounsTestName := p[0]
			pronounsPref := p[1]
			escapedPronouns := p[2]

			t.Run(fullNameTestName+" and "+pronounsTestName+" do not break out of mention cache", func(t *testing.T) {
				defer tests.PrintCurrentTest(t)()

				// set the user's display name
				req := NewRequestWithValues(t, "PATCH", "/api/v1/user/settings", map[string]string{
					"full_name": fullNamePref,
					"pronouns": pronounsPref,
				}).AddTokenAuth(token)
				userSession.MakeRequest(t, req, http.StatusOK)

				// visit a page with mentionValues
				resp := userSession.MakeRequest(t, NewRequest(t, "GET", issue.HTMLURL()), http.StatusOK)
				htmlDoc := NewHTMLParser(t, resp.Body)

				// check for expected values (breakout prevented by escaping single-quote characters)
				headScript := htmlDoc.Find("head > script").FilterFunction(func(i int, s *goquery.Selection) bool {
					return strings.Contains(s.Text(), "window.config =")
				})
				assert.Equal(t, 1, headScript.Length())

				_, after, found := strings.Cut(headScript.Text(), "mentionValues: Array.from(new Map([")
				require.True(t, found)
				mentionValues, _, found := strings.Cut(after, "]).values()),\n")
				require.True(t, found)

				// only the current user should be mentionable so far, but may be listed multiple times
				for mentionValue := range strings.SplitSeq(mentionValues, "\n") {
					trimmed := strings.TrimSpace(mentionValue)
					if len(trimmed) == 0 {
						continue
					}
					assert.Equal(t, `['`+user.Name+`', {key: '`+user.Name+` `+escapedFullName+`', value: '`+user.Name+`', name: '`+user.Name+`', fullname: '`+escapedFullName+escapedPronouns+`', avatar: 'http:\/\/localhost:3003\/avatars\/`+user.Avatar+`'}],`, trimmed)
				}
			})
		}
	}
}
