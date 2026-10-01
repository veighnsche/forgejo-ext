// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"context"
	"fmt"

	actions_model "forgejo.org/models/actions"
	activities_model "forgejo.org/models/activities"
	asymkey_model "forgejo.org/models/asymkey"
	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	model "forgejo.org/models/nativeoperation"
	organization "forgejo.org/models/organization"
	packages_model "forgejo.org/models/packages"
	access_model "forgejo.org/models/perm/access"
	pull_model "forgejo.org/models/pull"
	repo_model "forgejo.org/models/repo"
	secret_model "forgejo.org/models/secret"
	user_model "forgejo.org/models/user"

	"xorm.io/builder"
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
// User and organization deletes reconcile their full cross-family absence
// below; other multi-entity batches have no single-entity reconciliation
// and stay fenced.
func (s *Service) recoverAuthority(ctx context.Context, assessment *RecoveryAssessment, reservation *model.Reservation, scope Scope) (RecoveryAssessment, error) {
	if scope.AuthorityOp == "" {
		return fenced(assessment, ReasonRecoveryUnknownFamily, "authority scope names no attributable operation")
	}
	id, id2 := scope.AuthorityID, scope.AuthorityID2
	switch scope.AuthorityOp {
	case "user/delete":
		return s.recoverAuthorityUserDelete(ctx, assessment, reservation, id)
	case "org/delete":
		return s.recoverAuthorityOrgDelete(ctx, assessment, reservation, id)
	case "user/block":
		return s.recoverAuthorityUserBlock(ctx, assessment, reservation, id, id2)
	case "users/delete-inactive", "users/must-change-password", "directory-sync", "admin/regenerate-hooks", "admin/regenerate-keys":
		return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("authority operation %q covers many entities; no single-entity reconciliation", scope.AuthorityOp))
	case "user/update", "user/auth", "user/activate", "user/rename", "user/promote",
		"user/credentials/reset", "user/account-link", "user/openid",
		"user/unblock", "user/email", "user/email/activate",
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

// danglingCheck counts rows that a completed account deletion must have
// removed. Every check runs against committed state while the domain is
// stopped, so any remaining row proves the delete's final transaction
// never committed its full effect.
type danglingCheck struct {
	name  string
	count func(ctx context.Context, id int64) (int64, error)
}

func countWhere(bean func() any, cond string, args ...any) func(ctx context.Context, id int64) (int64, error) {
	return func(ctx context.Context, id int64) (int64, error) {
		full := make([]any, 0, len(args)+1)
		for _, arg := range args {
			if arg == nil {
				full = append(full, id)
			} else {
				full = append(full, arg)
			}
		}
		return db.GetEngine(ctx).Where(cond, full...).Count(bean())
	}
}

// recoverAuthorityUserDelete reconciles one held user deletion across its
// families. The delete commits its purge in separate transactions and its
// row removal plus credential cleanup in one final transaction guarded by
// ownership checks, so an absent user row proves the final transaction
// committed and the purge completed. Every nested row set must then be
// absent; any survivor fences as a partial delete. A present user row
// fences: the purge may have partially applied outside the final
// transaction, which no post-hoc check can distinguish from a delete
// that never started.
//
// Rows the delete keeps by design need no check: issues, comments and
// reactions left for a non-purge delete are self-consistent (their
// presence proves the purge branch never ran, since that branch shares
// the final transaction), and name-keyed rows (redirects) plus derived
// counters and post-commit filesystem effects (key files, avatars) follow
// from the committed transaction. Stale key-file lines reference deleted
// key IDs, which SSH authentication fails closed on.
func (s *Service) recoverAuthorityUserDelete(ctx context.Context, assessment *RecoveryAssessment, reservation *model.Reservation, userID int64) (RecoveryAssessment, error) {
	if userID <= 0 {
		return fenced(assessment, ReasonRecoveryUnknownFamily, "authority delete names no user")
	}
	if _, err := user_model.GetUserByID(ctx, userID); err != nil {
		if !user_model.IsErrUserNotExist(err) {
			return RecoveryAssessment{}, err
		}
	} else {
		return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("user %d still exists after a held deletion; complete the delete manually", userID))
	}
	checks := []danglingCheck{
		{"watch", countWhere(func() any { return new(repo_model.Watch) }, "user_id=?", nil)},
		{"star", countWhere(func() any { return new(repo_model.Star) }, "uid=?", nil)},
		{"follow", countWhere(func() any { return new(user_model.Follow) }, "user_id=? OR follow_id=?", nil, nil)},
		{"access_token", countWhere(func() any { return new(auth_model.AccessToken) }, "uid=?", nil)},
		{"collaboration", countWhere(func() any { return new(repo_model.Collaboration) }, "user_id=?", nil)},
		{"access", countWhere(func() any { return new(access_model.Access) }, "user_id=?", nil)},
		{"action", countWhere(func() any { return new(activities_model.Action) }, "user_id=? OR act_user_id=?", nil, nil)},
		{"issue_user", countWhere(func() any { return new(issues_model.IssueUser) }, "uid=?", nil)},
		{"email", countWhere(func() any { return new(user_model.EmailAddress) }, "uid=?", nil)},
		{"openid", countWhere(func() any { return new(user_model.UserOpenID) }, "uid=?", nil)},
		{"reaction", countWhere(func() any { return new(issues_model.Reaction) }, "user_id=?", nil)},
		{"team_user", countWhere(func() any { return new(organization.TeamUser) }, "uid=?", nil)},
		{"stopwatch", countWhere(func() any { return new(issues_model.Stopwatch) }, "user_id=?", nil)},
		{"setting", countWhere(func() any { return new(user_model.Setting) }, "user_id=?", nil)},
		{"badge", countWhere(func() any { return new(user_model.UserBadge) }, "user_id=?", nil)},
		{"automerge", countWhere(func() any { return new(pull_model.AutoMerge) }, "doer_id=?", nil)},
		{"review_state", countWhere(func() any { return new(pull_model.ReviewState) }, "user_id=?", nil)},
		{"action_runner", countWhere(func() any { return new(actions_model.ActionRunner) }, "owner_id=?", nil)},
		{"action_user", countWhere(func() any { return new(actions_model.ActionUser) }, "user_id=?", nil)},
		{"blocked", countWhere(func() any { return new(user_model.BlockedUser) }, "user_id=? OR block_id=?", nil, nil)},
		{"runner_token", countWhere(func() any { return new(actions_model.ActionRunnerToken) }, "owner_id=?", nil)},
		{"auth_token", countWhere(func() any { return new(auth_model.AuthorizationToken) }, "uid=?", nil)},
		{"tracked_time", countWhere(func() any { return new(issues_model.TrackedTime) }, "user_id=?", nil)},
		{"oauth2_app", countWhere(func() any { return new(auth_model.OAuth2Application) }, "uid=?", nil)},
		{"oauth2_grant", countWhere(func() any { return new(auth_model.OAuth2Grant) }, "user_id=?", nil)},
		{"public_key", countWhere(func() any { return new(asymkey_model.PublicKey) }, "owner_id=?", nil)},
		{"gpg_key", countWhere(func() any { return new(asymkey_model.GPGKey) }, "owner_id=?", nil)},
		{"assignee", countWhere(func() any { return new(issues_model.IssueAssignees) }, "assignee_id=?", nil)},
		{"external_login", countWhere(func() any { return new(user_model.ExternalLoginUser) }, "user_id=?", nil)},
	}
	for _, check := range checks {
		n, err := check.count(ctx, userID)
		if err != nil {
			return RecoveryAssessment{}, err
		}
		if n > 0 {
			assessment.Checks = append(assessment.Checks, fmt.Sprintf("user delete dangling %s=%d", check.name, n))
			return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("%d %s rows survive the deleted user %d", n, check.name, userID))
		}
	}
	repos, err := repo_model.CountRepositories(ctx, repo_model.CountRepositoryOptions{OwnerID: userID})
	if err != nil {
		return RecoveryAssessment{}, err
	}
	if repos > 0 {
		assessment.Checks = append(assessment.Checks, fmt.Sprintf("user delete dangling repositories=%d", repos))
		return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("%d repositories survive the deleted user %d", repos, userID))
	}
	ownsPackages, err := packages_model.HasOwnerPackages(ctx, userID)
	if err != nil {
		return RecoveryAssessment{}, err
	}
	if ownsPackages {
		assessment.Checks = append(assessment.Checks, "user delete dangling packages=true")
		return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("packages survive the deleted user %d", userID))
	}
	assessment.Checks = append(assessment.Checks, fmt.Sprintf("user %d absent with no dangling rows", userID))
	return s.releaseRecovered(ctx, assessment, reservation, "deleted")
}

