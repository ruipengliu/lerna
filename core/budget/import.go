package budget

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type BillingExecution interface {
	QuerySendExecution(context.Context, *v1.Caller, *v1.GlobalName, *v1.Ref) (*v1.Execution, error)
	QueryExecution(context.Context, *v1.Caller, *v1.GlobalName) (*v1.Execution, error)
	QuerySend(context.Context, *v1.Caller, *v1.Ref) (*v1.PhysicalSend, error)
}

// ImportBill 接纳受信宿主导入的原始账单；任务当前控制状态不删除原来源责任。
func (s *Service) ImportBill(ctx context.Context, caller *v1.Caller, c *v1.ImportBillCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	return s.decisions.Execute(ctx, caller, c.Header, command.SemanticFingerprint("import-bill", c.SendRef, c.EvidenceRef, c.Operation), "budget.import", func(tx context.Context) (*v1.Ref, error) {
		if !s.trustedCaller(caller) {
			return nil, command.Fail("PERMISSION_DENIED")
		}
		if c.Operation != "" && c.Operation != "SETTLE" {
			return nil, command.Fail("UNSUPPORTED_BILLING_OPERATION")
		}
		source, e := s.QueryBillingSource(tx, caller, c.SendRef)
		if e != nil {
			return nil, e
		}
		if source == nil {
			return nil, command.Fail("NOT_FOUND")
		}
		execution := s.usageSource.(BillingExecution)
		send, e := execution.QuerySend(tx, caller, c.SendRef)
		if e != nil {
			return nil, e
		}
		if send == nil {
			return nil, command.Fail("NOT_FOUND")
		}
		x, e := execution.QuerySendExecution(tx, caller, source.OperationId, c.SendRef)
		if e != nil {
			return nil, e
		}
		if x == nil || !proto.Equal(x.Send.Ref.Name, c.SendRef.Name) || x.Send.Phase == "REGISTERED" || x.Send.Phase == "CLOSED" {
			return nil, command.Fail("BILLING_SEND_UNPROVEN")
		}
		body, e := s.evidence.Read(tx, caller, c.EvidenceRef)
		if e != nil {
			return nil, e
		}
		if body == nil || body.Status != "AVAILABLE" || !s.trustedCaller(&v1.Caller{IssuerId: body.GetSource().GetIssuerId()}) || body.GetSource().GetUserId() != s.user {
			return nil, command.Fail("UNTRUSTED_BILLING_EVIDENCE")
		}
		account, e := billingAccount(x.CallDescriptor, s.user)
		if e != nil {
			return nil, e
		}
		bill, e := parseBillForAccount(command.ContentBytes(body), account)
		if e != nil {
			return nil, e
		}
		if bill == nil {
			return nil, command.Fail("INVALID_BILL")
		}
		if bill.SendID != source.SendRef.Name.LocalId || bill.ExternalKey != x.Attempt.ExternalKey {
			return nil, command.Fail("BILLING_BINDING_MISMATCH")
		}
		if e = s.applyBill(tx, source, bill, c.EvidenceRef); e != nil {
			return nil, e
		}
		current, e := s.store.(billingStore).LoadBillingSource(tx, c.SendRef)
		if e != nil {
			return nil, e
		}
		if current.ConflictRef != nil {
			return current.ConflictRef, nil
		}
		return current.EntryRef, nil
	})
}
