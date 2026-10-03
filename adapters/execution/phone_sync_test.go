package execution_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	target "github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/api"
	domain "github.com/ruipengliu/lerna/internal/execution"
	rt "github.com/ruipengliu/lerna/runtime"
)

func TestPhoneRestoresPrivateTargetJournalLargerThanDomainLimit(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	id := api.NewID("resource")
	scope := rt.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("executor")}
	instance := api.NewID("instance")
	attempts := []map[string]any{}
	for index := range 1000 {
		attemptID := api.NewID("attempt")
		attempts = append(attempts, map[string]any{"attempt_id": attemptID, "operation_id": api.NewID("operation"), "input_digest": api.Hash([]byte("fixed retained history")), "result": target.PhoneActionResult{ResourceID: id, OriginalAttemptID: attemptID, TargetVersion: strconv.Itoa(index + 2), Applied: true, Atomic: true}})
	}
	ledger := map[string]any{"resource_id": id, "tenant_id": scope.TenantID, "instance_id": instance, "control_epoch": uint64(1), "automatic": true, "state": target.PhoneState{Screen: "notes", Note: "retained original target", Version: 1001}, "attempts": attempts}
	body, err := json.Marshal(ledger)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) <= api.MaxJSONBytes || len(body) > 8<<20 {
		t.Fatalf("private native ledger fixture size %d", len(body))
	}
	if err = os.WriteFile(filepath.Join(root, id+".json"), body, 0600); err != nil {
		t.Fatal(err)
	}
	phones, err := target.NewSimulatedPhones(root, []string{id})
	if err != nil {
		t.Fatalf("legitimate >domain native ledger cannot restore: %v", err)
	}
	defer func() {
		if phones != nil {
			if err := phones.Close(); err != nil {
				t.Error(err)
			}
		}
	}()
	lease := domain.ResourceLease{ResourceID: id, HolderID: api.NewID("holder"), InstanceID: instance, ControlEpoch: 1, State: "held", LeaseUntil: api.Time(time.Now().Add(time.Minute))}
	before, err := phones.Observe(ctx, scope, lease, api.NewID("observation"))
	if err != nil {
		t.Fatal(err)
	}
	args := target.PhoneActionArguments{ResourceID: id, InstanceID: instance, ControlEpoch: 1, ObservationID: before.ObservationID, TargetVersion: before.TargetVersion, ActionBefore: before.ActionBefore, Action: "set_note", Value: "latest exact action after native restore"}
	prepared, err := phones.Prepare(ctx, scope, rt.Auth{}, domain.InvokeInput{}, domain.ExecutionIntent{}, api.Raw(args))
	if err != nil {
		t.Fatal(err)
	}
	original := domain.AttemptRequest{Scope: scope, Invoke: domain.InvokeInput{OperationID: api.NewID("operation")}, Attempt: domain.Attempt{AttemptID: api.NewID("attempt"), Prepared: prepared}}
	entered := 0
	first, err := phones.Start(ctx, original, func(context.Context) error { entered++; return nil })
	if err != nil || first.Effect != "applied" {
		t.Fatalf("latest native attempt failed %+v %v", first, err)
	}
	if err = phones.Close(); err != nil {
		t.Fatal(err)
	}
	phones = nil
	phones, err = target.NewSimulatedPhones(root, []string{id})
	if err != nil {
		t.Fatal(err)
	}
	restored, err := phones.Reconcile(ctx, original)
	if err != nil || !api.Equal(restored, first) {
		t.Fatalf("large native ledger changed original latest attempt %+v %v", restored, err)
	}
	if _, err = phones.Start(ctx, original, func(context.Context) error { entered++; return nil }); err != nil || entered != 1 {
		t.Fatalf("restored native action physically replayed entered=%d %v", entered, err)
	}
}

