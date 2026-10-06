package budget

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"unicode/utf8"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type BillingEvidence interface {
	QueryObservation(context.Context, *v1.Caller, *v1.Ref) (*v1.RawObservation, error)
	Read(context.Context, *v1.Caller, *v1.Ref) (*v1.Content, error)
}
type billingStore interface {
	SaveBillingSource(context.Context, *v1.BillingSource) error
	SaveBillingConflict(context.Context, *v1.BillingConflict) error
	LoadBillingConflict(context.Context, *v1.Ref) (*v1.BillingConflict, error)
	LoadBillingSource(context.Context, *v1.Ref) (*v1.BillingSource, error)
	SaveBillingEntry(context.Context, *v1.BillingEntry) error
	LoadBillingEntry(context.Context, *v1.Ref) (*v1.BillingEntry, error)
	LoadBillingAlias(context.Context, string) (string, error)
	SaveBillingAlias(context.Context, string, string) error
}

func (s *Service) WithBillingEvidence(e BillingEvidence) *Service { s.evidence = e; return s }

type referenceBill struct {
	Rule           string `json:"rule"`
	Namespace      string `json:"namespace"`
	Account        string `json:"account"`
	NativeInstance string `json:"native_instance"`
	Component      string `json:"component"`
	SendID         string `json:"send_id"`
	ExternalKey    string `json:"external_key"`
	SourceVersion  uint64 `json:"source_version"`
	Unit           string `json:"unit"`
	Amount         *int64 `json:"amount"`
	Final          bool   `json:"final"`
	PriceVersion   string `json:"price_version"`
}

