// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package mailer

import (
	"bytes"
	"context"
	"fmt"

	"forgejo.org/models/organization"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/base"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/translation"
)

const (
	tplRepoOwnerMail base.TplName = "moderation/contact_repo_owner"
)

// SendRepoOwnerModerationNoticeMail sends a notification e-mail to each repository owner with the provided custom message.
// All instance admins will be added as Bcc recipients (as a measure of traceability).
func SendRepoOwnerModerationNoticeMail(ctx context.Context, repo *repo_model.Repository, message string) error {
	if setting.MailService == nil {
		// No mail service configured
		return nil
	}

	instanceAdmins, err := user_model.GetAllAdmins(ctx)
	if err != nil {
		return err
	}
	bccRecipients := make([]string, 0, len(instanceAdmins))
	for _, admin := range instanceAdmins {
		bccRecipients = append(bccRecipients, admin.Email)
	}

	locale := translation.NewLocale("en_US")
	subject := locale.TrString("mail.repo.admin.moderation_notice.subject", setting.AppName)

	if repo.Owner.IsOrganization() {
		ownersTeam, err := organization.GetOwnerTeam(ctx, repo.Owner.ID)
		if err != nil {
			return err
		}
		err = ownersTeam.LoadMembers(ctx)
		if err != nil {
			return err
		}

		for _, user := range ownersTeam.Members {
			if !user.IsActive {
				// don't send emails to users that did not activated their account
				continue
			}
			err = sendRepoModerationNoticeMail(user, bccRecipients, repo, subject, message, locale)
			if err != nil {
				return err
			}
		}
	} else {
		err = sendRepoModerationNoticeMail(repo.Owner, bccRecipients, repo, subject, message, locale)
		if err != nil {
			return err
		}
	}

	return nil
}

func sendRepoModerationNoticeMail(recipient *user_model.User, bccRecipients []string, repo *repo_model.Repository, subject, notice string, locale translation.Locale) error {
	mailMeta := map[string]any{
		"Subject":       subject,
		"UserName":      recipient.Name,
		"RepoName":      repo.FullName(),
		"RepoURL":       repo.HTMLURL(),
		"CustomMessage": notice,
		"Language":      locale.Language(),
		"locale":        locale,
		"SanitizeHTML":  templates.SanitizeHTML,
	}

	var mailBody bytes.Buffer

	if err := bodyTemplates.ExecuteTemplate(&mailBody, string(tplRepoOwnerMail), mailMeta); err != nil {
		return err
	}

	msg := NewMessage(recipient.EmailTo(), subject, mailBody.String())
	msg.SetHeader("Bcc", bccRecipients...)
	msg.Info = fmt.Sprintf("Repo ID: %d, moderation notice", repo.ID)
	SendAsync(msg)

	return nil
}
