// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package admin

import (
	"net/http"

	"forgejo.org/models/forgefed"
	"forgejo.org/services/context"
)

// SetFederationHostBlocked marks a federation host as blocked (or unblocked).
// A blocked host is never contacted for federation, inbound or outbound.
func SetFederationHostBlocked(ctx *context.APIContext) {
	// swagger:operation POST /admin/federation/hosts/{id}/block admin adminSetFederationHostBlocked
	// ---
	// summary: Block or unblock a federation host
	// produces:
	// - application/json
	// parameters:
	// - name: id
	//   in: path
	//   description: id of the federation host
	//   type: integer
	//   format: int64
	//   required: true
	// - name: body
	//   in: body
	//   schema:
	//     type: object
	//     properties:
	//       blocked:
	//         type: boolean
	// responses:
	//   "204":
	//     "$ref": "#/responses/empty"
	//   "404":
	//     "$ref": "#/responses/notFound"

	host, err := forgefed.GetFederationHost(ctx, ctx.ParamsInt64(":id"))
	if err != nil {
		ctx.NotFound("GetFederationHost", err)
		return
	}

	blocked := ctx.FormBool("blocked")
	if err := forgefed.SetFederationHostBlocked(ctx, host.ID, blocked); err != nil {
		ctx.Error(http.StatusInternalServerError, "SetFederationHostBlocked", err)
		return
	}
	ctx.Status(http.StatusNoContent)
}