func TestPhoneReconciliationReadsRenamedTargetAfterDirectorySyncFailure(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	id := api.NewID("resource")
	phones, err := target.NewSimulatedPhones(root, []string{id})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if phones != nil {
			if err := phones.Close(); err != nil {
				t.Error(err)
			}
		}
	}()
	scope := rt.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("executor")}
	lease := domain.ResourceLease{ResourceID: id, HolderID: api.NewID("holder"), InstanceID: api.NewID("instance"), ControlEpoch: 1, State: "held", LeaseUntil: api.Time(time.Now().Add(time.Minute))}
	if _, err = phones.Fence(ctx, scope, lease); err != nil {
		t.Fatal(err)
	}
	before, err := phones.Observe(ctx, scope, lease, api.NewID("observation"))
	if err != nil {
		t.Fatal(err)
	}
	args := target.PhoneActionArguments{ResourceID: id, InstanceID: lease.InstanceID, ControlEpoch: 1, ObservationID: before.ObservationID, TargetVersion: before.TargetVersion, ActionBefore: before.ActionBefore, Action: "set_note", Value: "exact note renamed before fsync failure"}
	prepared, err := phones.Prepare(ctx, scope, rt.Auth{}, domain.InvokeInput{}, domain.ExecutionIntent{}, api.Raw(args))
	if err != nil {
		t.Fatal(err)
	}
	request := domain.AttemptRequest{Scope: scope, Invoke: domain.InvokeInput{OperationID: api.NewID("operation")}, Attempt: domain.Attempt{AttemptID: api.NewID("attempt"), Prepared: prepared}}
	entered := 0
	phones.Fault = func(resource, point string) error {
		if resource == id && point == "after_rename_before_directory_sync" {
			return syscall.EIO
		}
		return nil
	}
	if _, err = phones.Start(ctx, request, func(context.Context) error { entered++; return nil }); !errors.Is(err, syscall.EIO) {
		t.Fatalf("actual post-rename sync fault missing: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(root, id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var disk struct {
		ResourceID   string            `json:"resource_id"`
		TenantID     string            `json:"tenant_id,omitempty"`
		InstanceID   string            `json:"instance_id,omitempty"`
		ControlEpoch uint64            `json:"control_epoch"`
		Automatic    bool              `json:"automatic"`
		State        target.PhoneState `json:"state"`
		Attempts     []struct {
			AttemptID   string                   `json:"attempt_id"`
			OperationID string                   `json:"operation_id"`
			InputDigest string                   `json:"input_digest"`
			Result      target.PhoneActionResult `json:"result"`
		} `json:"attempts"`
	}
	if api.Decode(body, &disk) != nil || disk.State.Note != args.Value || disk.State.Version != 2 || len(disk.Attempts) != 1 || disk.Attempts[0].AttemptID != request.Attempt.AttemptID {
		t.Fatalf("independent target bytes do not prove original applied attempt: %s", body)
	}
	phones.Fault = nil
	fact, err := phones.Reconcile(ctx, request)
	if err != nil || fact.Effect != "applied" || !fact.UsageFinal || !api.Equal(fact.MayApplyLater, false) {
		t.Fatalf("same-process reconciliation denied renamed original attempt %+v %v", fact, err)
	}
	after, err := phones.Observe(ctx, scope, lease, api.NewID("observation"))
	var state target.PhoneState
	if err != nil || api.Decode(after.Data, &state) != nil || state.Note != disk.State.Note || state.Version != disk.State.Version {
		t.Fatalf("observation used stale process cache %+v %v", state, err)
	}
	// 无法读取/校验真实介质时继续未知，不允许缓存制造 definitive not_applied。
	if err = os.WriteFile(filepath.Join(root, id+".json"), []byte(`{"corrupt":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if fact, err = phones.Reconcile(ctx, request); err == nil {
		t.Fatalf("corrupt target became definitive %+v", fact)
	}
	if err = os.WriteFile(filepath.Join(root, id+".json"), body, 0600); err != nil {
		t.Fatal(err)
	}
	if err = phones.Close(); err != nil {
		t.Fatal(err)
	}
	phones = nil
	phones, err = target.NewSimulatedPhones(root, []string{id})
	if err != nil {
		t.Fatal(err)
	}
	fact, err = phones.Reconcile(ctx, request)
	if err != nil || fact.Effect != "applied" {
		t.Fatalf("original renamed attempt did not survive restart %+v %v", fact, err)
	}
	if _, err = phones.Start(ctx, request, func(context.Context) error { entered++; return nil }); err != nil || entered != 1 {
		t.Fatalf("original attempt performed another physical action after restart: entered=%d err=%v", entered, err)
	}
}
