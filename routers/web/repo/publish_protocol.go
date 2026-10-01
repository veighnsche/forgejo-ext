// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

// Conditional publish (git.ref.publish) receive protocol contract.
//
// The conditional receive is a new generic Fountain protocol on the smart-HTTP
// transport: a registered operation's publisher pushes exactly its authorized
// tuple through receive-pack, and the server binds that receive to the
// installation/actor/operation before Git starts. This file pins the
// transport-shape points of that contract that are decidable without the held
// operation seam:
//
//   - Only git-receive-pack serves a conditional publish. Upload-pack,
//     upload-archive and the dumb-HTTP endpoints never consume a registration;
//     discovery reads do not consume execution.
//   - The Git-Protocol header passes through under the existing native
//     allowlist, unchanged. Operation binding never travels in that header,
//     so no new header value is accepted here.
//   - Operation binding is never a push option. The conditional receive
//     rejects every push option, which also keeps policy push options from
//     smuggling intent past the registered tuple.
//
// Held for the seam wiring turn (needs the operation/recovery owners and
// background admission): the exact binding encoding that carries the
// operation ID plus current installation admission alongside the native PAT
// on the supported receive route, the route identity resolution by stable
// repository ID, the atomic claim before receive-pack launch, the host-only
// execution environment for the Git child with caller admission removed, and
// the diagnostic redaction around that material. Those pieces touch the held
// seam and are named here only as constraints, not standardized twice.

import (
	"errors"
	"fmt"
)

// PublishReceiveService is the only Git service that may serve a conditional
// publish receive.
const PublishReceiveService = "receive-pack"

// ErrPublishServiceRejected reports a Git service that cannot serve a
// conditional publish.
var ErrPublishServiceRejected = errors.New("service cannot serve a conditional publish")

// ValidatePublishService accepts only the receive-pack service for a
// conditional publish. Every other service name, including an empty one,
// rejects: a registration is consumed only by its matched receive.
func ValidatePublishService(service string) error {
	if service != PublishReceiveService {
		return fmt.Errorf("%w: %q", ErrPublishServiceRejected, service)
	}
	return nil
}

// ErrPublishProtocolRejected reports a Git-Protocol header the conditional
// receive cannot pass through.
var ErrPublishProtocolRejected = errors.New("git protocol header is not allowed on a conditional publish")

// ValidatePublishGitProtocol enforces the native header allowlist on the
// conditional receive. An empty header stays valid; a present header must
// satisfy the same safe shape the ordinary receive-pack path requires. The
// operation binding never travels here, so no additional value is accepted.
func ValidatePublishGitProtocol(header string) error {
	if header == "" {
		return nil
	}
	if !safeGitProtocolHeader.MatchString(header) {
		return fmt.Errorf("%w: %q", ErrPublishProtocolRejected, header)
	}
	return nil
}

// ErrPublishPushOptionsRejected reports push options on a conditional
// publish. The registered tuple is the complete intent; options cannot extend
// or select it.
var ErrPublishPushOptionsRejected = errors.New("push options are not allowed on a conditional publish")

// ValidatePublishPushOptions rejects every push option on a conditional
// publish receive, including an empty-valued map entry: presence alone
// rejects, regardless of content.
func ValidatePublishPushOptions(options map[string]string) error {
	if len(options) > 0 {
		return ErrPublishPushOptionsRejected
	}
	return nil
}
