// Copyright 2023, 2024 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package activitypub

import (
	"fmt"
	"net/http"
	"strings"

	"forgejo.org/models/db"
	perm_model "forgejo.org/models/perm"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/activitypub"
	"forgejo.org/modules/forgefed"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/web"
	"forgejo.org/services/context"
	"forgejo.org/services/federation"

	ap "github.com/go-ap/activitypub"
	"github.com/go-ap/jsonld"
)

// Repository function returns the Repository actor for a repo
func Repository(ctx *context.APIContext) {
	if !requireRepoReadAccess(ctx) {
		return
	}

	// swagger:operation GET /activitypub/repository-id/{repository-id} activitypub activitypubRepository
	// ---
	// summary: Returns the Repository actor for a repo
	// produces:
	// - application/json
	// parameters:
	// - name: repository-id
	//   in: path
	//   description: repository ID of the repo
	//   type: integer
	//   format: int64
	//   required: true
	// responses:
	//   "200":
	//     "$ref": "#/responses/ActivityPub"

	link := fmt.Sprintf("%s/api/v1/activitypub/repository-id/%d", strings.TrimSuffix(setting.AppURL, "/"), ctx.Repo().Repository.ID)
	repo := forgefed.RepositoryNew(ap.IRI(link))

	repo.Inbox = ap.IRI(link + "/inbox")
	repo.Outbox = ap.IRI(link + "/outbox")
	repo.Followers = ap.IRI(link + "/followers")

	repo.Name = ap.NaturalLanguageValuesNew()
	err := repo.Name.Set(ap.NilLangRef, ap.Content(ctx.Repo().Repository.Name))
	if err != nil {
		ctx.Error(http.StatusInternalServerError, "Set Name", err)
		return
	}

	// Expose the native git endpoints via the ForgeFed cloneUri/pushUri
	// properties so remote instances can clone this repository over plain git
	// (used by federated pull mirrors and federated pull requests).
	cloneLink := ctx.Repo().Repository.CloneLink()
	repo.CloneURI = cloneLink.HTTPS
	repo.PushURI = ap.ItemCollection{
		ap.IRI(cloneLink.HTTPS),
		ap.IRI(cloneLink.SSH),
	}

	// Expose the repository owner's key as the repository actor's public key:
	// repository-level activities (Like/Undo) are signed by the owner, so peers
	// need this key to verify them.
	owner := ctx.Repo().Repository.Owner
	if owner == nil {
		owner, err = user_model.GetUserByID(ctx, ctx.Repo().Repository.OwnerID)
		if err != nil {
			ctx.Error(http.StatusInternalServerError, "GetRepositoryOwner", err)
			return
		}
	}
	publicKeyPem, err := activitypub.GetPublicKey(ctx, owner)
	if err != nil {
		ctx.Error(http.StatusInternalServerError, "GetPublicKey", err)
		return
	}
	repo.PublicKey.ID = ap.IRI(link + "#main-key")
	repo.PublicKey.Owner = ap.IRI(link)
	repo.PublicKey.PublicKeyPem = publicKeyPem

	populateForgeFedRepositoryFields(ctx, ctx.Repo().Repository, repo)

	response(ctx, repo)
}

// PersonInbox function handles the incoming data for a repository inbox
func RepositoryInbox(ctx *context.APIContext) {
	if !requireRepoReadAccess(ctx) {
		return
	}

	// swagger:operation POST /activitypub/repository-id/{repository-id}/inbox activitypub activitypubRepositoryInbox
	// ---
	// summary: Send to the inbox
	// produces:
	// - application/json
	// parameters:
	// - name: repository-id
	//   in: path
	//   description: repository ID of the repo
	//   type: integer
	//   format: int64
	//   required: true
	// - name: body
	//   in: body
	//   schema:
	//     "$ref": "#/definitions/ForgeLike"
	// responses:
	//   "204":
	//     "$ref": "#/responses/empty"

	repository := ctx.Repo().Repository
	form := web.GetForm(ctx)
	activity := form.(*ap.Activity)
	if err := federation.VerifyRepositoryActivityActor(ctx, ctx.FederationPrincipal(), activity); err != nil {
		log.Warn("RepositoryInbox: %v", err)
		ctx.Error(http.StatusForbidden, "RepositoryInbox", "activity actor does not match the verified signature")
		return
	}
	result, err := federation.ProcessRepositoryInbox(ctx, activity, repository.ID)
	if err != nil {
		log.Error("Processing Repository Inbox failed: %v", err)
		ctx.Error(federation.HTTPStatus(err), "Processing Repository Inbox failed", err)
		return
	}
	responseServiceResult(ctx, result)
}

