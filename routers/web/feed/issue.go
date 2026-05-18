// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package feed

import (
	"fmt"
	"time"

	activities_model "forgejo.org/models/activities"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/services/context"

	"github.com/gorilla/feeds"
)

// ShowIssueFeed shows comment activity on the issue as RSS / Atom feed
func ShowIssueFeed(ctx *context.Context, issue *issues_model.Issue, formatType string) {
	actions, _, err := activities_model.GetFeeds(ctx, activities_model.GetFeedsOptions{
		RequestedIssue: issue,
		Actor:          ctx.Doer,
		IncludePrivate: true,
		Date:           ctx.FormString("date"),
	})
	if err != nil {
		ctx.ServerError("GetFeeds", err)
		return
	}

	kind := "Issue"

	if issue.IsPull {
		kind = "Pull request"
	}

	title := fmt.Sprintf("%s/%s: %s #%d - %s", issue.Repo.OwnerName, issue.Repo.Name, kind, issue.Index, issue.Title)

	feed := &feeds.Feed{
		Title:       ctx.Locale.TrString("home.feed_of", title),
		Link:        &feeds.Link{Href: issue.HTMLURL()},
		Description: issue.Content,
		Created:     time.Now(),
	}

	feed.Items, err = feedActionsToFeedItems(ctx, actions)
	if err != nil {
		ctx.ServerError("convert feed", err)
		return
	}

	writeFeed(ctx, feed, formatType)
}
