package grpcwire

import "github.com/ruipengliu/lerna/api"

// ValidateReply 核对原 delivery 与真实结果身份；领域输出Schema由原 owner 继续验证。
func ValidateReply(d Delivery, reply Reply) error {
	if reply.DeliveryID != d.DeliveryID || reply.RequestDigest != d.RequestDigest {
		return api.E("idempotency_conflict", "reply_delivery_mismatch")
	}
	if reply.ResultKind == "error" {
		var problem api.Error
		if e := api.Decode(reply.Payload, &problem); e != nil {
			return e
		}
		if problem.Code == "" || problem.Reason == "" || problem.Scope == "" || problem.Retry == "" {
			return api.E("invalid_request", "invalid_delivery_error")
		}
		return nil
	}
	if d.Kind == "query" {
		if reply.ResultKind != "query_result" {
			return api.E("invalid_request", "reply_result_kind_mismatch")
		}
		_, e := api.ParseJSON(reply.Payload)
		return e
	}
	if reply.ResultKind != "receipt" {
		return api.E("invalid_request", "reply_result_kind_mismatch")
	}
	var receipt api.Receipt
	if e := api.Decode(reply.Payload, &receipt); e != nil {
		return e
	}
	if !api.ValidID(receipt.CommandID) || len(receipt.RequestDigest) != 71 || receipt.Stage != "accepted" && receipt.Stage != "applied" && receipt.Stage != "rejected" {
		return api.E("invalid_request", "invalid_delivery_receipt")
	}
	if d.Kind == "command" {
		var command api.Command
		if e := api.Decode(d.Request, &command); e != nil {
			return e
		}
		if receipt.CommandID != command.CommandID || receipt.RequestDigest != d.RequestDigest {
			return api.E("idempotency_conflict", "reply_command_mismatch")
		}
	} else if d.Kind == "receipt_lookup" {
		var lookup api.ReceiptLookup
		if e := api.Decode(d.Request, &lookup); e != nil {
			return e
		}
		if receipt.CommandID != lookup.CommandID {
			return api.E("idempotency_conflict", "reply_command_mismatch")
		}
	} else {
		return api.E("invalid_request", "invalid_delivery_kind")
	}
	if receipt.Stage == "rejected" {
		if receipt.Error == nil || len(receipt.Output) > 0 {
			return api.E("invalid_request", "invalid_delivery_receipt")
		}
	} else if receipt.Error != nil || len(receipt.Output) == 0 {
		return api.E("invalid_request", "invalid_delivery_receipt")
	}
	if receipt.Stage == "accepted" {
		if _, e := api.ParseTime(receipt.AcceptedAt); e != nil {
			return api.E("invalid_request", "invalid_receipt_time")
		}
	} else if _, e := api.ParseTime(receipt.DecidedAt); e != nil {
		return api.E("invalid_request", "invalid_receipt_time")
	}
	return nil
}
