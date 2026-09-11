// Copyright 2021 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package common

import (
	"io"
	"time"

	git_model "forgejo.org/models/git"
	"forgejo.org/modules/git"
	"forgejo.org/modules/httpcache"
	"forgejo.org/modules/httplib"
	"forgejo.org/modules/lfs"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/storage"
	"forgejo.org/services/context"
)

// ServeBlob serves a [git.Blob] (redirects to LFS if necessary,
// with caching support).
//
// Git LFS pointers shall be resolved; their target shall be provided.
// See also: [ServeBlobRaw]
func ServeBlob(ctx *context.Base, repo *context.Repository, blob *git.Blob, lastModified *time.Time) error {
	if httpcache.HandleGenericETagTimeCache(ctx.Req, ctx.Resp, `"`+blob.ID.String()+`"`, lastModified) {
		return nil
	}

	dataRc, err := blob.DataAsync()
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if closed {
			return
		}
		if err = dataRc.Close(); err != nil {
			log.Error("ServeBlob: Close: %v", err)
		}
	}()

	pointer, err := lfs.ReadPointer(dataRc)
	if err == nil {
		meta, _ := git_model.GetLFSMetaObjectByOid(ctx, repo.Repository.ID, pointer.Oid)
		if meta == nil {
			if err = dataRc.Close(); err != nil {
				log.Error("ServeBlob: Close: %v", err)
			}
			closed = true
			return ServeBlobRaw(ctx, repo, blob, lastModified)
		}
		if httpcache.HandleGenericETagCache(ctx.Req, ctx.Resp, `"`+pointer.Oid+`"`) {
			return nil
		}

		if setting.LFS.Storage.MinioConfig.ServeDirect {
			// If we have a signed url (S3, object storage, blob storage), redirect to this directly.
			u, err := storage.LFS.URL(pointer.RelativePath(), blob.Name(), nil)
			if u != nil && err == nil {
				ctx.Redirect(u.String())
				return nil
			}
		}

		lfsDataRc, err := lfs.ReadMetaObject(meta.Pointer)
		if err != nil {
			return err
		}
		defer func() {
			if err = lfsDataRc.Close(); err != nil {
				log.Error("ServeBlob: Close: %v", err)
			}
		}()
		ServeContentByReadSeeker(ctx, repo.TreePath, lastModified, lfsDataRc)
		return nil
	}
	if err = dataRc.Close(); err != nil {
		log.Error("ServeBlob: Close: %v", err)
	}
	closed = true

	return ServeBlobRaw(ctx, repo, blob, lastModified)
}

// ServeBlobRaw serves a [git.Blob] as-is (with caching support).
// For "Git LFS files", the file's pointer is served instead of its target.
// See also: [ServeBlob]
func ServeBlobRaw(ctx *context.Base, repo *context.Repository, blob *git.Blob, lastModified *time.Time) error {
	if httpcache.HandleGenericETagTimeCache(ctx.Req, ctx.Resp, `"`+blob.ID.String()+`"`, lastModified) {
		return nil
	}

	dataRc, err := blob.DataAsync()
	if err != nil {
		return err
	}
	defer func() {
		if err = dataRc.Close(); err != nil {
			log.Error("ServeBlobRaw: Close: %v", err)
		}
	}()

	ServeContentByReader(ctx, repo.TreePath, blob.Size(), dataRc)
	return nil
}

func ServeContentByReader(ctx *context.Base, filePath string, size int64, reader io.Reader) {
	httplib.ServeContentByReader(ctx.Req, ctx.Resp, filePath, size, reader)
}

func ServeContentByReadSeeker(ctx *context.Base, filePath string, modTime *time.Time, reader io.ReadSeeker) {
	httplib.ServeContentByReadSeeker(ctx.Req, ctx.Resp, filePath, modTime, reader)
}
