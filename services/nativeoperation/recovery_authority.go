// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"context"
	"fmt"

	asymkey_model "forgejo.org/models/asymkey"
	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	model "forgejo.org/models/nativeoperation"
	organization "forgejo.org/models/organization"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
)

// EffectAuthorityConsistent releases an interrupted authority writer whose
// referenced entities are intact. Authority credential changes apply in one
// database transaction, so the observed state is always a valid end state;
// the exclusive held reservation proves no other writer interleaved.
const EffectAuthorityConsistent = "consistent"

// recoverAuthority reconciles one held ordinary authority writer from its
// operation's entities. Referenced users, teams, repositories and credential
// parents must still exist: an authority change never deletes its own
// context, so a missing entity means unaccounted interference and fences.
// Cross-family deletes and multi-entity batches have no single-entity
// reconciliation and stay fenced.
func (s *Service) recoverAuthority(ctx context.Context, assessment *RecoveryAssessment, reservation *model.Reservation, scope Scope) (RecoveryAssessment, error) {
	if scope.AuthorityOp == "" {
		return fenced(assessment, ReasonRecoveryUnknownFamily, "authority scope names no attributable operation")
	}
	id, id2 := scope.AuthorityID, scope.AuthorityID2
	switch scope.AuthorityOp {
	case "user/delete", "org/delete":
		return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("authority operation %q spans repository effects; composed recovery owns it", scope.AuthorityOp))
	case "users/delete-inactive", "users/must-change-password", "directory-sync", "admin/regenerate-hooks", "admin/regenerate-keys":
		return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("authority operation %q covers many entities; no single-entity reconciliation", scope.AuthorityOp))
	case "user/update", "user/auth", "user/activate", "user/rename", "user/promote",
		"user/credentials/reset", "user/account-link", "user/openid",
		"user/block", "user/unblock", "user/email", "user/email/activate",
		"user/token", "user/2fa", "user/webauthn",
		"user/key", "user/key/verify", "user/gpg-key", "user/gpg-key/verify",
		"user/principal-key", "user/oauth2-app":
		return s.recoverAuthorityUser(ctx, assessment, reservation, scope, id)
	case "org/email", "org/team":
		return s.recoverAuthorityOrg(ctx, assessment, reservation, scope, id)
	case "org/unblock":
		return s.recoverAuthorityOrgMember(ctx, assessment, reservation, scope, id, id2)
	case "team", "team/repos", "team/invite":
		return s.recoverAuthorityTeam(ctx, assessment, reservation, scope, id)
	case "team/member":
		return s.recoverAuthorityTeamMember(ctx, assessment, reservation, scope, id, id2)
	case "org/member":
		return s.recoverAuthorityOrgMember(ctx, assessment, reservation, scope, id, id2)
	case "team/repo":
		return s.recoverAuthorityTeamRepo(ctx, assessment, reservation, scope, id, id2)
	case "token":
		return s.recoverAuthorityToken(ctx, assessment, reservation, scope, id)
	case "key":
		return s.recoverAuthorityKey(ctx, assessment, reservation, scope, id)
	case "gpg-key":
		return s.recoverAuthorityGPGKey(ctx, assessment, reservation, scope, id)
	case "oauth2-app", "owner/oauth2-app":
		return s.recoverAuthorityOAuth2App(ctx, assessment, reservation, scope, id)
	case "oauth2-grant":
		return s.recoverAuthorityOAuth2Grant(ctx, assessment, reservation, scope, id)
	case "auth-source":
		return s.recoverAuthoritySource(ctx, assessment, reservation, scope, id)
	case "repo/collaborator":
		return s.recoverAuthorityCollaborator(ctx, assessment, reservation, scope, id, id2)
	case "repo/deploy-key":
		return s.recoverAuthorityDeployKey(ctx, assessment, reservation, scope, id, id2)
	default:
		return fenced(assessment, ReasonRecoveryUnknownFamily, fmt.Sprintf("authority operation %q has no offline reconciliation", scope.AuthorityOp))
	}
}