// recoverAuthorityOrgDelete reconciles one held organization deletion
// across its families. The delete purges repositories first, then removes
// the organization row with its teams, secrets, runners and follows in one
// guarded transaction, so an absent organization row proves the removal
// committed. Every nested row set must then be absent; any survivor
// fences as a partial delete. A present organization row fences: the
// purge may have partially applied outside the final transaction.
func (s *Service) recoverAuthorityOrgDelete(ctx context.Context, assessment *RecoveryAssessment, reservation *model.Reservation, orgID int64) (RecoveryAssessment, error) {
	if orgID <= 0 {
		return fenced(assessment, ReasonRecoveryUnknownFamily, "authority delete names no organization")
	}
	if _, err := user_model.GetUserByID(ctx, orgID); err != nil {
		if !user_model.IsErrUserNotExist(err) {
			return RecoveryAssessment{}, err
		}
	} else {
		return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("organization %d still exists after a held deletion; complete the delete manually", orgID))
	}
	checks := []danglingCheck{
		{"team", countWhere(func() any { return new(organization.Team) }, "org_id=?", nil)},
		{"org_user", countWhere(func() any { return new(organization.OrgUser) }, "org_id=?", nil)},
		{"team_user", countWhere(func() any { return new(organization.TeamUser) }, "org_id=?", nil)},
		{"team_unit", countWhere(func() any { return new(organization.TeamUnit) }, "org_id=?", nil)},
		{"team_invite", countWhere(func() any { return new(organization.TeamInvite) }, "org_id=?", nil)},
		{"secret", countWhere(func() any { return new(secret_model.Secret) }, "owner_id=?", nil)},
		{"action_runner", countWhere(func() any { return new(actions_model.ActionRunner) }, "owner_id=?", nil)},
		{"runner_token", countWhere(func() any { return new(actions_model.ActionRunnerToken) }, "owner_id=?", nil)},
		{"blocked", countWhere(func() any { return new(user_model.BlockedUser) }, "user_id=?", nil)},
		{"follow", countWhere(func() any { return new(user_model.Follow) }, "follow_id=?", nil)},
	}
	for _, check := range checks {
		n, err := check.count(ctx, orgID)
		if err != nil {
			return RecoveryAssessment{}, err
		}
		if n > 0 {
			assessment.Checks = append(assessment.Checks, fmt.Sprintf("org delete dangling %s=%d", check.name, n))
			return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("%d %s rows survive the deleted organization %d", n, check.name, orgID))
		}
	}
	repos, err := repo_model.CountRepositories(ctx, repo_model.CountRepositoryOptions{OwnerID: orgID})
	if err != nil {
		return RecoveryAssessment{}, err
	}
	if repos > 0 {
		assessment.Checks = append(assessment.Checks, fmt.Sprintf("org delete dangling repositories=%d", repos))
		return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("%d repositories survive the deleted organization %d", repos, orgID))
	}
	ownsPackages, err := packages_model.HasOwnerPackages(ctx, orgID)
	if err != nil {
		return RecoveryAssessment{}, err
	}
	if ownsPackages {
		assessment.Checks = append(assessment.Checks, "org delete dangling packages=true")
		return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("packages survive the deleted organization %d", orgID))
	}
	assessment.Checks = append(assessment.Checks, fmt.Sprintf("organization %d absent with no dangling rows", orgID))
	return s.releaseRecovered(ctx, assessment, reservation, "deleted")
}

