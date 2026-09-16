// Copyright 2022 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package activitypub

import (
	"fmt"
	"net/http"
	"net/url"

	"forgejo.org/models/activities"
	fedhost "forgejo.org/models/forgefed"
	"forgejo.org/models/user"
	"forgejo.org/modules/activitypub"
	"forgejo.org/modules/forgefed"
	"forgejo.org/modules/log"
	"forgejo.org/modules/web"
	"forgejo.org/routers/api/v1/utils"
	"forgejo.org/services/context"
	app_context "forgejo.org/services/context"
	"forgejo.org/services/convert"
	"forgejo.org/services/federation"

	"github.com/42wim/httpsig"
	ap "github.com/go-ap/activitypub"
	"github.com/go-ap/jsonld"
)

// Person function returns the Person actor for a user
func Person(ctx *context.APIContext) {
	// swagger:operation GET /activitypub/user-id/{user-id} activitypub activitypubPerson
	// ---
	// summary: Returns the Person actor for a user
	// produces:
	// - application/json
	// parameters:
	// - name: user-id
	//   in: path
	//   description: user ID of the user
	//   type: integer
	//   format: int64
	//   required: true
	// responses:
	//   "200":
	//     "$ref": "#/responses/ActivityPub"

	person, err := convert.ToActivityPubPerson(ctx, ctx.User())
	if err != nil {
		ctx.ServerError("convert.ToActivityPubPerson", err)
		return
	}

	binary, err := jsonld.WithContext(jsonld.IRI(ap.ActivityBaseURI), jsonld.IRI(ap.SecurityContextURI)).Marshal(person)
	if err != nil {
		ctx.ServerError("MarshalJSON", err)
		return
	}
	ctx.Resp.Header().Add("Content-Type", activitypub.ActivityStreamsContentType)
	ctx.Resp.WriteHeader(http.StatusOK)
	if _, err = ctx.Resp.Write(binary); err != nil {
		log.Error("write to resp err: %v", err)
	}
}

// PersonInbox function handles the incoming data for a user inbox
func PersonInbox(ctx *context.APIContext) {
	// swagger:operation POST /activitypub/user-id/{user-id}/inbox activitypub activitypubPersonInbox
	// ---
	// summary: Send to the inbox
	// produces:
	// - application/json
	// parameters:
	// - name: user-id
	//   in: path
	//   description: user ID of the user
	//   type: integer
	//   format: int64
	//   required: true
	// responses:
	//   "202":
	//     "$ref": "#/responses/empty"

	form := web.GetForm(ctx)
	activity := form.(*ap.Activity)
	keyID := getKeyID(*ctx)

	err := verifyKeyIDMatchesActorID(*ctx, keyID, activity)
	if err != nil {
		ctx.Error(http.StatusNotAcceptable, "For keyID match failed: %v", err)
		return
	}

	result, err := federation.ProcessPersonInbox(ctx, ctx.User(), activity, keyID)
	if err != nil {
		ctx.Error(federation.HTTPStatus(err), "PersonInbox", err)
		return
	}
	responseServiceResult(ctx, result)
}

// PersonFeed returns the recorded activities in the user's feed
func PersonFeed(ctx *context.APIContext) {
	// swagger:operation GET /activitypub/user-id/{user-id}/outbox activitypub activitypubPersonFeed
	// ---
	// summary: List the user's recorded activity
	// produces:
	// - application/json
	// parameters:
	// - name: user-id
	//   in: path
	//   description: user ID of the user
	//   type: integer
	//   required: true
	// responses:
	//   "200":
	//     "$ref": "#/responses/Outbox"
	//   "403":
	//     "$ref": "#/responses/forbidden"

	listOptions := utils.GetListOptions(ctx)
	opts := activities.GetFollowingFeedsOptions{
		ListOptions: listOptions,
	}
	items, count, err := activities.GetFollowingFeeds(ctx, ctx.User().ID, opts)
	if err != nil {
		ctx.Error(http.StatusInternalServerError, "GetFollowingFeeds", err)
		return
	}
	ctx.SetTotalCountHeader(count)

	feed := ap.OrderedCollectionNew(ap.IRI(ctx.User().APActorID() + "/outbox"))
	feed.AttributedTo = ap.IRI(ctx.User().APActorID())
	for _, item := range items {
		if err := feed.OrderedItems.Append(convert.ToActivityPubPersonFeedItem(item)); err != nil {
			ctx.Error(http.StatusInternalServerError, "OrderedItems.Append", err)
			return
		}
	}

	binary, err := jsonld.WithContext(jsonld.IRI(ap.ActivityBaseURI), jsonld.IRI(ap.SecurityContextURI)).Marshal(feed)
	if err != nil {
		ctx.ServerError("MarshalJSON", err)
		return
	}

	ctx.Resp.Header().Add("Content-Type", activitypub.ActivityStreamsContentType)
	ctx.Resp.WriteHeader(http.StatusOK)
	if _, err = ctx.Resp.Write(binary); err != nil {
		log.Error("write to resp err: %v", err)
	}
}