// uniqueJSON 拒绝重复字段和尾随文档；不能由解析器挑选费用证据。
func uniqueJSON(raw []byte) bool {
	if !utf8.Valid(raw) {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value func() error
	value = func() error {
		tok, e := d.Token()
		if e != nil {
			return e
		}
		delimiter, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return e
				}
				key, ok := k.(string)
				if !ok || seen[key] {
					return command.Fail("BILLING_CONFLICT")
				}
				seen[key] = true
				if e = value(); e != nil {
					return e
				}
			}
			_, e = d.Token()
			return e
		case '[':
			for d.More() {
				if e = value(); e != nil {
					return e
				}
			}
			_, e = d.Token()
			return e
		default:
			return command.Fail("INVALID_BILL")
		}
	}
	if value() != nil {
		return false
	}
	_, e := d.Token()
	return e == io.EOF
}
func parseBill(raw []byte) (*referenceBill, error) {
	if !uniqueJSON(raw) {
		return nil, nil
	}
	var envelope struct {
		Billing json.RawMessage `json:"billing"`
	}
	if json.Unmarshal(raw, &envelope) != nil || len(envelope.Billing) == 0 {
		return nil, nil
	}
	var bill referenceBill
	d := json.NewDecoder(bytes.NewReader(envelope.Billing))
	d.DisallowUnknownFields()
	if d.Decode(&bill) != nil {
		return nil, nil
	}
	if bill.Rule != "reference-billing-v1" || bill.Namespace != "lerna-reference" || bill.Account != "reference-account" || bill.NativeInstance == "" || bill.Component != "call" || bill.SourceVersion == 0 || bill.Unit != "USD_MICRO" || bill.PriceVersion != "reference-price-v1" || bill.Amount == nil || *bill.Amount < 0 || !bill.Final {
		return nil, nil
	}
	return &bill, nil
}
func (s *Service) settleReport(ctx context.Context, caller *v1.Caller, u *v1.UsageReport) error {
	source, e := s.store.(billingStore).LoadBillingSource(ctx, u.SendRef)
	if e != nil {
		return e
	}
	if source == nil || !proto.Equal(source.SendRef.Name, u.SendRef.Name) || !proto.Equal(source.TaskId, u.TaskId) || !proto.Equal(source.OperationId, u.OperationId) || !proto.Equal(source.PriceRuleRef, u.PriceRuleRef) || u.Unit != source.Unit || !proto.Equal(u.BillingSource, u.SendRef) {
		return command.Fail("INVALID_USAGE_SOURCE")
	}
	raw, e := s.evidence.QueryObservation(ctx, caller, u.MeasurementRef)
	if e != nil {
		return e
	}
	if raw == nil || !proto.Equal(raw.SendRef, u.SendRef) || !proto.Equal(raw.TaskId, u.TaskId) || !proto.Equal(raw.AttemptId, u.AttemptId) || !proto.Equal(raw.OperationId, u.OperationId) || raw.Source != "TRUSTED_IO" {
		return command.Fail("INVALID_USAGE_SOURCE")
	}
	body, e := s.evidence.Read(ctx, caller, raw.BodyRef)
	if e != nil {
		return e
	}
	if body == nil || body.Status != "AVAILABLE" {
		return command.Fail("CONTENT_UNUSABLE")
	}
	bill, e := parseBill(command.ContentBytes(body))
	if raw.Protocol == "FILE" {
		bill, e = s.fileZeroBill(ctx, caller, raw)
	}
	if e != nil {
		return e
	}
	if bill == nil {
		return nil
	}
	if bill.SendID != raw.SendRef.Name.LocalId || bill.ExternalKey != raw.ExternalKey {
		return s.recordConflict(ctx, source, raw.Ref, "BILLING_BINDING_MISMATCH", "")
	}
	return s.applyBill(ctx, source, bill, raw.Ref)
}
func (s *Service) applyBill(ctx context.Context, source *v1.BillingSource, bill *referenceBill, evidence *v1.Ref) error {
	store := s.store.(billingStore)
	alias := command.SemanticFingerprint("billing-alias", bill.Namespace, bill.Account, bill.NativeInstance, bill.Component)
	fingerprint := command.SemanticFingerprint("billing-fact", alias, bill.SourceVersion, bill.Unit, *bill.Amount, bill.Rule, bill.PriceVersion, bill.Final)
	owner, e := store.LoadBillingAlias(ctx, alias)
	if e != nil {
		return e
	}
	if owner != "" && owner != source.SendRef.Name.LocalId {
		return s.recordConflict(ctx, source, evidence, "BILLING_ALIAS_CONFLICT", alias)
	}
	if source.Status == "SETTLED" {
		entry, e := store.LoadBillingEntry(ctx, source.EntryRef)
		if e != nil {
			return e
		}
		if entry.SemanticFingerprint == fingerprint {
			return nil
		}
		if bill.SourceVersion == source.SourceVersion {
			return s.recordConflict(ctx, source, evidence, "BILLING_VERSION_CONFLICT", alias)
		}
		return command.Fail("UNSUPPORTED_BILLING_CORRECTION")
	}
	if source.Status != "PENDING" {
		return command.Fail("BILLING_SOURCE_CLOSED")
	}
	r, e := s.store.(sendStore).LoadCurrentReservation(ctx, source.ReservationRef)
	if e != nil {
		return e
	}
	if r == nil || r.Status != "RESERVED" || !proto.Equal(r.TaskId, source.TaskId) || !proto.Equal(r.OperationId, source.OperationId) {
		return command.Fail("INVALID_RESERVATION")
	}
	amount := *bill.Amount
	for _, id := range []*v1.GlobalName{nil, r.TaskId} {
		b, e := s.store.LoadBudget(ctx, id)
		if e != nil {
			return e
		}
		if b == nil || b.Reserved < r.Ceiling || b.Settled > math.MaxInt64-amount {
			return command.Fail("AMOUNT_OVERFLOW")
		}
		b.Reserved -= r.Ceiling
		b.Settled += amount
		b.CeilingViolation = b.CeilingViolation || amount > r.Ceiling
		b.Ref.Revision++
		if e = projectBudget(b); e != nil {
			return e
		}
		if e = s.saveBudget(ctx, b); e != nil {
			return e
		}
	}
	released := int64(0)
	if amount < r.Ceiling {
		released = r.Ceiling - amount
	}
	entry := &v1.BillingEntry{Ref: command.NewRef(s.user, s.domain, "billing-entry", "lerna.v1.BillingEntry"), SourceRef: source.Ref, EvidenceRef: evidence, SourceVersion: bill.SourceVersion, Amount: amount, Released: released, Kind: "SETTLEMENT", MeasurementRule: bill.Rule, PriceVersion: bill.PriceVersion, Alias: alias, SemanticFingerprint: fingerprint, CeilingExceeded: amount > r.Ceiling, Identity: &v1.BillingIdentity{Namespace: bill.Namespace, Account: bill.Account, NativeInstance: bill.NativeInstance, Component: bill.Component}}
	if e = store.SaveBillingEntry(ctx, entry); e != nil {
		return e
	}
	if owner == "" {
		if e = store.SaveBillingAlias(ctx, alias, source.SendRef.Name.LocalId); e != nil {
			return e
		}
	}
	source.Ref.Revision++
	source.Status = "SETTLED"
	source.Amount = &amount
	source.Identity = &v1.BillingIdentity{Namespace: bill.Namespace, Account: bill.Account, NativeInstance: bill.NativeInstance, Component: bill.Component}
	source.SourceVersion = bill.SourceVersion
	source.EntryRef = entry.Ref
	source.Alias = alias
	if e = s.saveBillingSource(ctx, source); e != nil {
		return e
	}
	r.Ref.Revision++
	r.Status = "SETTLED"
	return s.saveReservation(ctx, r)
}
func (s *Service) QueryBillingSource(ctx context.Context, c *v1.Caller, send *v1.Ref) (*v1.BillingSource, error) {
	if send == nil || send.Name == nil {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	if e := command.CheckName(c, send.Name, s.user, s.domain+"/ledger", "send"); e != nil {
		return nil, e
	}
	v, e := s.usageSource.(BillingExecution).QuerySend(ctx, c, send)
	if e != nil {
		return nil, e
	}
	if v == nil {
		return nil, command.Fail("NOT_FOUND")
	}
	return s.store.(billingStore).LoadBillingSource(ctx, send)
}
func (s *Service) QueryBillingEntry(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.BillingEntry, error) {
	if r == nil || r.Revision != 1 || r.SchemaId != "lerna.v1.BillingEntry" {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	if e := command.CheckName(c, r.Name, s.user, s.domain, "billing-entry"); e != nil {
		return nil, e
	}
	entry, e := s.store.(billingStore).LoadBillingEntry(ctx, r)
	if entry != nil && !proto.Equal(entry.Ref, r) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return entry, e
}

// recordConflict 接纳冲突证据但不接纳冲突金额；余额与原预留保持不变。
func (s *Service) recordConflict(ctx context.Context, source *v1.BillingSource, evidence *v1.Ref, reason, alias string) error {
	c := &v1.BillingConflict{Ref: command.NewRef(s.user, s.domain, "billing-conflict", "lerna.v1.BillingConflict"), SourceRef: source.Ref, EvidenceRef: evidence, Reason: reason, Alias: alias}
	if e := s.store.(billingStore).SaveBillingConflict(ctx, c); e != nil {
		return e
	}
	source.Ref = proto.Clone(source.Ref).(*v1.Ref)
	source.Ref.Revision++
	source.Status = "CONFLICT"
	source.ConflictRef = c.Ref
	if e := s.saveBillingSource(ctx, source); e != nil {
		return e
	}
	for _, id := range []*v1.GlobalName{nil, source.TaskId} {
		b, e := s.store.LoadBudget(ctx, id)
		if e != nil {
			return e
		}
		if b == nil {
			return command.Fail("NOT_FOUND")
		}
		b.Ref.Revision++
		b.BillingBlocked = true
		if e = s.saveBudget(ctx, b); e != nil {
			return e
		}
	}
	return nil
}
func (s *Service) QueryBillingConflict(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.BillingConflict, error) {
	if r == nil || r.Revision != 1 || r.SchemaId != "lerna.v1.BillingConflict" {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	if e := command.CheckName(c, r.Name, s.user, s.domain, "billing-conflict"); e != nil {
		return nil, e
	}
	v, e := s.store.(billingStore).LoadBillingConflict(ctx, r)
	if v != nil && !proto.Equal(v.Ref, r) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return v, e
}
func (s *Service) QueryBillingSourceVersion(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.BillingSource, error) {
	if r == nil || r.Revision == 0 || r.SchemaId != "lerna.v1.BillingSource" {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	if e := command.CheckName(c, r.Name, s.user, s.domain, "billing-source"); e != nil {
		return nil, e
	}
	v, e := s.store.(interface {
		LoadBillingSourceVersion(context.Context, *v1.Ref) (*v1.BillingSource, error)
	}).LoadBillingSourceVersion(ctx, r)
	if v != nil && !proto.Equal(v.Ref, r) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return v, e
}

// fileZeroBill 的零费用来自固定本机协议，不从效果或缺少账单推断。
func (s *Service) fileZeroBill(ctx context.Context, caller *v1.Caller, raw *v1.RawObservation) (*referenceBill, error) {
	facts, ok := s.usageSource.(interface {
		QueryOperation(context.Context, *v1.Caller, *v1.GlobalName) (*v1.Operation, error)
	})
	if !ok {
		return nil, command.Fail("DEPENDENCY_UNAVAILABLE")
	}
	op, e := facts.QueryOperation(ctx, caller, raw.OperationId)
	if e != nil {
		return nil, e
	}
	if op == nil || op.Execution == nil || op.CapabilitySnapshot == nil {
		return nil, command.Fail("INVALID_USAGE_SOURCE")
	}
	execution, ok := s.usageSource.(BillingExecution)
	if !ok {
		return nil, command.Fail("DEPENDENCY_UNAVAILABLE")
	}
	x, e := execution.QuerySendExecution(ctx, caller, raw.OperationId, raw.SendRef)
	if e != nil {
		return nil, e
	}
	if x == nil {
		return nil, command.Fail("INVALID_USAGE_SOURCE")
	}
	cap := op.CapabilitySnapshot
	if cap.AdapterRef.GetName().GetLocalId() != "managed-file" || cap.AdapterRef.GetRevision() != 1 || !cap.Nonbillable || cap.FeeCeiling == nil || cap.GetFeeCeiling() != 0 || x.Attempt.Capabilities.GetProtocolVersion() != "lerna-managed-file-v1" || raw.FileEvidence.GetBillingRule() != "managed-file-zero-v1" || !proto.Equal(x.Send.Ref.Name, raw.SendRef.Name) || raw.ExternalKey != x.Attempt.ExternalKey {
		return nil, nil
	}
	zero := int64(0)
	return &referenceBill{Rule: "managed-file-zero-v1", Namespace: "lerna-managed-file", Account: raw.UserId, NativeInstance: raw.SendRef.Name.LocalId, Component: "local-io", SendID: raw.SendRef.Name.LocalId, ExternalKey: raw.ExternalKey, SourceVersion: 1, Unit: "USD_MICRO", Amount: &zero, Final: true, PriceVersion: "managed-file-price-v1"}, nil
}