// recoverAuthorityUserBlock reconciles one held user block across its
// families. The block commits its row, unfollows, unwatches, collaborator
// removals and nested trust revocations (with their run cancellations) in
// one transaction, so a present block row proves every nested effect
// committed. The nested Actions effects are then verified directly: no
// trust row may remain for the blocked user in any repository owned by
// the blocker, and none of their runs there may be unfinished. An absent
// block row proves the transaction never committed, so nothing applied.
func (s *Service) recoverAuthorityUserBlock(ctx context.Context, assessment *RecoveryAssessment, reservation *model.Reservation, userID, blockID int64) (RecoveryAssessment, error) {
	if userID <= 0 || blockID <= 0 {
		return fenced(assessment, ReasonRecoveryUnknownFamily, "authority block names no user pair")
	}
	if _, err := user_model.GetUserByID(ctx, userID); err != nil {
		if user_model.IsErrUserNotExist(err) {
			return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("user %d for the held authority scope no longer exists", userID))
		}
		return RecoveryAssessment{}, err
	}
	blocked := user_model.IsBlocked(ctx, userID, blockID)
	assessment.Checks = append(assessment.Checks, fmt.Sprintf("block %d/%d present=%t", userID, blockID, blocked))
	if !blocked {
		return s.releaseRecovered(ctx, assessment, reservation, EffectAuthorityConsistent)
	}
	err := db.Iterate(ctx, builder.Eq{"owner_id": userID}, func(ctx context.Context, repo *repo_model.Repository) error {
		if _, err := actions_model.GetActionUserByUserIDAndRepoID(ctx, blockID, repo.ID); err != nil {
			if !actions_model.IsErrUserNotExist(err) {
				return err
			}
		} else {
			assessment.Checks = append(assessment.Checks, fmt.Sprintf("block trust row survives in repository %d", repo.ID))
			return errBlockSurvivor
		}
		runs, err := actions_model.GetRunsNotDoneByRepoIDAndPullRequestPosterID(ctx, repo.ID, blockID)
		if err != nil {
			return err
		}
		if len(runs) > 0 {
			assessment.Checks = append(assessment.Checks, fmt.Sprintf("block unfinished runs=%d in repository %d", len(runs), repo.ID))
			return errBlockSurvivor
		}
		return nil
	})
	if err != nil {
		if err == errBlockSurvivor {
			return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("trust or run state for user %d survives in a repository of user %d after a held block", blockID, userID))
		}
		return RecoveryAssessment{}, err
	}
	return s.releaseRecovered(ctx, assessment, reservation, EffectAuthorityConsistent)
}

// errBlockSurvivor marks a nested trust or run row that survived a held
// block whose anchor row is present. The iterate callback cannot fence
// directly, so it returns this sentinel for the caller to translate.
var errBlockSurvivor = fmt.Errorf("nested block effect survives")
