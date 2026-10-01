// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package nativeoperation implements the generic conditional-operation
// service: durable submit/lookup/cancel, the exclusive mutation reservation,
// prepared-commit admission and ordinary-writer participation. It performs no
// factory policy.
package nativeoperation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	sdk "forgejo.org/extension-sdk"
	model "forgejo.org/models/nativeoperation"
	"forgejo.org/modules/git"
	"forgejo.org/modules/setting"
)

// MaxOperationLifetime bounds not_after at submission: a guarded commit
// admission must occur within this host lifetime. It limits admission, not
// physical publication and never releases a reservation.
const MaxOperationLifetime = time.Hour

// Service errors with router-mapped outcomes.
var (
	// ErrInvalidIntent rejects malformed or out-of-scope intents (HTTP 400).
	ErrInvalidIntent = errors.New("invalid operation intent")
	// ErrIntentConflict rejects changed content under a recorded ID (HTTP 409).
	ErrIntentConflict = errors.New("intent_conflict")
	// ErrCancelledBeforeSubmit rejects delayed submission under a cancelled
	// ID (HTTP 409).
	ErrCancelledBeforeSubmit = errors.New("cancelled_before_submit")
	// ErrKindUnavailable reports an admitted kind whose stage is not
	// implemented yet (HTTP 200 operations_unavailable).
	ErrKindUnavailable = errors.New("operations_unavailable")
	// ErrBusy reports a held reservation without recording anything, so the
	// same ID may be retried after release (HTTP 503).
	ErrBusy = errors.New("native mutation reservation is busy")
)

// IsBusy reports whether err is a reservation-contention refusal.
func IsBusy(err error) bool {
	return errors.Is(err, ErrBusy) || errors.Is(err, model.ErrBusy)
}

// RefReader resolves live ref tips for admission and reconciliation.
type RefReader interface {
	CommitID(ctx context.Context, repoPath, ref string) (string, error)
}

type gitRefReader struct{}

func (gitRefReader) CommitID(ctx context.Context, repoPath, ref string) (string, error) {
	return git.GetFullCommitID(ctx, repoPath, ref)
}

// Service executes conditional operations against the native boundary.
type Service struct {
	refs          RefReader
	now           func() int64
	capabilityDir string
}

// NewService returns a service with the real native bindings.
func NewService() *Service {
	return &Service{refs: gitRefReader{}, now: func() int64 { return time.Now().Unix() }}
}

var defaultService = NewService()

// Default returns the shared service for router and hook dispatch.
func Default() *Service {
	return defaultService
}

func (s *Service) clock() int64 {
	if s.now != nil {
		return s.now()
	}
	return time.Now().Unix()
}

func (s *Service) readRef(ctx context.Context, repoPath, ref string) (string, error) {
	refs := s.refs
	if refs == nil {
		refs = gitRefReader{}
	}
	return refs.CommitID(ctx, repoPath, ref)
}

func (s *Service) execDir() (string, error) {
	if s.capabilityDir != "" {
		return s.capabilityDir, nil
	}
	return filepath.Join(setting.AppDataPath, "nativeop-exec"), nil
}

// MergeIntent carries the validated pull_request.merge payload: the exact PR,
// source, target and OIDs the reviewed candidate was verified against.
type MergeIntent struct {
	PullRequestNumber int64  `json:"pull_request_number"`
	HeadRepositoryID  int64  `json:"head_repository_id"`
	HeadRef           string `json:"head_ref"`
	BaseRef           string `json:"base_ref"`
	ExpectedHeadOID   string `json:"expected_head_oid"`
	ExpectedBaseOID   string `json:"expected_base_oid"`
	Method            string `json:"method"`
}

// ValidIntent is a fully validated immutable operation intent with its
// canonical identity digest.
type ValidIntent struct {
	OperationID            string
	ActorID                int64
	RepositoryID           int64
	Kind                   string
	AuthRevision           string
	ExpectedNativeRevision int64
	NotAfter               int64
	Merge                  *MergeIntent
	Digest                 string
	Canonical              string
}