func RepositoryOutbox(ctx *context.APIContext) {
	if !requireRepoReadAccess(ctx) {
		return
	}

	// swagger:operation POST /activitypub/repository-id/{repository-id}/outbox activitypub activitypubRepositoryOutbox
	// ---
	// summary: Display the outbox
	// produces:
	// - application/ld+json
	// parameters:
	// - name: repository-id
	//   in: path
	//   description: repository ID of the repo
	//   type: integer
	//   format: int64
	//   required: true
	// responses:
	//   "200":
	//     "$ref": "#/responses/Outbox"

	repository := ctx.Repo().Repository
	outbox := ap.OrderedCollectionNew(ap.IRI(repository.APActorID() + "/outbox"))

	binary, err := jsonld.WithContext(
		jsonld.IRI(ap.ActivityBaseURI),
	).Marshal(outbox)
	if err != nil {
		ctx.ServerError("MarshalJSON", err)
		return
	}

	ctx.Resp.Header().Add("Content-Type", activitypub.ActivityStreamsContentType)
	ctx.Resp.WriteHeader(http.StatusOK)

	_, err = ctx.Resp.Write(binary)
	if err != nil {
		log.Error("write to resp err: %s", err)
	}
}

func RepositoryFollowers(ctx *context.APIContext) {
	if !requireRepoReadAccess(ctx) {
		return
	}

	// swagger:operation GET /activitypub/repository-id/{repository-id}/followers activitypub activitypubRepositoryFollowers
	// ---
	// summary: Returns the followers collection of a Repository actor
	// produces:
	// - application/ld+json
	// parameters:
	// - name: repository-id
	//   in: path
	//   description: ID of the repository
	//   type: integer
	//   format: int64
	//   required: true
	// responses:
	//   "200":
	//     "$ref": "#/responses/ActivityPub"

	repository := ctx.Repo().Repository
	followers, err := repo_model.GetFederatedRepoFollowersByRepoID(ctx, repository.ID)
	if err != nil {
		ctx.ServerError("GetFederatedRepoFollowersByRepoID", err)
		return
	}

	collection := ap.OrderedCollectionNew(ap.IRI(repository.APActorID() + "/followers"))
	collection.TotalItems = uint(len(followers))
	for _, follower := range followers {
		_, federatedUser, err := user_model.GetFederatedUserByUserID(ctx, follower.UserID)
		if err != nil {
			log.Warn("Unable to resolve federated repo follower %d: %v", follower.UserID, err)
			continue
		}
		if err := collection.OrderedItems.Append(ap.IRI(federatedUser.NormalizedOriginalURL)); err != nil {
			ctx.ServerError("OrderedItems.Append", err)
			return
		}
	}

	binary, err := jsonld.WithContext(jsonld.IRI(ap.ActivityBaseURI)).Marshal(collection)
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

// populateForgeFedRepositoryFields fills the ForgeFed-specific repository
// actor properties: forkedFrom (the upstream repository when this repo is a
// fork), forks (the collection of repositories forked from this one) and
// team (the actors with management/push access). Failures to load any of
// these are non-fatal: the actor is still served with the fields that could
// be resolved.
func populateForgeFedRepositoryFields(ctx *context.APIContext, repository *repo_model.Repository, repo *forgefed.Repository) {
	// forkedFrom: the base repository this repository was created from.
	if repository.IsFork {
		if err := repository.GetBaseRepo(ctx); err == nil && repository.BaseRepo != nil {
			repo.ForkedFrom = ap.IRI(repository.BaseRepo.APActorID())
		} else if err != nil {
			log.Warn("Unable to resolve base repository for fork %d: %v", repository.ID, err)
		}
	}

	// forks: the repositories created as forks of this one.
	forks, err := repo_model.GetRepositoriesByForkID(ctx, repository.ID)
	if err != nil {
		log.Warn("Unable to resolve forks for repository %d: %v", repository.ID, err)
	} else if len(forks) > 0 {
		items := make(ap.ItemCollection, 0, len(forks))
		for _, fork := range forks {
			items = append(items, ap.IRI(fork.APActorID()))
		}
		repo.Forks = items
	}

	// team: the actors with management/push access (owner + collaborators).
	owner := repository.Owner
	if owner == nil {
		owner, err = user_model.GetUserByID(ctx, repository.OwnerID)
		if err != nil {
			log.Warn("Unable to resolve owner for repository %d: %v", repository.ID, err)
			return
		}
	}
	collaborators, err := repo_model.GetCollaborators(ctx, repository.ID, db.ListOptions{})
	if err != nil {
		log.Warn("Unable to resolve collaborators for repository %d: %v", repository.ID, err)
	} else {
		items := make(ap.ItemCollection, 0, 1+len(collaborators))
		items = append(items, ap.IRI(owner.APActorID()))
		for _, collaborator := range collaborators {
			if collaborator.Collaboration != nil && collaborator.Collaboration.Mode >= perm_model.AccessModeWrite {
				items = append(items, ap.IRI(collaborator.APActorID()))
			}
		}
		repo.Team = items
	}

	// mirrors/mirrorsTo: the federation-side mirror relationships. A local pull
	// mirror declares the remote repository it mirrors via `mirrors`; a local
	// push mirror declares its target via `mirrorsTo`.
	if federatedMirror, err := repo_model.GetFederatedMirrorByRepoID(ctx, repository.ID); err == nil {
		if federatedMirror.IsPush {
			repo.MirrorsTo = ap.IRI(federatedMirror.RemoteActorURI)
		} else {
			repo.Mirrors = ap.IRI(federatedMirror.RemoteActorURI)
		}
	} else if !repo_model.IsErrFederatedMirrorNotExists(err) {
		log.Warn("Unable to resolve federated mirror for repository %d: %v", repository.ID, err)
	}
}

// requireRepoReadAccess enforces the visibility of the repository on
// federation endpoints: public repositories are served to any verified
// federation peer, private repositories only to authenticated identities
// (local users or federation principals) with read access. Private
// repositories the requester cannot see are reported as not found, to
// avoid leaking their existence.
func requireRepoReadAccess(ctx *context.APIContext) bool {
	repo := ctx.Repo().Repository
	if !repo.IsPrivate {
		return true
	}
	if hasRepoReadAccess(ctx, ctx.Doer()) || hasRepoReadAccess(ctx, federationPrincipalUser(ctx)) {
		return true
	}
	ctx.NotFound()
	return false
}

func federationPrincipalUser(ctx *context.APIContext) *user_model.User {
	if principal := ctx.FederationPrincipal(); principal != nil {
		return principal.User
	}
	return nil
}

func hasRepoReadAccess(ctx *context.APIContext, user *user_model.User) bool {
	if user == nil {
		return false
	}
	userPerm, err := access_model.GetUserRepoPermission(ctx, ctx.Repo().Repository, user)
	if err != nil {
		log.Error("Failed to compute repository access for user %d on repository %d: %v", user.ID, ctx.Repo().Repository.ID, err)
		return false
	}
	return userPerm.AccessMode >= perm_model.AccessModeRead
}
