package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/runtime"
)

func lockKey(namespace string, owner contract.OwnerRef, id string) string {
	// JSON arrays are unambiguous even when IDs contain punctuation.
	data, _ := json.Marshal([]string{namespace, string(owner.TenantID), string(owner.OwnerID), id})
	return string(data)
}
func (s *Store) LockCommand(ctx context.Context, token runtime.Tx, ref contract.CommandRef) (*runtime.CommandRecord, error) {
	tx, err := s.token(ctx, token, ref.Owner)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(1,hashtext($1))`, lockKey("command", ref.Owner, string(ref.CommandID))); err != nil {
		return nil, err
	}
	var digest string
	var data, metadata []byte
	err = tx.QueryRowContext(ctx, `SELECT digest,metadata,receipt FROM `+s.table("command_receipts")+` WHERE tenant_id=$1 AND owner_id=$2 AND command_id=$3`, ref.Owner.TenantID, ref.Owner.OwnerID, ref.CommandID).Scan(&digest, &metadata, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	receipt, err := contract.Decode[contract.CommandReceipt](data)
	if err != nil {
		return nil, err
	}
	var original runtime.CommandMetadata
	if err = json.Unmarshal(metadata, &original); err != nil {
		return nil, err
	}
	return &runtime.CommandRecord{Digest: digest, Metadata: original, Receipt: receipt}, nil
}
func (s *Store) SaveCommand(ctx context.Context, token runtime.Tx, ref contract.CommandRef, record runtime.CommandRecord) error {
	tx, err := s.token(ctx, token, ref.Owner)
	if err != nil {
		return err
	}
	data, err := contract.Encode(record.Receipt)
	if err != nil {
		return err
	}
	// Cross-reference validation also protects this internal store seam.
	response := contract.NewCommandGetResponseFound(contract.CommandGetResponseFound{CommandRef: ref, Receipt: record.Receipt, Progress: contract.NewCommandProgressNone(contract.CommandProgressNone{})})
	if _, err = contract.EncodeCommandResponse(response, ref); err != nil {
		return err
	}
	metadata, err := json.Marshal(record.Metadata)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO `+s.table("command_receipts")+`(tenant_id,owner_id,command_id,digest,metadata,receipt) VALUES($1,$2,$3,$4,$5,$6)`, ref.Owner.TenantID, ref.Owner.OwnerID, ref.CommandID, record.Digest, metadata, data)
	return err
}
func (s *Store) ReadCommand(ctx context.Context, ref contract.CommandRef) (contract.CommandGetResponse, error) {
	if _, finite := ctx.Deadline(); !finite {
		return contract.CommandGetResponse{}, errors.New("finite read deadline required")
	}
	var result contract.CommandGetResponse
	err := s.Within(ctx, ref.Owner, func(ctx context.Context, token runtime.Tx) error {
		tx, err := s.token(ctx, token, ref.Owner)
		if err != nil {
			return err
		}
		var data []byte
		err = tx.QueryRowContext(ctx, `SELECT receipt FROM `+s.table("command_receipts")+` WHERE tenant_id=$1 AND owner_id=$2 AND command_id=$3`, ref.Owner.TenantID, ref.Owner.OwnerID, ref.CommandID).Scan(&data)
		if errors.Is(err, sql.ErrNoRows) {
			result = contract.NewCommandGetResponseNotFound(contract.CommandGetResponseNotFound{CommandRef: ref})
			return nil
		}
		if err != nil {
			return err
		}
		receipt, err := contract.Decode[contract.CommandReceipt](data)
		if err != nil {
			return err
		}
		result = contract.NewCommandGetResponseFound(contract.CommandGetResponseFound{CommandRef: ref, Receipt: receipt, Progress: contract.NewCommandProgressNone(contract.CommandProgressNone{})})
		return nil
	})
	return result, err
}
