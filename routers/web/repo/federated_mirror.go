// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"net/http"

	"forgejo.org/models/organization"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/web"
	"forgejo.org/services/context"
	"forgejo.org/services/federationmirror"
	"forgejo.org/services/forms"
)

const (
	tplFederatedMirror base.TplName = "repo/migrate/federated"
)

// FederatedMirror renders the "mirror a federated repository" page: the
// homeserver-side entry point of the ForgeFed pull-mirror workflow.
func FederatedMirror(ctx *context.Context) {
	if !setting.Federation.Enabled {
		ctx.NotFound("FederatedMirror", nil)
		return
	}
	if setting.Mirror.DisableNewPull {
		ctx.Error(http.StatusForbidden, "FederatedMirror: the site administrator has disabled the creation of new pull mirrors")
		return
	}

	setFederatedMirrorContextData(ctx)

	ctxUser := checkContextUser(ctx, ctx.FormInt64("org"))
	if ctx.Written() {
		return
	}
	ctx.Data["ContextUser"] = ctxUser
	ctx.Data["private"] = getRepoPrivate(ctx)

	ctx.HTML(http.StatusOK, tplFederatedMirror)
}

// FederatedMirrorPost creates a local pull mirror of a remote federated
// repository.
func FederatedMirrorPost(ctx *context.Context) {
	if !setting.Federation.Enabled {
		ctx.NotFound("FederatedMirror", nil)
		return
	}
	if setting.Mirror.DisableNewPull {
		ctx.Error(http.StatusForbidden, "FederatedMirrorPost: the site administrator has disabled the creation of new pull mirrors")
		return
	}

	form := web.GetForm(ctx).(*forms.FederatedMirrorForm)
	ctxUser := checkContextUser(ctx, form.UID)
	if ctx.Written() {
		return
	}
	setFederatedMirrorContextData(ctx)
	ctx.Data["ContextUser"] = ctxUser
	ctx.Data["private"] = form.Private

	if ctx.HasError() {
		ctx.HTML(http.StatusOK, tplFederatedMirror)
		return
	}

	repo, err := federationmirror.CreateFederatedPullMirror(ctx, ctx.Doer, ctxUser, federationmirror.Options{
		RemoteActorURI: form.RemoteActorURI,
		RepoName:       form.RepoName,
		Description:    form.Description,
		Private:        form.Private,
		MirrorInterval: form.MirrorInterval,
	})
	if err != nil {
		handleFederatedMirrorError(ctx, ctxUser, err, tplFederatedMirror, form)
		return
	}

	log.Trace("Federated mirror created: %s/%s", ctxUser.Name, repo.Name)
	ctx.Redirect(repo.Link())
}

func handleFederatedMirrorError(ctx *context.Context, owner *user_model.User, err error, tpl base.TplName, form *forms.FederatedMirrorForm) {
	switch {
	case repo_model.IsErrReachLimitOfRepo(err):
		maxCreationLimit := owner.MaxCreationLimit()
		ctx.RenderWithErr(ctx.TrN(maxCreationLimit, "repo.form.reach_limit_of_creation_1", "repo.form.reach_limit_of_creation_n", maxCreationLimit), tpl, form)
	case repo_model.IsErrRepoAlreadyExist(err):
		ctx.Data["Err_RepoName"] = true
		ctx.RenderWithErr(ctx.Tr("form.repo_name_been_taken"), tpl, form)
	case repo_model.IsErrRepoFilesAlreadyExist(err):
		ctx.Data["Err_RepoName"] = true
		ctx.RenderWithErr(ctx.Tr("form.repository_files_already_exist"), tpl, form)
	default:
		ctx.RenderWithErr(ctx.Tr("repo.migrate.failed", err.Error()), tpl, form)
	}
}

// setFederatedMirrorContextData sets the template data shared by the GET and
// POST handlers of the federated mirror page.
func setFederatedMirrorContextData(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("repo.migrate.federated.title")
	ctx.Data["IsForcedPrivate"] = setting.Repository.ForcePrivate
	ctx.Data["DefaultMirrorInterval"] = setting.Mirror.DefaultInterval
	ctx.Data["MinimumMirrorInterval"] = setting.Mirror.MinInterval

	ownedOrgs, err := organization.GetOrgsCanCreateRepoByUserID(ctx, ctx.Doer.ID)
	if err != nil {
		log.Error("GetOrgsCanCreateRepoByUserID: %v", err)
	} else {
		ctx.Data["Orgs"] = ownedOrgs
	}
}