func validOperationID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == ':' {
			continue
		}
		return false
	}
	return true
}

func validBranchRef(ref string) bool {
	if !strings.HasPrefix(ref, git.BranchPrefix) || len(ref) > 512 {
		return false
	}
	name := strings.TrimSpace(ref)
	if name != ref || strings.ContainsAny(ref, " ~^:?*\\") || strings.Contains(ref, "..") {
		return false
	}
	return len(strings.TrimPrefix(ref, git.BranchPrefix)) > 0
}

func validOID(oid string) bool {
	if len(oid) != 40 && len(oid) != 64 {
		return false
	}
	for _, c := range oid {
		if c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' {
			continue
		}
		return false
	}
	return true
}

// ValidateIntent validates the complete immutable intent. Unknown fields,
// conflicting values and out-of-scope requests fail validation rather than
// silently broadening the request. now is host time in Unix seconds.
func ValidateIntent(operationID string, actorID, repositoryID int64, kind, authRevision string, expectedRevision, notAfter int64, payload []byte, now int64) (*ValidIntent, error) {
	if !validOperationID(operationID) || actorID <= 0 || repositoryID <= 0 {
		return nil, ErrInvalidIntent
	}
	if !model.ValidKind(kind) {
		return nil, ErrInvalidIntent
	}
	if authRevision == "" || len(authRevision) > 512 {
		return nil, ErrInvalidIntent
	}
	if expectedRevision < 1 || notAfter <= now || notAfter > now+int64(MaxOperationLifetime/time.Second) {
		return nil, ErrInvalidIntent
	}
	intent := &ValidIntent{
		OperationID:            operationID,
		ActorID:                actorID,
		RepositoryID:           repositoryID,
		Kind:                   kind,
		AuthRevision:           authRevision,
		ExpectedNativeRevision: expectedRevision,
		NotAfter:               notAfter,
	}
	if kind == model.KindMerge {
		merge, err := parseMergeIntent(payload, repositoryID)
		if err != nil {
			return nil, err
		}
		intent.Merge = merge
	} else if len(payload) > 0 {
		var probe map[string]any
		decoder := json.NewDecoder(bytes.NewReader(payload))
		if err := decoder.Decode(&probe); err != nil {
			return nil, ErrInvalidIntent
		}
	}
	canonical, digest, err := canonicalDigest(intent, payload)
	if err != nil {
		return nil, ErrInvalidIntent
	}
	intent.Canonical = canonical
	intent.Digest = digest
	return intent, nil
}

func parseMergeIntent(payload []byte, repositoryID int64) (*MergeIntent, error) {
	if len(payload) == 0 {
		return nil, ErrInvalidIntent
	}
	var merge MergeIntent
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&merge); err != nil {
		return nil, ErrInvalidIntent
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, ErrInvalidIntent
	}
	if merge.PullRequestNumber <= 0 || merge.HeadRepositoryID <= 0 {
		return nil, ErrInvalidIntent
	}
	// F-core proves the same-repository fast-forward-only path; other
	// methods and fork sources refuse without fallback.
	if merge.Method != "fast-forward-only" {
		return nil, ErrInvalidIntent
	}
	if merge.HeadRepositoryID != repositoryID {
		return nil, ErrInvalidIntent
	}
	if !validBranchRef(merge.HeadRef) || !validBranchRef(merge.BaseRef) || merge.HeadRef == merge.BaseRef {
		return nil, ErrInvalidIntent
	}
	if !validOID(merge.ExpectedHeadOID) || !validOID(merge.ExpectedBaseOID) {
		return nil, ErrInvalidIntent
	}
	if strings.EqualFold(merge.ExpectedHeadOID, merge.ExpectedBaseOID) {
		return nil, ErrInvalidIntent
	}
	merge.ExpectedHeadOID = strings.ToLower(merge.ExpectedHeadOID)
	merge.ExpectedBaseOID = strings.ToLower(merge.ExpectedBaseOID)
	return &merge, nil
}

