// Package runtime provides internal owner-scoped durable work mechanisms.
// It does not interpret payloads or decide domain state.
package runtime

import (
	"context"
	"errors"
	"time"

	"github.com/ruipengliu/lerna/contract"
)

// Tx is an opaque adapter token. Storage ports must reject foreign, expired,
// differently owned or differently bound database tokens.
type Tx interface{ Owner() contract.OwnerRef }
type TxRunner interface {
	Within(context.Context, contract.OwnerRef, func(context.Context, Tx) error) error
}
type Clock interface {
	Now(context.Context, Tx) (time.Time, error)
}

var ErrCommitUnknown = errors.New("transaction commit outcome unknown")
var ErrScope = errors.New("transaction scope mismatch or expired")

type CommandMetadata struct {
	Version          contract.ContractVersion `json:"contract_version"`
	Profile          contract.ProfileName     `json:"profile"`
	Method           contract.MethodName      `json:"method"`
	Target           contract.ObjectRef       `json:"target"`
	Subject          contract.SubjectBinding  `json:"subject_binding"`
	AcceptBefore     contract.Time            `json:"accept_before"`
	ExpectedRevision *contract.Revision       `json:"expected_revision,omitempty"`
}

type CommandRecord struct {
	Metadata CommandMetadata
	Digest   string
	Receipt  contract.CommandReceipt
}
type CommandStore interface {
	LockCommand(context.Context, Tx, contract.CommandRef) (*CommandRecord, error)
	SaveCommand(context.Context, Tx, contract.CommandRef, CommandRecord) error
}
type Job struct {
	ID                contract.ID
	Object            contract.ObjectRef
	Phase             string
	WorkRevision      int64
	CompletedRevision int64
	State             string
	DueAt             time.Time
}
type JobStore interface {
	Trigger(context.Context, Tx, contract.ObjectRef, string, int64, time.Time) (Job, error)
}

// Admit locks the original key before checking a new admission's deadline.
// decide owns business preconditions and uses this same short transaction.
// It returns a fixed receipt only when the runner confirms COMMIT.
func Admit(ctx context.Context, runner TxRunner, commands CommandStore, clock Clock, ref contract.CommandRef, digest string, metadata CommandMetadata, cutoff time.Time, decide func(context.Context, Tx, time.Time) (contract.CommandReceipt, error)) (contract.TransportOutcome, error) {
	var receipt contract.CommandReceipt
	err := runner.Within(ctx, ref.Owner, func(txctx context.Context, tx Tx) error {
		original, err := commands.LockCommand(txctx, tx, ref)
		if err != nil {
			return err
		}
		if original != nil {
			if original.Digest != digest {
				return &contract.ContractError{PublicError: contract.PublicError{Code: "idempotency_conflict"}}
			}
			receipt = original.Receipt
			return nil
		}
		now, err := clock.Now(txctx, tx)
		if err != nil {
			return err
		}
		if !now.Before(cutoff) {
			receipt = Rejected(ref, "expired")
		} else {
			receipt, err = decide(txctx, tx, now)
			if err != nil {
				return err
			}
		}
		return commands.SaveCommand(txctx, tx, ref, CommandRecord{Digest: digest, Metadata: metadata, Receipt: receipt})
	})
	if errors.Is(err, ErrCommitUnknown) {
		return contract.NewTransportOutcomeCommitUnknown(contract.TransportOutcomeCommitUnknown{CommandRef: ref, NextAction: "query_or_retransmit_original"}), nil
	}
	if err != nil {
		return contract.TransportOutcome{}, err
	}
	return contract.NewTransportOutcomeReceived(contract.TransportOutcomeReceived{Receipt: receipt}), nil
}
func Rejected(ref contract.CommandRef, reason contract.ErrorCode) contract.CommandReceipt {
	next := "resolve_rejection"
	return contract.NewCommandReceiptRejected(contract.CommandReceiptRejected{CommandRef: ref, Reason: reason, NextAction: &next})
}