// recoverAuthorityUser releases a user-scoped authority writer when the user
// row is intact. Credential and profile changes never delete their user.
func (s *Service) recoverAuthorityUser(ctx context.Context, assessment *RecoveryAssessment, reservation *model.Reservation, scope Scope, userID int64) (RecoveryAssessment, error) {
	if userID <= 0 {
		return fenced(assessment, ReasonRecoveryUnknownFamily, fmt.Sprintf("authority operation %q names no user", scope.AuthorityOp))
	}
	if _, err := user_model.GetUserByID(ctx, userID); err != nil {
		if user_model.IsErrUserNotExist(err) {
			return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("user %d for the held authority scope no longer exists", userID))
		}
		return RecoveryAssessment{}, err
	}
	assessment.Checks = append(assessment.Checks, fmt.Sprintf("user %d exists", userID))
	return s.releaseRecovered(ctx, assessment, reservation, EffectAuthorityConsistent)
}

// recoverAuthorityOrg releases an organization-scoped writer when the
// organization row is intact and still an organization.
func (s *Service) recoverAuthorityOrg(ctx context.Context, assessment *RecoveryAssessment, reservation *model.Reservation, scope Scope, orgID int64) (RecoveryAssessment, error) {
	if orgID <= 0 {
		return fenced(assessment, ReasonRecoveryUnknownFamily, fmt.Sprintf("authority operation %q names no organization", scope.AuthorityOp))
	}
	org, err := user_model.GetUserByID(ctx, orgID)
	if err != nil {
		if user_model.IsErrUserNotExist(err) {
			return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("organization %d for the held authority scope no longer exists", orgID))
		}
		return RecoveryAssessment{}, err
	}
	if !org.IsOrganization() {
		return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("account %d for the held authority scope is not an organization", orgID))
	}
	assessment.Checks = append(assessment.Checks, fmt.Sprintf("organization %d exists", orgID))
	return s.releaseRecovered(ctx, assessment, reservation, EffectAuthorityConsistent)
}

// recoverAuthorityTeam releases a team-scoped writer when the team row is
// intact. Member, repository and invite changes never delete their team.
func (s *Service) recoverAuthorityTeam(ctx context.Context, assessment *RecoveryAssessment, reservation *model.Reservation, scope Scope, teamID int64) (RecoveryAssessment, error) {
	if teamID <= 0 {
		return fenced(assessment, ReasonRecoveryUnknownFamily, fmt.Sprintf("authority operation %q names no team", scope.AuthorityOp))
	}
	if _, err := organization.GetTeamByID(ctx, teamID); err != nil {
		if organization.IsErrTeamNotExist(err) {
			return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("team %d for the held authority scope no longer exists", teamID))
		}
		return RecoveryAssessment{}, err
	}
	assessment.Checks = append(assessment.Checks, fmt.Sprintf("team %d exists", teamID))
	return s.releaseRecovered(ctx, assessment, reservation, EffectAuthorityConsistent)
}

// recoverAuthorityTeamMember releases a team-membership writer when both the
// team and the user rows are intact.
func (s *Service) recoverAuthorityTeamMember(ctx context.Context, assessment *RecoveryAssessment, reservation *model.Reservation, scope Scope, teamID, userID int64) (RecoveryAssessment, error) {
	if teamID <= 0 || userID <= 0 {
		return fenced(assessment, ReasonRecoveryUnknownFamily, fmt.Sprintf("authority operation %q names no team member pair", scope.AuthorityOp))
	}
	if _, err := organization.GetTeamByID(ctx, teamID); err != nil {
		if organization.IsErrTeamNotExist(err) {
			return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("team %d for the held authority scope no longer exists", teamID))
		}
		return RecoveryAssessment{}, err
	}
	if _, err := user_model.GetUserByID(ctx, userID); err != nil {
		if user_model.IsErrUserNotExist(err) {
			return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("user %d for the held authority scope no longer exists", userID))
		}
		return RecoveryAssessment{}, err
	}
	assessment.Checks = append(assessment.Checks, fmt.Sprintf("team %d and user %d exist", teamID, userID))
	return s.releaseRecovered(ctx, assessment, reservation, EffectAuthorityConsistent)
}