// canonicalDigest binds every semantic intent field to the operation ID so a
// changed authorization revision, expiry or payload cannot reuse it. Intent
// equality compares validated semantic fields, not JSON whitespace or order.
func canonicalDigest(intent *ValidIntent, payload []byte) (string, string, error) {
	var normalizedPayload any
	if len(payload) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(payload))
		if err := decoder.Decode(&normalizedPayload); err != nil {
			return "", "", err
		}
	}
	raw, err := json.Marshal(struct {
		OperationID            string `json:"operation_id"`
		ActorID                int64  `json:"actor_id"`
		RepositoryID           int64  `json:"repository_id"`
		Kind                   string `json:"kind"`
		AuthorizationRevision  string `json:"authorization_revision"`
		ExpectedNativeRevision int64  `json:"expected_native_revision"`
		NotAfter               int64  `json:"not_after"`
		Payload                any    `json:"payload"`
	}{
		OperationID:            intent.OperationID,
		ActorID:                intent.ActorID,
		RepositoryID:           intent.RepositoryID,
		Kind:                   intent.Kind,
		AuthorizationRevision:  intent.AuthRevision,
		ExpectedNativeRevision: intent.ExpectedNativeRevision,
		NotAfter:               intent.NotAfter,
		Payload:                normalizedPayload,
	})
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256(raw)
	return string(raw), hex.EncodeToString(sum[:]), nil
}

// NativeRevisionObservation is one atomic revision/occupancy observation.
type NativeRevisionObservation struct {
	Revision int64
	Idle     bool
}

// ReadNativeRevision atomically observes the host's native mutation revision
// and idle/busy state from one authoritative database snapshot.
func (s *Service) ReadNativeRevision(ctx context.Context) (NativeRevisionObservation, error) {
	reservation, err := model.ReadReservation(ctx)
	if err != nil {
		return NativeRevisionObservation{}, err
	}
	return NativeRevisionObservation{Revision: reservation.Revision, Idle: reservation.Owner == ""}, nil
}

// ToRecord maps a stored operation to its SDK receipt. Tombstones and
// not_observed lookups report actor/repository/intent fields as unavailable
// rather than inventing them, and the private credential fingerprint never
// appears.
func ToRecord(op *model.Operation) sdk.OperationRecord {
	record := sdk.OperationRecord{
		InstallationID:     op.InstallationID,
		OperationID:        op.OperationID,
		Outcome:            op.EffectState,
		ReasonCode:         op.Reason,
		EffectState:        op.EffectState,
		CancellationStatus: displayCancellation(op),
	}
	if op.Submitted {
		record.ActorID = int64String(op.ActorID)
		record.RepositoryID = int64String(op.RepositoryID)
		record.Kind = op.Kind
		record.CompletionState = op.Completion
		record.IntentDigest = op.IntentDigest
		if op.Receipt != "" {
			record.Receipt = json.RawMessage(op.Receipt)
		}
	}
	return record
}

// ToLookup maps a stored operation, or its absence, to an SDK lookup.
func ToLookup(installationID, operationID string, op *model.Operation) sdk.OperationLookup {
	if op == nil {
		return sdk.OperationLookup{InstallationID: installationID, OperationID: operationID, Status: sdk.BackgroundOutcomeNotObserved}
	}
	record := ToRecord(op)
	return sdk.OperationLookup{
		InstallationID: installationID,
		OperationID:    operationID,
		Status:         op.EffectState,
		Record:         &record,
	}
}

// displayCancellation reports the cancellation outcome honestly: pending
// while an effect may still be in flight, cancelled only when no primary
// effect occurred and none can occur, too_late when it already committed,
// and none when no cancellation was ever requested.
func displayCancellation(op *model.Operation) string {
	if op.Revoked {
		switch op.EffectState {
		case model.EffectCommitted:
			return model.CancellationTooLate
		case model.EffectNotCommitted:
			return model.CancellationCancelled
		case model.EffectIndeterminate:
			return model.CancellationIndeterminate
		default:
			return model.CancellationPending
		}
	}
	if op.Cancellation != "" {
		return op.Cancellation
	}
	return model.CancellationNone
}

func int64String(id int64) string {
	if id <= 0 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}
