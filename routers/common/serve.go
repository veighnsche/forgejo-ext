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
	// Deal with caching (blob ID).
	if httpcache.HandleGenericETagTimeCache(ctx.Req, ctx.Resp, `"`+blob.ID.String()+`"`, lastModified) {
		return nil
	}

	// If it's over 1024 bytes, it can't be an LFS file.
	if blob.Size() > lfs.BlobSizeCutoff {
		// First handle caching for the blob
		if httpcache.HandleGenericETagTimeCache(ctx.Req, ctx.Resp, `"`+blob.ID.String()+`"`, lastModified) {
			return nil
		}

		// OK not cached - serve!
		return ServeBlobRaw(ctx, repo, blob, lastModified)
	}

	// Now that we don't know whether this is an LFS file or not,
	// let's investigate...
	//
	// (dataRc should be closed before ServeBlobRaw is called)
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
	if err != nil {
		// First handle caching for the blob
		if httpcache.HandleGenericETagTimeCache(ctx.Req, ctx.Resp, `"`+blob.ID.String()+`"`, lastModified) {
			return nil
		}

		// OK not cached - serve!
		if err = dataRc.Close(); err != nil {
			log.Error("ServeBlob: Close: %v", err)
		}
		closed = true
		return ServeBlobRaw(ctx, repo, blob, lastModified)
	}

	// Now check if there is a MetaObject for this pointer
	meta, err := git_model.GetLFSMetaObjectByOid(ctx, repo.Repository.ID, pointer.Oid)
	// If there isn't one, just serve the data directly
	if err != nil {
		if err == git_model.ErrLFSObjectNotExist || err == lfs.ErrInvalidOIDFormat {
			// Handle caching for the blob SHA (not the LFS object OID)
			if httpcache.HandleGenericETagTimeCache(ctx.Req, ctx.Resp, `"`+blob.ID.String()+`"`, lastModified) {
				return nil
			}

			// Again, not cached - serve!
			if err = dataRc.Close(); err != nil {
				log.Error("ServeBlob: Close: %v", err)
			}
			closed = true
			return ServeBlobRaw(ctx, repo, blob, lastModified)
		}

		return err
	}

	// Handle caching for the LFS object OID
	if httpcache.HandleGenericETagCache(ctx.Req, ctx.Resp, `"`+pointer.Oid+`"`) {
		return nil
	}

	if setting.LFS.Storage.MinioConfig.ServeDirect {
		// If we have a signed url (S3, object storage, blob storage), redirect to this directly.
		u, err := storage.LFS.URL(pointer.RelativePath(), pointer.Oid, nil)
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
			log.Error("ServeBlobOrLFS: Close: %v", err)
		}
	}()

	if err = dataRc.Close(); err != nil {
		log.Error("ServeBlob: Close: %v", err)
	}
	closed = true
	ServeContentByReadSeeker(ctx, repo.TreePath, lastModified, lfsDataRc)
	return nil
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
