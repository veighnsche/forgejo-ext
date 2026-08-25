// Copyright 2024 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package federation

import (
	"net/http"

	"forgejo.org/modules/log"

	ap "github.com/go-ap/activitypub"
)

func processPersonInboxAccept(activity *ap.Activity) (ServiceResult, error) {
	// Accept(Follow) completes the follow handshake; Accept(Offer) completes
	// a federated pull request handshake. Both are acknowledged and dropped,
	// since Forgejo does not track pending outbound requests locally.
	if activity.Object.GetType() != ap.FollowType && activity.Object.GetType() != ap.OfferType {
		log.Error("Invalid object type for Accept activity: %v", activity.Object.GetType())
		return ServiceResult{}, NewErrNotAcceptablef("invalid object type for Accept activity: %v", activity.Object.GetType())
	}

	// We currently do not do anything here, we just drop it.
	return NewServiceResultStatusOnly(http.StatusNoContent), nil
}