// recoverAuthorityOrgMember releases an organization-membership writer when
// both the organization and the user rows are intact.
func (s *Service) recoverAuthorityOrgMember(ctx context.Context, assessment *RecoveryAssessment, reservation *model.Reservation, scope Scope, orgID, userID int64) (RecoveryAssessment, error) {
	if orgID <= 0 || userID <= 0 {
		return fenced(assessment, ReasonRecoveryUnknownFamily, fmt.Sprintf("authority operation %q names no organization member pair", scope.AuthorityOp))
	}
	org, err := user_model.GetUserByID(ctx, orgID)
	if err != nil {
		if user_model.IsErrUserNotExist(err) {
			return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("organization %d for the held authority scope no longer exists", orgID))
		}
		return RecoveryAssessment{}, err
	}
	if !org.IsOrganization() {
		return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("account %d for the held authority scope is not an organization", orgID))
	}
	if _, err := user_model.GetUserByID(ctx, userID); err != nil {
		if user_model.IsErrUserNotExist(err) {
			return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("user %d for the held authority scope no longer exists", userID))
		}
		return RecoveryAssessment{}, err
	}
	assessment.Checks = append(assessment.Checks, fmt.Sprintf("organization %d and user %d exist", orgID, userID))
	return s.releaseRecovered(ctx, assessment, reservation, EffectAuthorityConsistent)
}

// recoverAuthorityTeamRepo releases a team-repository writer when both the
// team and the repository rows are intact.
func (s *Service) recoverAuthorityTeamRepo(ctx context.Context, assessment *RecoveryAssessment, reservation *model.Reservation, scope Scope, teamID, repoID int64) (RecoveryAssessment, error) {
	if teamID <= 0 || repoID <= 0 {
		return fenced(assessment, ReasonRecoveryUnknownFamily, fmt.Sprintf("authority operation %q names no team repository pair", scope.AuthorityOp))
	}
	if _, err := organization.GetTeamByID(ctx, teamID); err != nil {
		if organization.IsErrTeamNotExist(err) {
			return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("team %d for the held authority scope no longer exists", teamID))
		}
		return RecoveryAssessment{}, err
	}
	if _, err := repo_model.GetRepositoryByID(ctx, repoID); err != nil {
		if repo_model.IsErrRepoNotExist(err) {
			return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("repository %d for the held authority scope no longer exists", repoID))
		}
		return RecoveryAssessment{}, err
	}
	assessment.Checks = append(assessment.Checks, fmt.Sprintf("team %d and repository %d exist", teamID, repoID))
	return s.releaseRecovered(ctx, assessment, reservation, EffectAuthorityConsistent)
}

// recoverAuthorityToken releases a token writer. The token row itself may be
// present or absent (both are valid single-transaction end states); a live
// token needs its owner, a withdrawn token must leave no dangling resource
// rows behind.
func (s *Service) recoverAuthorityToken(ctx context.Context, assessment *RecoveryAssessment, reservation *model.Reservation, scope Scope, tokenID int64) (RecoveryAssessment, error) {
	if tokenID <= 0 {
		return fenced(assessment, ReasonRecoveryUnknownFamily, fmt.Sprintf("authority operation %q names no token", scope.AuthorityOp))
	}
	token := &auth_model.AccessToken{}
	present, err := db.GetEngine(ctx).ID(tokenID).Get(token)
	if err != nil {
		return RecoveryAssessment{}, err
	}
	if !present {
		resources, err := auth_model.GetRepositoriesAccessibleWithToken(ctx, tokenID)
		if err != nil {
			return RecoveryAssessment{}, err
		}
		assessment.Checks = append(assessment.Checks, fmt.Sprintf("token %d absent with %d dangling resource rows", tokenID, len(resources)))
		if len(resources) > 0 {
			return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("withdrawn token %d still authorizes repositories", tokenID))
		}
		return s.releaseRecovered(ctx, assessment, reservation, EffectAuthorityConsistent)
	}
	if _, err := user_model.GetUserByID(ctx, token.UID); err != nil {
		if user_model.IsErrUserNotExist(err) {
			return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("owner %d of token %d no longer exists", token.UID, tokenID))
		}
		return RecoveryAssessment{}, err
	}
	assessment.Checks = append(assessment.Checks, fmt.Sprintf("token %d present with owner %d", tokenID, token.UID))
	return s.releaseRecovered(ctx, assessment, reservation, EffectAuthorityConsistent)
}

