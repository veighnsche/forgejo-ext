// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package feed

import (
	"time"

	activities_model "forgejo.org/models/activities"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/services/context"

	"github.com/gorilla/feeds"
)

// ShowIssueFeed shows comment activity on the issue as RSS / Atom feed
func ShowIssueFeed(ctx *context.Context, issue *issues_model.Issue, formatType string) {
	actions, _, err := activities_model.GetFeeds(ctx, activities_model.GetFeedsOptions{
		OnlyPerformedByActor: true,
		RequestedIssue:       issue,
		Actor:                ctx.Doer,
		IncludePrivate:       true,
		Date:                 ctx.FormString("date"),
	})
	if err != nil {
		ctx.ServerError("GetFeeds", err)
		return
	}

	feed := &feeds.Feed{
		Title:       ctx.Locale.TrString("home.feed_of", issue.Title),
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
