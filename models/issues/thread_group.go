// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package issues

import (
	"context"
	"sort"

	"forgejo.org/models/db"
)

// CodeThread is one native code-review conversation: code comments sharing
// one (review, path, signed line) anchor in native (created_unix, id)
// order with the anchored root first. Replies are members, never
// resolution targets: the root carries the conversation identity and its
// resolve-doer carries the resolution.
type CodeThread []*Comment

// Root reports the anchored conversation root: the first comment in
// native (created_unix, id) order. It reports nil for an empty thread.
func (t CodeThread) Root() *Comment {
	if len(t) == 0 {
		return nil
	}
	return t[0]
}

// FetchThreadComments returns every code comment on the issue in native
// (created_unix, id) order with the owning reviews joined, including
// outdated (invalidated) comments and pending-review comments the reader
// may not see. Callers decide visibility per member: a member behind a
// pending review authored by another actor stays hidden. Reads perform no
// filtering so snapshot digests and resolve bindings observe the same
// complete conversation the host resolves.
func FetchThreadComments(ctx context.Context, issue *Issue) (CommentList, map[int64]*Review, error) {
	conds := FindCommentsOptions{Type: CommentTypeCode, IssueID: issue.ID}.ToConds()
	var comments CommentList
	if err := db.GetEngine(ctx).Where(conds).
		Asc("comment.created_unix").
		Asc("comment.id").
		Find(&comments); err != nil {
		return nil, nil, err
	}
	if err := issue.LoadRepo(ctx); err != nil {
		return nil, nil, err
	}
	if err := comments.LoadPosters(ctx); err != nil {
		return nil, nil, err
	}
	if err := comments.LoadResolveDoers(ctx); err != nil {
		return nil, nil, err
	}
	ids := make([]int64, 0, len(comments))
	for _, comment := range comments {
		if comment.ReviewID != 0 {
			ids = append(ids, comment.ReviewID)
		}
	}
	reviews := make(map[int64]*Review)
	if err := db.GetEngine(ctx).In("id", ids).Find(&reviews); err != nil {
		return nil, nil, err
	}
	for _, comment := range comments {
		if re, ok := reviews[comment.ReviewID]; ok && re != nil {
			comment.Review = re
		}
	}
	return comments, reviews, nil
}

// threadGroupKey anchors one conversation: its review, tree path and
// signed line. The sign carries the diff side: negative lines anchor the
// previous (merge-base) tree, positive lines the proposed (head) tree.
type threadGroupKey struct {
	reviewID int64
	treePath string
	line     int64
}

// GroupCodeThreads groups code comments into conversations by (review,
// path, signed line), ordered by ascending root (created_unix, id) with
// members in native order. Comments without a code anchor or review
// grouping never form threads and are skipped so grouping stays total
// over anchored conversations.
func GroupCodeThreads(comments CommentList) []CodeThread {
	groups := make(map[threadGroupKey][]*Comment)
	order := make([]threadGroupKey, 0, len(comments))
	for _, comment := range comments {
		if comment == nil || comment.Type != CommentTypeCode || comment.ReviewID == 0 {
			continue
		}
		key := threadGroupKey{reviewID: comment.ReviewID, treePath: comment.TreePath, line: comment.Line}
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], comment)
	}
	threads := make([]CodeThread, 0, len(order))
	for _, key := range order {
		members := groups[key]
		sort.Slice(members, func(i, j int) bool {
			if members[i].CreatedUnix != members[j].CreatedUnix {
				return members[i].CreatedUnix < members[j].CreatedUnix
			}
			return members[i].ID < members[j].ID
		})
		threads = append(threads, CodeThread(members))
	}
	sort.Slice(threads, func(i, j int) bool {
		a, b := threads[i].Root(), threads[j].Root()
		if a.CreatedUnix != b.CreatedUnix {
			return a.CreatedUnix < b.CreatedUnix
		}
		return a.ID < b.ID
	})
	return threads
}

// FindCodeThread returns the conversation holding the named comment ID,
// or nil when no grouped thread contains it.
func FindCodeThread(threads []CodeThread, commentID int64) CodeThread {
	for _, thread := range threads {
		for _, member := range thread {
			if member != nil && member.ID == commentID {
				return thread
			}
		}
	}
	return nil
}