// recoverAuthorityKey releases a public-key writer. The key row itself may be
// present or absent; a live key needs its owner.
func (s *Service) recoverAuthorityKey(ctx context.Context, assessment *RecoveryAssessment, reservation *model.Reservation, scope Scope, keyID int64) (RecoveryAssessment, error) {
	if keyID <= 0 {
		return fenced(assessment, ReasonRecoveryUnknownFamily, fmt.Sprintf("authority operation %q names no key", scope.AuthorityOp))
	}
	key, err := asymkey_model.GetPublicKeyByID(ctx, keyID)
	if err != nil {
		if asymkey_model.IsErrKeyNotExist(err) {
			assessment.Checks = append(assessment.Checks, fmt.Sprintf("key %d absent", keyID))
			return s.releaseRecovered(ctx, assessment, reservation, EffectAuthorityConsistent)
		}
		return RecoveryAssessment{}, err
	}
	if _, err := user_model.GetUserByID(ctx, key.OwnerID); err != nil {
		if user_model.IsErrUserNotExist(err) {
			return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("owner %d of key %d no longer exists", key.OwnerID, keyID))
		}
		return RecoveryAssessment{}, err
	}
	assessment.Checks = append(assessment.Checks, fmt.Sprintf("key %d present with owner %d", keyID, key.OwnerID))
	return s.releaseRecovered(ctx, assessment, reservation, EffectAuthorityConsistent)
}

// recoverAuthorityGPGKey releases a GPG-key writer. The key row itself may be
// present or absent; a live key needs its owner.
func (s *Service) recoverAuthorityGPGKey(ctx context.Context, assessment *RecoveryAssessment, reservation *model.Reservation, scope Scope, keyID int64) (RecoveryAssessment, error) {
	if keyID <= 0 {
		return fenced(assessment, ReasonRecoveryUnknownFamily, fmt.Sprintf("authority operation %q names no key", scope.AuthorityOp))
	}
	key := &asymkey_model.GPGKey{}
	present, err := db.GetEngine(ctx).ID(keyID).Get(key)
	if err != nil {
		return RecoveryAssessment{}, err
	}
	if !present {
		assessment.Checks = append(assessment.Checks, fmt.Sprintf("gpg key %d absent", keyID))
		return s.releaseRecovered(ctx, assessment, reservation, EffectAuthorityConsistent)
	}
	if _, err := user_model.GetUserByID(ctx, key.OwnerID); err != nil {
		if user_model.IsErrUserNotExist(err) {
			return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("owner %d of gpg key %d no longer exists", key.OwnerID, keyID))
		}
		return RecoveryAssessment{}, err
	}
	assessment.Checks = append(assessment.Checks, fmt.Sprintf("gpg key %d present with owner %d", keyID, key.OwnerID))
	return s.releaseRecovered(ctx, assessment, reservation, EffectAuthorityConsistent)
}

