package grpc

import (
	"context"
	"time"

	"github.com/ruipengliu/lerna/api"
	rt "github.com/ruipengliu/lerna/runtime"
	"github.com/ruipengliu/lerna/sdk/go/grpcwire"
)

type deliveryRecord struct {
	Revision       uint64               `json:"revision"`
	Registration   EndpointRegistration `json:"registration"`
	Delivery       grpcwire.Delivery    `json:"delivery"`
	DeliveryDigest string               `json:"delivery_digest"`
	Phase          string               `json:"phase"`
	Reply          *grpcwire.Reply      `json:"reply,omitempty"`
	ReplyDigest    string               `json:"reply_digest,omitempty"`
	StoredByOwner  bool                 `json:"stored_by_owner"`
}

// Deliver 只接受原 owner 已保存的 Delivery；断连不为调用方另造业务身份。
func (s *Server) Deliver(ctx context.Context, tenantID string, d grpcwire.Delivery) error {
	if !s.begin() {
		return api.E("dependency_unavailable", "grpc_service_closing")
	}
	defer s.owned.Done()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if s.cfg.EndpointAuthority == nil {
		return api.E("unsupported", "endpoint_authority_not_configured")
	}
	if _, e := grpcwire.DecodeFrame(api.Raw(d)); e != nil {
		return e
	}
	if d.SenderServiceID != s.cfg.OwnerID {
		return api.E("forbidden", "delivery_sender_mismatch")
	}
	s.mu.Lock()
	var c *channelSession
	for _, candidate := range s.channels {
		if candidate.reg.TenantID == tenantID && candidate.reg.EndpointID == d.RecipientEndpointID && candidate.reg.InstanceID == d.RecipientInstanceID {
			c = candidate
			break
		}
	}
	s.mu.Unlock()
	if c == nil {
		return api.E("dependency_unavailable", "endpoint_not_connected")
	}
	if e := c.current(ctx); e != nil {
		return e
	}
	if e := s.cfg.EndpointAuthority.Check(ctx, c.reg); e != nil {
		return e
	}
	if e := transportPayload(c.reg.RecipientServiceID, d.Kind, d.Request); e != nil {
		return e
	}
	if e := s.cfg.EndpointAuthority.VerifyDelivery(ctx, c.reg, d); e != nil {
		return e
	}
	deadline, e := api.ParseTime(d.DeliverBefore)
	if e != nil || !time.Now().Before(deadline) {
		return api.E("expired", "delivery_window_expired")
	}
	digest, e := api.Digest(d)
	if e != nil {
		return e
	}
	var record deliveryRecord
	state, e := s.cfg.Store.Within(ctx, c.scope(), []string{"grpc"}, func(tx rt.Tx) error {
		_, e := tx.Get(ctx, "grpc.deliveries", d.DeliveryID, &record)
		if e == nil {
			if record.DeliveryDigest != digest || !api.Equal(record.Registration, c.reg) {
				return api.E("idempotency_conflict", "delivery_changed")
			}
			return nil
		}
		if !api.IsCode(e, "not_found") {
			return e
		}
		record = deliveryRecord{Revision: 1, Registration: c.reg, Delivery: d, DeliveryDigest: digest, Phase: "pending"}
		return tx.Create(ctx, "grpc.deliveries", d.DeliveryID, d.RecipientEndpointID, record)
	})
	if state == rt.CommitUnknown {
		return rt.ErrCommitUnknown
	}
	if e != nil {
		return e
	}
	if record.StoredByOwner {
		return nil
	}
	if record.Reply != nil {
		return api.E("effect_unknown", "original_reply_pending")
	}
	if e = c.queue.push(d, false); e != nil {
		c.stop(e)
	}
	return e
}
func (c *channelSession) prepareDeliverySend(ctx context.Context, d grpcwire.Delivery) (bool, error) {
	if e := c.s.cfg.EndpointAuthority.VerifyDelivery(ctx, c.reg, d); e != nil {
		return false, e
	}
	deadline, e := api.ParseTime(d.DeliverBefore)
	if e != nil || !time.Now().Before(deadline) {
		return false, api.E("expired", "delivery_window_expired")
	}
	send := false
	state, e := c.s.cfg.Store.Within(ctx, c.scope(), []string{"grpc"}, func(tx rt.Tx) error {
		var binding bindingRecord
		if _, e := tx.Get(ctx, "grpc.bindings", c.bind.Bind.ConnectionID, &binding); e != nil {
			return e
		}
		if e := c.checkRecord(binding); e != nil {
			return e
		}
		var record deliveryRecord
		rev, e := tx.Get(ctx, "grpc.deliveries", d.DeliveryID, &record)
		if e != nil {
			return e
		}
		if !api.Equal(record.Delivery, d) || !api.Equal(record.Registration, c.reg) {
			return api.E("idempotency_conflict", "delivery_changed")
		}
		if record.Reply != nil || record.StoredByOwner {
			return nil
		}
		record.Revision = rev + 1
		record.Phase = "possibly_sent"
		send = true
		return tx.Put(ctx, "grpc.deliveries", d.DeliveryID, rev, record)
	})
	if state == rt.CommitUnknown {
		return false, rt.ErrCommitUnknown
	}
	return send && e == nil, e
}
func (c *channelSession) recoverDeliveries() error {
	records, e := c.s.cfg.Store.List(c.ctx, c.scope(), "grpc.deliveries", c.reg.EndpointID, "", 100)
	if e != nil {
		return e
	}
	if len(records) == 100 {
		return api.E("overloaded", "delivery_set_requires_batch")
	}
	for _, row := range records {
		var record deliveryRecord
		if e = row.Decode(&record); e != nil {
			return e
		}
		if record.StoredByOwner || record.Reply != nil || !api.Equal(record.Registration, c.reg) {
			continue
		}
		deadline, e := api.ParseTime(record.Delivery.DeliverBefore)
		if e != nil {
			return e
		}
		if !time.Now().Before(deadline) {
			continue
		}
		if e = c.queue.push(record.Delivery, false); e != nil {
			return e
		}
	}
	return nil
}
func (c *channelSession) reply(reply grpcwire.Reply, size int) error {
	if e := c.reserveIncoming(true, size); e != nil {
		return e
	}
	c.workers.Add(1)
	c.s.owned.Add(1)
	go func() {
		defer c.workers.Done()
		defer c.s.owned.Done()
		defer c.releaseIncoming(true, size)
		ctx, cancel := context.WithTimeout(context.WithoutCancel(c.stream.Context()), 5*time.Second)
		defer cancel()
		if e := c.storeReply(ctx, reply); e != nil {
			c.stop(e)
		}
	}()
	return nil
}
func (c *channelSession) storeReply(ctx context.Context, reply grpcwire.Reply) error {
	digest, e := api.Digest(reply)
	if e != nil {
		return e
	}
	var original deliveryRecord
	if _, e = c.s.cfg.Store.Read(ctx, c.scope(), "grpc.deliveries", reply.DeliveryID, 0, &original); e != nil {
		return e
	}
	if !api.Equal(original.Registration, c.reg) {
		return api.E("forbidden", "reply_endpoint_mismatch")
	}
	if validator, ok := c.s.cfg.EndpointAuthority.(EndpointReplyValidator); ok {
		if e = validator.ValidateReply(ctx, c.reg, original.Delivery, reply); e != nil {
			return e
		}
	}
	var record deliveryRecord
	state, e := c.s.cfg.Store.Within(ctx, c.scope(), []string{"grpc"}, func(tx rt.Tx) error {
		var binding bindingRecord
		if _, e := tx.Get(ctx, "grpc.bindings", c.bind.Bind.ConnectionID, &binding); e != nil {
			return e
		}
		if e := c.checkRecord(binding); e != nil {
			return e
		}
		rev, e := tx.Get(ctx, "grpc.deliveries", reply.DeliveryID, &record)
		if e != nil {
			return e
		}
		if !api.Equal(record.Registration, c.reg) {
			return api.E("forbidden", "reply_endpoint_mismatch")
		}
		if record.DeliveryDigest != original.DeliveryDigest || !api.Equal(record.Delivery, original.Delivery) {
			return api.E("idempotency_conflict", "reply_original_delivery_changed")
		}
		if e = grpcwire.ValidateReply(record.Delivery, reply); e != nil {
			return e
		}
		if record.Reply != nil {
			if record.ReplyDigest != digest {
				return api.E("idempotency_conflict", "reply_changed")
			}
			return nil
		}
		record.Revision = rev + 1
		record.Reply = &reply
		record.ReplyDigest = digest
		record.Phase = "reply_pending"
		return tx.Put(ctx, "grpc.deliveries", reply.DeliveryID, rev, record)
	})
	if state == rt.CommitUnknown {
		return rt.ErrCommitUnknown
	}
	if e != nil {
		return e
	}
	if !record.StoredByOwner {
		stored, e := c.s.cfg.EndpointAuthority.ReceiveReply(ctx, c.reg, record.Delivery, reply)
		if e != nil {
			return e
		}
		if !stored {
			return api.E("effect_unknown", "reply_owner_not_confirmed")
		}
		state, e = c.s.cfg.Store.Within(ctx, c.scope(), []string{"grpc"}, func(tx rt.Tx) error {
			var current deliveryRecord
			rev, e := tx.Get(ctx, "grpc.deliveries", reply.DeliveryID, &current)
			if e != nil {
				return e
			}
			if current.ReplyDigest != digest {
				return api.E("idempotency_conflict", "reply_changed")
			}
			if current.StoredByOwner {
				return nil
			}
			current.Revision = rev + 1
			current.StoredByOwner = true
			current.Phase = "stored"
			return tx.Put(ctx, "grpc.deliveries", reply.DeliveryID, rev, current)
		})
		if state == rt.CommitUnknown {
			return rt.ErrCommitUnknown
		}
		if e != nil {
			return e
		}
	}
	return c.queue.push(grpcwire.ReplyAck{Type: "reply_ack", DeliveryID: reply.DeliveryID, RequestDigest: reply.RequestDigest, Stored: true}, true)
}
