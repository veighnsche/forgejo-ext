// Copyright 2021 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package asymkey

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"forgejo.org/models/db"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/modules/process"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/util"

	"github.com/42wim/sshsig"
	"golang.org/x/crypto/ssh"
)

// ParseObjectWithSSHSignature check if signature is good against keystore.
func ParseObjectWithSSHSignature(ctx context.Context, c *GitObject, committer *user_model.User) *ObjectVerification {
	// Now try to associate the signature with the committer, if present
	if committer.ID != 0 {
		keys, err := db.Find[PublicKey](ctx, FindPublicKeyOptions{
			OwnerID:    committer.ID,
			NotKeytype: KeyTypePrincipal,
		})
		if err != nil { // Skipping failed to get ssh keys of user
			log.Error("ListPublicKeys: %v", err)
			return &ObjectVerification{
				CommittingUser: committer,
				Verified:       false,
				Reason:         "gpg.error.failed_retrieval_gpg_keys",
			}
		}

		committerEmailAddresses, err := user_model.GetEmailAddresses(ctx, committer.ID)
		if err != nil {
			log.Error("GetEmailAddresses: %v", err)
		}

		// Add the noreply email address as verified address.
		committerEmailAddresses = append(committerEmailAddresses, &user_model.EmailAddress{
			IsActivated: true,
			Email:       committer.GetPlaceholderEmail(),
		})

		activated := false
		for _, e := range committerEmailAddresses {
			if e.IsActivated && strings.EqualFold(e.Email, c.Committer.Email) {
				activated = true
				break
			}
		}

		for _, k := range keys {
			if k.Verified && activated {
				commitVerification := verifySSHObjectVerification(ctx, c.Signature.Signature, c.Signature.Payload, k, committer, committer, c.Committer.Email)
				if commitVerification != nil {
					return commitVerification
				}
			}
		}
	}

	// If the SSH instance key is set, try to verify it with that key.
	if setting.SSHInstanceKey != nil {
		instanceSSHKey := &PublicKey{
			Content:     string(ssh.MarshalAuthorizedKey(setting.SSHInstanceKey)),
			Fingerprint: ssh.FingerprintSHA256(setting.SSHInstanceKey),
		}
		instanceUser := &user_model.User{
			Name:  setting.Repository.Signing.SigningName,
			Email: setting.Repository.Signing.SigningEmail,
		}
		commitVerification := verifySSHObjectVerification(ctx, c.Signature.Signature, c.Signature.Payload, instanceSSHKey, committer, instanceUser, setting.Repository.Signing.SigningEmail)
		if commitVerification != nil {
			return commitVerification
		}
	}

	return &ObjectVerification{
		CommittingUser: committer,
		Verified:       false,
		Reason:         NoKeyFound,
	}
}

func verifySSHObjectVerification(ctx context.Context, sig, payload string, k *PublicKey, committer, signer *user_model.User, email string) *ObjectVerification {
	var err error

	if len(setting.SSH.KeygenPath) == 0 || setting.SSH.StartBuiltinServer {
		err = sshsig.Verify(bytes.NewBuffer([]byte(payload)), []byte(sig), []byte(k.Content), "git")
	} else {
		var (
			tmpSignature, tmpAuthorizedSigners string
		)
		if tmpSignature, err = writeTmpKeyFile(sig); err != nil {
			log.Error("Failed to write signature to temporary file: %v", err)
			return nil
		}
		defer func() {
			if err := util.Remove(tmpSignature); err != nil {
				log.Warn("Unable to remove temporary signature file: %s: Error: %v", tmpSignature, err)
			}
		}()

		if tmpAuthorizedSigners, err = writeTmpKeyFile(fmt.Sprintf("* %s", k.Content)); err != nil {
			log.Error("Failed to write authorized signers to temporary file: %v", err)
			return nil
		}
		defer func() {
			if err := util.Remove(tmpAuthorizedSigners); err != nil {
				log.Warn("Unable to remove temporary signers file: %s: Error: %v", tmpAuthorizedSigners, err)
			}
		}()

		_, _, err = process.GetManager().ExecDirEnvStdIn(ctx, -1, "", "VerifySSHKey", nil, bytes.NewBuffer([]byte(payload)), setting.SSH.KeygenPath,
			"-Y", "verify",
			"-s", tmpSignature,
			"-I", "",
			"-f", tmpAuthorizedSigners,
			"-n", "git",
		)
	}

	if err != nil {
		log.Warn("Signature verification failed: %v", err)
		return nil
	}

	return &ObjectVerification{ // Everything is ok
		CommittingUser: committer,
		Verified:       true,
		Reason:         fmt.Sprintf("%s / %s", signer.Name, k.Fingerprint),
		SigningUser:    signer,
		SigningSSHKey:  k,
		SigningEmail:   email,
	}
}