// recoverAuthorityOAuth2App releases an OAuth2-application writer. A live
// application needs its owner (unless instance-wide); a deleted application
// must leave no dangling grants behind.
func (s *Service) recoverAuthorityOAuth2App(ctx context.Context, assessment *RecoveryAssessment, reservation *model.Reservation, scope Scope, appID int64) (RecoveryAssessment, error) {
	if appID <= 0 {
		return fenced(assessment, ReasonRecoveryUnknownFamily, fmt.Sprintf("authority operation %q names no application", scope.AuthorityOp))
	}
	app, err := auth_model.GetOAuth2ApplicationByID(ctx, appID)
	if err != nil {
		if auth_model.IsErrOAuthApplicationNotFound(err) {
			dangling, err := db.GetEngine(ctx).Where("application_id = ?", appID).Count(new(auth_model.OAuth2Grant))
			if err != nil {
				return RecoveryAssessment{}, err
			}
			assessment.Checks = append(assessment.Checks, fmt.Sprintf("application %d absent with %d dangling grants", appID, dangling))
			if dangling > 0 {
				return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("deleted application %d still has grants", appID))
			}
			return s.releaseRecovered(ctx, assessment, reservation, EffectAuthorityConsistent)
		}
		return RecoveryAssessment{}, err
	}
	if app.UID != 0 {
		if _, err := user_model.GetUserByID(ctx, app.UID); err != nil {
			if user_model.IsErrUserNotExist(err) {
				return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("owner %d of application %d no longer exists", app.UID, appID))
			}
			return RecoveryAssessment{}, err
		}
	}
	assessment.Checks = append(assessment.Checks, fmt.Sprintf("application %d present with owner %d", appID, app.UID))
	return s.releaseRecovered(ctx, assessment, reservation, EffectAuthorityConsistent)
}

// recoverAuthorityOAuth2Grant releases an OAuth2-grant writer. The grant row
// itself may be present or absent; a live grant needs its application and
// its user.
func (s *Service) recoverAuthorityOAuth2Grant(ctx context.Context, assessment *RecoveryAssessment, reservation *model.Reservation, scope Scope, grantID int64) (RecoveryAssessment, error) {
	if grantID <= 0 {
		return fenced(assessment, ReasonRecoveryUnknownFamily, fmt.Sprintf("authority operation %q names no grant", scope.AuthorityOp))
	}
	grant := &auth_model.OAuth2Grant{}
	present, err := db.GetEngine(ctx).ID(grantID).Get(grant)
	if err != nil {
		return RecoveryAssessment{}, err
	}
	if !present {
		assessment.Checks = append(assessment.Checks, fmt.Sprintf("grant %d absent", grantID))
		return s.releaseRecovered(ctx, assessment, reservation, EffectAuthorityConsistent)
	}
	if _, err := auth_model.GetOAuth2ApplicationByID(ctx, grant.ApplicationID); err != nil {
		if auth_model.IsErrOAuthApplicationNotFound(err) {
			return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("application %d of grant %d no longer exists", grant.ApplicationID, grantID))
		}
		return RecoveryAssessment{}, err
	}
	if _, err := user_model.GetUserByID(ctx, grant.UserID); err != nil {
		if user_model.IsErrUserNotExist(err) {
			return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("user %d of grant %d no longer exists", grant.UserID, grantID))
		}
		return RecoveryAssessment{}, err
	}
	assessment.Checks = append(assessment.Checks, fmt.Sprintf("grant %d present with application %d and user %d", grantID, grant.ApplicationID, grant.UserID))
	return s.releaseRecovered(ctx, assessment, reservation, EffectAuthorityConsistent)
}

