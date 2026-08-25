// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package federation

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"forgejo.org/models/moderation"
	"forgejo.org/modules/log"

	ap "github.com/go-ap/activitypub"
)

// processFlagActivity handles an inbound Flag activity. Mastodon and other
// Fediverse software use Flag to report abusive content to the instance that
// hosts it. Forgejo maps each flagged local object (user or repository actor)
// to an abuse report that admins can review in the moderation UI.
//
// The Flag activity shape (per Mastodon):
//
//	{ "type": "Flag", "actor": "<remote reporter>",
//	  "object": ["<local user actor>", "<local repo actor>", ...],
//	  "content": "<reason>" }
func processFlagActivity(ctx context.Context, activity *ap.Activity) (ServiceResult, error) {
	if activity.Type != ap.FlagType {
		return ServiceResult{}, NewErrNotAcceptablef("not a flag activity: %v", activity.Type)
	}

	// Rate limit inbound reports per actor so a misbehaving peer cannot
	// flood the moderation queue (see flagRateLimiter).
	if flagRateLimiter.Allow(activity.Actor.GetID().String()) {
		return ServiceResult{}, NewErrNotAcceptablef("flag rate limit exceeded")
	}

	// Resolve (or materialise) the remote reporter.
	actorURI := activity.Actor.GetID().String()
	reporter, _, _, err := FindOrCreateFederatedUser(ctx, actorURI)
	if err != nil {
		log.Error("Federated user not found (%s): %v", actorURI, err)
		return ServiceResult{}, NewErrNotAcceptablef("federated user not found (%s): %v", actorURI, err)
	}

	remarks := strings.TrimSpace(activity.Content.String())
	if len(remarks) > 500 {
		remarks = remarks[:500]
	}

	reports := 0
	for _, objIRI := range flagObjectIRIs(activity.Object) {
		contentType, contentID, err := mapFlaggedObject(objIRI)
		if err != nil {
			log.Warn("Ignoring flag object %q: %v", objIRI, err)
			continue
		}
		if err := moderation.ReportAbuse(ctx, &moderation.AbuseReport{
			ReporterID:  reporter.ID,
			ContentType: contentType,
			ContentID:   contentID,
			Category:    moderation.AbuseCategoryTypeOther,
			Remarks:     remarks,
		}); err != nil {
			log.Warn("Unable to record flag report for object %q: %v", objIRI, err)
			continue
		}
		reports++
	}

	if reports == 0 {
		return ServiceResult{}, NewErrNotAcceptablef("no local content could be matched from the flag objects")
	}
	return NewServiceResultStatusOnly(http.StatusNoContent), nil
}

// flagObjectIRIs extracts the IRI(s) from a Flag's object, which may be a
// single IRI, a single object, or a collection of IRIs.
func flagObjectIRIs(object ap.Item) []string {
	switch obj := object.(type) {
	case nil:
		return nil
	case ap.ItemCollection:
		result := make([]string, 0, len(obj))
		for _, item := range obj {
			if item != nil {
				result = append(result, item.GetLink().String())
			}
		}
		return result
	default:
		if link := object.GetLink(); link != "" {
			return []string{link.String()}
		}
		return nil
	}
}

// mapFlaggedObject maps a local ActivityPub actor IRI to the content type and
// ID used by the abuse-report system. Only local actor path patterns are
// recognised; the host part is not used for security (any peer can reference
// a local content ID), which also keeps localhost/127.0.0.1 dev setups working.
func mapFlaggedObject(objIRI string) (moderation.ReportedContentType, int64, error) {
	if objIRI == "" {
		return 0, 0, errInvalidFlagObject("empty object IRI")
	}

	u, err := url.Parse(objIRI)
	if err != nil {
		return 0, 0, errInvalidFlagObject(err.Error())
	}

	path := strings.TrimPrefix(u.Path, "/")
	parts := strings.Split(path, "/")

	// /api/v1/activitypub/user-id/{id}
	if len(parts) == 5 && parts[0] == "api" && parts[1] == "v1" &&
		parts[2] == "activitypub" && parts[3] == "user-id" {
		if id, err := strconv.ParseInt(parts[4], 10, 64); err == nil {
			return moderation.ReportedContentTypeUser, id, nil
		}
	}

	// /api/v1/activitypub/repository-id/{id}
	if len(parts) == 5 && parts[0] == "api" && parts[1] == "v1" &&
		parts[2] == "activitypub" && parts[3] == "repository-id" {
		if id, err := strconv.ParseInt(parts[4], 10, 64); err == nil {
			return moderation.ReportedContentTypeRepository, id, nil
		}
	}

	return 0, 0, errInvalidFlagObject("unrecognised object IRI: " + objIRI)
}

type errInvalidFlagObject string

func (e errInvalidFlagObject) Error() string { return string(e) }
