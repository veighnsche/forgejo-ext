// Copyright 2021 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package asymkey

import (
	"bytes"
	"context"
	"fmt"

	"forgejo.org/models/db"
	"forgejo.org/modules/log"
	"forgejo.org/modules/process"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/util"

	"github.com/42wim/sshsig"
)

// VerifySSHKey marks a SSH key as verified
func VerifySSHKey(ctx context.Context, ownerID int64, fingerprint, token, signature string) (string, error) {
	ctx, committer, err := db.TxContext(ctx)
	if err != nil {
		return "", err
	}
	defer committer.Close()

	key := new(PublicKey)

	has, err := db.GetEngine(ctx).Where("owner_id = ? AND fingerprint = ?", ownerID, fingerprint).Get(key)
	if err != nil {
		return "", err
	} else if !has {
		return "", ErrKeyNotExist{}
	}

	if len(setting.SSH.KeygenPath) == 0 || setting.SSH.StartBuiltinServer {
		err = sshsig.Verify(bytes.NewBuffer([]byte(token)), []byte(signature), []byte(key.Content), setting.Domain)
		if err != nil {
			// edge case for Windows based shells that will add CR LF if piped to ssh-keygen command
			// see https://github.com/PowerShell/PowerShell/issues/5974
			err = sshsig.Verify(bytes.NewBuffer([]byte(token+"\r\n")), []byte(signature), []byte(key.Content), setting.Domain)
		}
	} else {
		var (
			tmpSignature, tmpAuthorizedSigners string
		)
		if tmpSignature, err = writeTmpKeyFile(signature); err != nil {
			return "", err
		}
		defer func() {
			if err := util.Remove(tmpSignature); err != nil {
				log.Warn("Unable to remove temporary signature file: %s: Error: %v", tmpSignature, err)
			}
		}()

		if tmpAuthorizedSigners, err = writeTmpKeyFile(fmt.Sprintf("* %s", key.Content)); err != nil {
			return "", err
		}
		defer func() {
			if err := util.Remove(tmpAuthorizedSigners); err != nil {
				log.Warn("Unable to remove temporary signers file: %s: Error: %v", tmpAuthorizedSigners, err)
			}
		}()

		_, _, err = process.GetManager().ExecDirEnvStdIn(ctx, -1, "", "VerifySSHKey", nil, bytes.NewBuffer([]byte(token)), setting.SSH.KeygenPath,
			"-Y", "verify",
			"-s", tmpSignature,
			"-I", "",
			"-f", tmpAuthorizedSigners,
			"-n", setting.Domain,
		)
	}

	if err != nil {
		log.Error("Unable to validate token signature. Error: %v", err)
		return "", ErrSSHInvalidTokenSignature{
			Fingerprint: key.Fingerprint,
		}
	}

	key.Verified = true
	if _, err := db.GetEngine(ctx).ID(key.ID).Cols("verified").Update(key); err != nil {
		return "", err
	}

	if err := committer.Commit(); err != nil {
		return "", err
	}

	return key.Fingerprint, nil
}