// recoverAuthoritySource releases an auth-source writer. A live source needs
// nothing else; a deleted source must leave no dangling external logins
// behind.
func (s *Service) recoverAuthoritySource(ctx context.Context, assessment *RecoveryAssessment, reservation *model.Reservation, scope Scope, sourceID int64) (RecoveryAssessment, error) {
	if sourceID <= 0 {
		return fenced(assessment, ReasonRecoveryUnknownFamily, fmt.Sprintf("authority operation %q names no auth source", scope.AuthorityOp))
	}
	if _, err := auth_model.GetSourceByID(ctx, sourceID); err != nil {
		if auth_model.IsErrSourceNotExist(err) {
			dangling, err := db.GetEngine(ctx).Where("login_source_id = ?", sourceID).Count(new(user_model.ExternalLoginUser))
			if err != nil {
				return RecoveryAssessment{}, err
			}
			assessment.Checks = append(assessment.Checks, fmt.Sprintf("auth source %d absent with %d dangling external logins", sourceID, dangling))
			if dangling > 0 {
				return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("deleted auth source %d still has external logins", sourceID))
			}
			return s.releaseRecovered(ctx, assessment, reservation, EffectAuthorityConsistent)
		}
		return RecoveryAssessment{}, err
	}
	assessment.Checks = append(assessment.Checks, fmt.Sprintf("auth source %d present", sourceID))
	return s.releaseRecovered(ctx, assessment, reservation, EffectAuthorityConsistent)
}

// recoverAuthorityCollaborator releases a collaborator writer when both the
// repository and the user rows are intact.
func (s *Service) recoverAuthorityCollaborator(ctx context.Context, assessment *RecoveryAssessment, reservation *model.Reservation, scope Scope, repoID, userID int64) (RecoveryAssessment, error) {
	if repoID <= 0 || userID <= 0 {
		return fenced(assessment, ReasonRecoveryUnknownFamily, fmt.Sprintf("authority operation %q names no repository collaborator pair", scope.AuthorityOp))
	}
	if _, err := repo_model.GetRepositoryByID(ctx, repoID); err != nil {
		if repo_model.IsErrRepoNotExist(err) {
			return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("repository %d for the held authority scope no longer exists", repoID))
		}
		return RecoveryAssessment{}, err
	}
	if _, err := user_model.GetUserByID(ctx, userID); err != nil {
		if user_model.IsErrUserNotExist(err) {
			return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("user %d for the held authority scope no longer exists", userID))
		}
		return RecoveryAssessment{}, err
	}
	assessment.Checks = append(assessment.Checks, fmt.Sprintf("repository %d and user %d exist", repoID, userID))
	return s.releaseRecovered(ctx, assessment, reservation, EffectAuthorityConsistent)
}

// recoverAuthorityDeployKey releases a deploy-key writer when the repository
// row is intact. The deploy association itself may be present or absent
// (both are valid end states); an orphaned public-key row left by the
// non-transactional deploy delete authorizes nothing without its association.
func (s *Service) recoverAuthorityDeployKey(ctx context.Context, assessment *RecoveryAssessment, reservation *model.Reservation, scope Scope, repoID, keyID int64) (RecoveryAssessment, error) {
	if repoID <= 0 || keyID <= 0 {
		return fenced(assessment, ReasonRecoveryUnknownFamily, fmt.Sprintf("authority operation %q names no repository deploy-key pair", scope.AuthorityOp))
	}
	if _, err := repo_model.GetRepositoryByID(ctx, repoID); err != nil {
		if repo_model.IsErrRepoNotExist(err) {
			return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("repository %d for the held authority scope no longer exists", repoID))
		}
		return RecoveryAssessment{}, err
	}
	key, err := asymkey_model.GetDeployKeyByID(ctx, keyID)
	if err != nil {
		if asymkey_model.IsErrDeployKeyNotExist(err) {
			assessment.Checks = append(assessment.Checks, fmt.Sprintf("deploy association %d absent", keyID))
			return s.releaseRecovered(ctx, assessment, reservation, EffectAuthorityConsistent)
		}
		return RecoveryAssessment{}, err
	}
	if key.RepoID != repoID {
		return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("deploy association %d belongs to repository %d, not %d", keyID, key.RepoID, repoID))
	}
	assessment.Checks = append(assessment.Checks, fmt.Sprintf("deploy association %d present for repository %d", keyID, repoID))
	return s.releaseRecovered(ctx, assessment, reservation, EffectAuthorityConsistent)
}
