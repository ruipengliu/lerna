package harness

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/sdk/go/grpcwire"
)

type wsEndpoint struct {
	cfg                       EndpointConfig
	methods                   map[string]endpointMethod
	errorValidator            *api.Validator
	transport                 *WSTransport
	normal, control           chan grpcwire.Delivery
	mu                        sync.Mutex
	pending                   map[string]grpcwire.Delivery
	bytes, normalBytes        int
	normalItems, controlItems int
	recovered                 []ReplyEntry
	workers                   sync.WaitGroup
	ctx                       context.Context
}

func (e *wsEndpoint) start(ctx context.Context, t *WSTransport) error {
	e.ctx, e.transport = ctx, t
	e.workers.Add(3)
	for range 2 {
		go e.worker(e.normal)
	}
	go e.worker(e.control)
	for _, entry := range e.recovered {
		if err := e.accept(entry.Delivery); err != nil {
			return err
		}
	}
	e.recovered = nil
	return nil
}
func (e *wsEndpoint) accept(d grpcwire.Delivery) error {
	if _, err := e.invocation(d); err != nil {
		return err
	}
	control := d.Kind == "receipt_lookup"
	if d.Kind == "command" {
		var command api.Command
		if err := api.Decode(d.Request, &command); err != nil {
			return err
		}
		control = api.IsControlMethod(command.Method)
	}
	n := len(api.Raw(d))
	e.mu.Lock()
	defer e.mu.Unlock()
	if old, exists := e.pending[d.DeliveryID]; exists {
		if !api.Equal(old, d) {
			return api.E("idempotency_conflict", "endpoint_queued_delivery_changed")
		}
		return nil
	}
	if e.ctx.Err() != nil || len(e.pending) >= 32 || e.bytes+n > 4<<20 || !control && (e.normalItems >= 28 || e.normalBytes+n > 3<<20) || control && e.controlItems >= 4 {
		return api.E("overloaded", "endpoint_incoming_queue_limit")
	}
	queue := e.normal
	if control {
		queue = e.control
	}
	select {
	case queue <- d:
		e.pending[d.DeliveryID] = d
		e.bytes += n
		if !control {
			e.normalBytes += n
			e.normalItems++
		} else {
			e.controlItems++
		}
		return nil
	default:
		return api.E("overloaded", "endpoint_incoming_queue_items")
	}
}
func (e *wsEndpoint) worker(queue <-chan grpcwire.Delivery) {
	defer e.workers.Done()
	for {
		select {
		case <-e.ctx.Done():
			return
		case d := <-queue:
			ctx, cancel := context.WithTimeout(e.ctx, 5*time.Second)
			err := e.process(ctx, d)
			cancel()
			e.mu.Lock()
			delete(e.pending, d.DeliveryID)
			n := len(api.Raw(d))
			e.bytes -= n
			if queue == e.normal {
				e.normalBytes -= n
				e.normalItems--
			} else {
				e.controlItems--
			}
			e.mu.Unlock()
			if err != nil {
				e.transport.fail(err)
				e.transport.conn.CloseNow()
				return
			}
		}
	}
}
func (e *wsEndpoint) process(ctx context.Context, d grpcwire.Delivery) error {
	if err := e.cfg.Current(ctx); err != nil {
		return err
	}
	entry, err := e.cfg.Journal.Invocation(ctx, d.DeliveryID)
	if errors.Is(err, os.ErrNotExist) {
		if err = e.verifyDelivery(ctx, d); err != nil {
			return err
		}
		meta, err := e.invocation(d)
		if err != nil {
			return err
		}
		entry, err = e.cfg.Journal.PrepareInvocation(ctx, d, meta)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if !api.Equal(entry.Delivery, d) {
		return api.E("idempotency_conflict", "endpoint_original_delivery_changed")
	}
	if err = e.checkEntry(entry); err != nil {
		return err
	}
	if entry.ReplyDigest != "" {
		return e.sendReply(ctx, entry.Reply)
	}
	var reply grpcwire.Reply
	if entry.Invocation.Phase == "prepared" {
		if err = e.verifyDelivery(ctx, d); err != nil {
			return err
		}
		entry, err = e.cfg.Journal.StartInvocation(ctx, d.DeliveryID)
		if err != nil {
			return err
		}
		if err = e.cfg.Current(ctx); err != nil {
			return err
		}
		deadline, err := api.ParseTime(d.DeliverBefore)
		if err != nil || !time.Now().Before(deadline) {
			return api.E("expired", "endpoint_delivery_window_expired")
		}
		if d.Kind == "command" {
			var command api.Command
			if err := api.Decode(d.Request, &command); err != nil {
				return err
			}
			commandDeadline, err := api.ParseTime(command.ExpiresAt)
			if err != nil || !time.Now().Before(commandDeadline) {
				return api.E("expired", "endpoint_original_command_expired")
			}
		}
		reply, err = e.cfg.Receiver.Invoke(ctx, d)
		if err != nil {
			// 实际回调结果不明；原started责任已fsync，后续只Lookup。
			return nil
		}
	} else {
		var found bool
		reply, found, err = e.cfg.Receiver.Lookup(ctx, d)
		if err != nil || !found {
			return nil
		}
	}
	if err = e.validateReply(ctx, d, reply); err != nil {
		return err
	}
	if err = e.cfg.Journal.Save(ctx, d, reply); err != nil {
		return err
	}
	return e.sendReply(ctx, reply)
}
func (e *wsEndpoint) sendReply(ctx context.Context, reply grpcwire.Reply) error {
	if err := e.cfg.Current(ctx); err != nil {
		return err
	}
	e.transport.write.Lock()
	defer e.transport.write.Unlock()
	return e.transport.conn.Write(ctx, websocket.MessageText, api.Raw(reply))
}
func (e *wsEndpoint) ack(ctx context.Context, ack grpcwire.ReplyAck) error {
	if err := e.cfg.Current(ctx); err != nil {
		return err
	}
	return e.cfg.Journal.Acknowledge(ctx, ack)
}
func (e *wsEndpoint) join() error {
	done := make(chan struct{})
	go func() { e.workers.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-time.After(5 * time.Second):
		return api.E("effect_unknown", "endpoint_handlers_not_exited")
	}
}

// RecoverEndpoint只恢复此固定接收owner的原责任；started不再Invoke。
func (t *WSTransport) RecoverEndpoint(ctx context.Context) (bool, error) {
	if t.endpoint == nil {
		return false, api.E("unsupported", "endpoint_receiver_not_configured")
	}
	entries, partial, err := t.endpoint.cfg.Journal.Incomplete(ctx, 32)
	if err != nil {
		return partial, err
	}
	for _, entry := range entries {
		if err = t.endpoint.checkEntry(entry); err != nil {
			return partial, err
		}
		if err = t.endpoint.accept(entry.Delivery); err != nil {
			return partial, err
		}
	}
	return partial, nil
}
