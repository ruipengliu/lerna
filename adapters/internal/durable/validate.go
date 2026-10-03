// Package durable 只共享存储合同的纯校验，不保存业务权威或执行 SQL。
package durable

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

var namespacePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(?:[./][a-z][a-z0-9_]*)*$`)

func DatabaseIdentity(actual, expected string) error {
	if expected != "" && !api.ValidID(expected) {
		return api.E("invalid_request", "invalid_expected_database_identity")
	}
	if actual != "" && !api.ValidID(actual) {
		return api.E("invalid_state", "invalid_stored_database_identity")
	}
	if expected != "" && actual != expected {
		return api.E("invalid_state", "original_database_missing_or_replaced")
	}
	return nil
}

func Scope(scope runtime.Scope, databaseID string) error {
	if databaseID == "" || scope.DatabaseID != databaseID || !api.ValidID(scope.TenantID) || !api.ValidID(scope.OwnerID) {
		return api.E("forbidden", "transaction_scope_mismatch")
	}
	return nil
}

func Namespace(namespace string) error {
	if len(namespace) > 160 || !namespacePattern.MatchString(namespace) {
		return api.E("invalid_request", "invalid_namespace")
	}
	return nil
}

func Participants(names []string) (map[string]bool, error) {
	if len(names) > 64 {
		return nil, api.E("invalid_request", "too_many_participants")
	}
	m := make(map[string]bool, len(names))
	for _, name := range names {
		if err := Namespace(name); err != nil {
			return nil, err
		}
		if strings.ContainsAny(name, "./") {
			return nil, api.E("invalid_request", "participant_must_be_namespace_root")
		}
		m[name] = true
	}
	return m, nil
}

func Participant(namespace string, names map[string]bool) error {
	if err := Namespace(namespace); err != nil {
		return err
	}
	root := strings.FieldsFunc(namespace, func(r rune) bool { return r == '.' || r == '/' })[0]
	if !names[root] {
		return api.E("forbidden", "undeclared_transaction_participant")
	}
	return nil
}

func Identity(id string) error {
	if !api.ValidID(id) {
		return api.E("invalid_request", "invalid_identity")
	}
	return nil
}

func Text(text string) error {
	if len(text) == 0 || len(text) > 512 || strings.ContainsRune(text, 0) {
		return api.E("invalid_request", "invalid_storage_key")
	}
	return nil
}

func Record(value any) ([]byte, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return api.Canonical(b)
}

const maxCommandEnvelopeBytes = 1 << 20

// CommandRecord 的聚合上限容纳各自合法的原命令与回执；不扩大单份领域边界。
func CommandRecord(value runtime.StoredCommand) ([]byte, error) {
	if _, err := Record(value.Command); err != nil {
		return nil, err
	}
	if _, err := Record(value.Receipt); err != nil {
		return nil, err
	}
	b, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return api.CanonicalLimit(b, maxCommandEnvelopeBytes)
}

func DecodeCommandRecord(raw []byte) (runtime.StoredCommand, error) {
	var value runtime.StoredCommand
	if err := api.DecodeLimit(raw, &value, maxCommandEnvelopeBytes); err != nil {
		return value, err
	}
	if _, err := Record(value.Command); err != nil {
		return value, err
	}
	if _, err := Record(value.Receipt); err != nil {
		return value, err
	}
	return value, nil
}

func Limits(limit int) error {
	if limit < 1 || limit > 1000 {
		return api.E("invalid_request", "invalid_storage_scan_limit")
	}
	return nil
}

func Time(value time.Time) (int64, error) {
	if value.IsZero() || value.Year() < 1970 || value.Year() >= 2262 {
		return 0, api.E("invalid_request", "invalid_storage_time")
	}
	return value.UTC().UnixNano(), nil
}

func Claim(scope runtime.Scope, claim api.Claim) error {
	if claim.TenantID != scope.TenantID || claim.OwnerID != scope.OwnerID || !api.ValidID(claim.JobID) || !api.ValidID(claim.HolderID) || claim.LeaseEpoch == 0 || claim.LeaseEpoch > api.MaxSafeInteger || claim.ObservedWorkRevision == 0 || claim.ObservedWorkRevision > api.MaxSafeInteger {
		return runtime.ErrClaimLost
	}
	if _, err := api.ParseTime(claim.LeaseUntil); err != nil {
		return runtime.ErrClaimLost
	}
	return nil
}

func Lease(lease time.Duration) error {
	if lease < time.Millisecond || lease > time.Hour {
		return api.E("invalid_request", "invalid_lease_duration")
	}
	return nil
}

// Command 保留原请求与主体；终态只能重存同一决定，墓碑不能重开。
func Command(scope runtime.Scope, old *runtime.StoredCommand, next runtime.StoredCommand) error {
	if next.Command.LogicalServiceID != scope.OwnerID || !api.ValidID(next.Command.CommandID) || !api.ValidID(next.PrincipalID) || next.Digest == "" || next.Receipt.CommandID != next.Command.CommandID || next.Receipt.RequestDigest != next.Digest {
		return api.E("invalid_request", "invalid_stored_command")
	}
	if next.Receipt.Stage != "accepted" && next.Receipt.Stage != "applied" && next.Receipt.Stage != "rejected" {
		return api.E("invalid_request", "invalid_receipt_phase")
	}
	if old == nil {
		return nil
	}
	if old.PrincipalID != next.PrincipalID || old.Digest != next.Digest || !api.Equal(api.Raw(old.Command), api.Raw(next.Command)) {
		return api.E("idempotency_conflict", "command_input_changed")
	}
	if old.Tombstone && !next.Tombstone {
		return api.E("gone", "receipt_collected")
	}
	if old.Receipt.Stage != "accepted" && !next.Tombstone && !api.Equal(api.Raw(old.Receipt), api.Raw(next.Receipt)) {
		return api.E("invalid_state", "command_already_decided")
	}
	return nil
}
