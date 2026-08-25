// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"errors"
	"net/http"

	"forgejo.org/models"
	"forgejo.org/models/db"
	"forgejo.org/models/organization"
	"forgejo.org/models/perm"
	access_model "forgejo.org/models/perm/access"
	quota_model "forgejo.org/models/quota"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	api "forgejo.org/modules/structs"
	"forgejo.org/modules/web"
	"forgejo.org/services/context"
	"forgejo.org/services/convert"
	"forgejo.org/services/federationmirror"
)

// CreateFederatedMirror creates a local pull mirror of a remote federated
// repository, discovered by its ForgeFed repository actor URI. This is the
// homeserver-side entry point of the ForgeFed pull-mirror workflow: the local
// mirror follows the remote repository, and stays in sync via scheduled syncs
// and inbound Push activities.
func CreateFederatedMirror(ctx *context.APIContext) {
	// swagger:operation POST /repos/federated-mirror repository repoCreateFederatedMirror
	// ---
	// summary: Mirror a remote federated repository (pull mirror)
	// consumes:
	// - application/json
	// produces:
	// - application/json
	// parameters:
	// - name: body
	//   in: body
	//   schema:
	//     "$ref": "#/definitions/FederatedMirrorOption"
	// responses:
	//   "201":
	//     "$ref": "#/responses/Repository"
	//   "403":
	//     "$ref": "#/responses/forbidden"
	//   "409":
	//     "$ref": "#/responses/conflict"
	//   "413":
	//     "$ref": "#/responses/quotaExceeded"
	//   "422":
	//     "$ref": "#/responses/validationError"

	form := web.GetForm(ctx).(*api.FederatedMirrorOption)

	// Resolve the owner of the local mirror repository.
	var (
		repoOwner *user_model.User
		err       error
	)
	if len(form.RepoOwner) != 0 {
		repoOwner, err = user_model.GetUserByName(ctx, form.RepoOwner)
	} else {
		repoOwner = ctx.Doer()
	}
	if err != nil {
		if user_model.IsErrUserNotExist(err) {
			ctx.Error(http.StatusUnprocessableEntity, "", err)
		} else {
			ctx.Error(http.StatusInternalServerError, "GetUser", err)
		}
		return
	}

	if ctx.HasAPIError() {
		ctx.Error(http.StatusUnprocessableEntity, "", ctx.GetErrMsg())
		return
	}

	if !ctx.CheckQuota(quota_model.LimitSubjectSizeReposAll, repoOwner.ID, repoOwner.Name) {
		return
	}

	if !ctx.IsUserSiteAdmin() {
		if !repoOwner.IsOrganization() && ctx.Doer().ID != repoOwner.ID {
			ctx.Error(http.StatusForbidden, "", "Given user is not an organization.")
			return
		}

		if repoOwner.IsOrganization() {
			// Check ownership of organization.
			isOwner, err := organization.OrgFromUser(repoOwner).IsOwnedBy(ctx, ctx.Doer().ID)
			if err != nil {
				ctx.Error(http.StatusInternalServerError, "IsOwnedBy", err)
				return
			} else if !isOwner {
				ctx.Error(http.StatusForbidden, "", "Given user is not owner of organization.")
				return
			}
		}
	}

	if setting.Mirror.DisableNewPull {
		ctx.Error(http.StatusForbidden, "MirrorsGlobalDisabled", errors.New("the site administrator has disabled the creation of new pull mirrors"))
		return
	}

	repo, err := federationmirror.CreateFederatedPullMirror(ctx, ctx.Doer(), repoOwner, federationmirror.Options{
		RemoteActorURI: form.RemoteActorURI,
		RepoName:       form.RepoName,
		Description:    form.Description,
		Private:        form.Private,
		MirrorInterval: form.MirrorInterval,
	})
	if err != nil {
		handleFederatedMirrorError(ctx, err)
		return
	}

	log.Trace("Federated mirror created: %s/%s", repoOwner.Name, repo.Name)
	ctx.JSON(http.StatusCreated, convert.ToRepo(ctx, repo, access_model.Permission{AccessMode: perm.AccessModeAdmin}))
}

// CreateFederatedPushMirror sets up a push mirror from a local repository to
// a remote federated repository, discovered by its ForgeFed actor URI. The
// local repository's actor JSON declares the ForgeFed `mirrorsTo` property.
func CreateFederatedPushMirror(ctx *context.APIContext) {
	// swagger:operation POST /repos/{owner}/{repo}/federated-push-mirror repository repoCreateFederatedPushMirror
	// ---
	// summary: Mirror a local repository to a remote federated repository (push mirror)
	// consumes:
	// - application/json
	// produces:
	// - application/json
	// parameters:
	// - name: owner
	//   in: path
	//   description: owner of the repo
	//   type: string
	//   required: true
	// - name: repo
	//   in: path
	//   description: name of the repo
	//   type: string
	//   required: true
	// - name: body
	//   in: body
	//   schema:
	//     "$ref": "#/definitions/FederatedPushMirrorOption"
	// responses:
	//   "201":
	//     "$ref": "#/responses/PushMirror"
	//   "403":
	//     "$ref": "#/responses/forbidden"
	//   "422":
	//     "$ref": "#/responses/validationError"
	form := web.GetForm(ctx).(*api.FederatedPushMirrorOption)
	if ctx.HasAPIError() {
		ctx.Error(http.StatusUnprocessableEntity, "", ctx.GetErrMsg())
		return
	}

	if !setting.Mirror.Enabled {
		ctx.Error(http.StatusBadRequest, "CreateFederatedPushMirror", "Mirror feature is disabled")
		return
	}

	pushMirror, err := federationmirror.CreateFederatedPushMirror(ctx, ctx.Repo().Repository, federationmirror.PushMirrorOptions{
		RemoteActorURI: form.RemoteActorURI,
		Interval:       form.Interval,
		SyncOnCommit:   form.SyncOnCommit,
		BranchFilter:   form.BranchFilter,
		RemoteUsername: form.RemoteUsername,
		RemotePassword: form.RemotePassword,
	})
	if err != nil {
		ctx.Error(http.StatusUnprocessableEntity, "CreateFederatedPushMirror", err)
		return
	}

	apiPushMirror, err := convert.ToPushMirror(ctx, pushMirror)
	if err != nil {
		ctx.Error(http.StatusInternalServerError, "ToPushMirror", err)
		return
	}
	ctx.JSON(http.StatusCreated, apiPushMirror)
}

func handleFederatedMirrorError(ctx *context.APIContext, err error) {
	switch {
	case models.IsErrInvalidCloneAddr(err):
		ctx.Error(http.StatusUnprocessableEntity, "", err)
	case db.IsErrNameReserved(err):
		ctx.Error(http.StatusUnprocessableEntity, "", err)
	case db.IsErrNameCharsNotAllowed(err):
		ctx.Error(http.StatusUnprocessableEntity, "", err)
	case db.IsErrNamePatternNotAllowed(err):
		ctx.Error(http.StatusUnprocessableEntity, "", err)
	default:
		ctx.Error(http.StatusUnprocessableEntity, "CreateFederatedMirror", err)
	}
}