func getActivity(ctx *context.APIContext, id int64) (*forgefed.ForgeUserActivity, error) {
	action, err := activities.GetActivityByID(ctx, id)
	if err != nil {
		ctx.Error(http.StatusInternalServerError, "GetActivityByID", err.Error())
		return nil, err
	}

	if action.UserID != action.ActUserID || action.ActUserID != ctx.User().ID {
		ctx.NotFound()
		return nil, err
	}

	private, err := action.IsActionPrivate(ctx)
	if err != nil {
		ctx.Error(http.StatusInternalServerError, "action.IsActionPrivate", err.Error())
		return nil, err
	}

	if private {
		ctx.NotFound()
		return nil, activities.ErrActivityPrivate{}
	}

	actions := activities.ActionList{action}
	if err := actions.LoadAttributes(ctx); err != nil {
		ctx.Error(http.StatusInternalServerError, "action.LoadAttributes", err.Error())
		return nil, err
	}

	activity, err := convert.ActionToForgeUserActivity(ctx, actions[0])
	if err != nil {
		ctx.Error(http.StatusInternalServerError, "ActionToForgeUserActivity", err.Error())
		return nil, err
	}

	return &activity, nil
}

// PersonActivity returns a user's given activity
func PersonActivity(ctx *context.APIContext) {
	// swagger:operation GET /activitypub/user-id/{user-id}/activities/{activity-id}/activity activitypub activitypubPersonActivity
	// ---
	// summary: Get a specific activity of the user
	// produces:
	// - application/json
	// parameters:
	// - name: user-id
	//   in: path
	//   description: user ID of the user
	//   type: integer
	//   required: true
	// - name: activity-id
	//   in: path
	//   description: activity ID of the sought activity
	//   type: integer
	//   required: true
	// responses:
	//   "200":
	//     "$ref": "#/responses/ActivityPub"

	id := ctx.ParamsInt64("activity-id")
	activity, err := getActivity(ctx, id)
	if err != nil {
		return
	}

	binary, err := jsonld.WithContext(jsonld.IRI(ap.ActivityBaseURI), jsonld.IRI(ap.SecurityContextURI)).Marshal(activity)
	if err != nil {
		ctx.ServerError("MarshalJSON", err)
		return
	}
	ctx.Resp.Header().Add("Content-Type", activitypub.ActivityStreamsContentType)
	ctx.Resp.WriteHeader(http.StatusOK)
	if _, err = ctx.Resp.Write(binary); err != nil {
		log.Error("write to resp err: %v", err)
	}
}

// PersonActivity returns the Object part of a user's given activity
func PersonActivityNote(ctx *context.APIContext) {
	// swagger:operation GET /activitypub/user-id/{user-id}/activities/{activity-id} activitypub activitypubPersonActivityNote
	// ---
	// summary: Get a specific activity object of the user
	// produces:
	// - application/json
	// parameters:
	// - name: user-id
	//   in: path
	//   description: user ID of the user
	//   type: integer
	//   required: true
	// - name: activity-id
	//   in: path
	//   description: activity ID of the sought activity
	//   type: integer
	//   required: true
	// responses:
	//   "200":
	//     "$ref": "#/responses/ActivityPub"

	id := ctx.ParamsInt64("activity-id")
	activity, err := getActivity(ctx, id)
	if err != nil {
		return
	}

	binary, err := jsonld.WithContext(jsonld.IRI(ap.ActivityBaseURI), jsonld.IRI(ap.SecurityContextURI)).Marshal(activity.Object)
	if err != nil {
		ctx.ServerError("MarshalJSON", err)
		return
	}
	ctx.Resp.Header().Add("Content-Type", activitypub.ActivityStreamsContentType)
	ctx.Resp.WriteHeader(http.StatusOK)
	if _, err = ctx.Resp.Write(binary); err != nil {
		log.Error("write to resp err: %v", err)
	}
}

func getKeyID(ctx app_context.APIContext) string {
	r := ctx.Req
	v, err := httpsig.NewVerifier(r)

	if err != nil {
		log.Debug("For %q verification failed: %v", r.URL.Path, err)
	}
	keyURI := v.KeyId()

	return keyURI
}

func verifyKeyIDMatchesActorID(ctx app_context.APIContext, keyID string, activity *ap.Activity) error {
	keyURL, err := url.Parse(keyID)
	if err != nil {
		return err
	}

	actorURI := activity.Actor.GetLink().String()
	_, federatedUser, federationHost, err := federation.FindOrCreateFederatedUser(ctx, actorURI)

	_, keyUser, err := user.FindFederatedUserByKeyID(ctx, keyURL.String())
	if err != nil {

		if !user.IsErrFederatedUserNotExists(err) {
			return err
		}

		// Check for existing federation host key
		keyHost, err := fedhost.FindFederationHostByKeyID(ctx, keyURL.String())
		if err != nil {
			if !fedhost.IsErrFederationHostNotFound(err) {
				return err
			}
		} else {
			if federationHost.ID != keyHost.ID {
				return fmt.Errorf("KeyID (%v) in signature does not match FederationHost ID (%v)", keyHost.ID, federationHost.ID)
			}
		}
	} else {
		if federatedUser.ID != keyUser.ID {
			return fmt.Errorf("KeyID (%v) in signature does not match FederatedUser ID (%v)", keyUser.ID, federatedUser.ID)
		}
	}
	return nil
}
