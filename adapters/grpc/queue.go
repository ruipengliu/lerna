package grpc

import (
	"context"
	"sync"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/api/proto/rpcv1"
	"github.com/ruipengliu/lerna/sdk/go/grpcwire"
)

type queuedFrame struct {
	body    []byte
	control bool
	size    int
}
type frameQueue struct {
	c                                                        *channelSession
	mu                                                       sync.Mutex
	ordinary, control                                        []queuedFrame
	ordinaryCount, controlCount, ordinaryBytes, controlBytes int
	closed                                                   bool
	wake                                                     chan struct{}
}

func newFrameQueue(c *channelSession) *frameQueue {
	return &frameQueue{c: c, wake: make(chan struct{}, 1)}
}
func (q *frameQueue) push(body any, control bool) error {
	b := api.Raw(body)
	if _, e := grpcwire.DecodeFrame(b); e != nil {
		return e
	}
	size := len(b) + len(q.c.bind.BindingID) + 32
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return api.E("dependency_unavailable", "channel_closed")
	}
	if control {
		if q.controlCount >= 4 || q.controlBytes+size > 1<<20 {
			return api.E("overloaded", "channel_control_limit")
		}
	} else {
		if q.ordinaryCount >= 32 || q.ordinaryBytes+size > 3<<20 {
			return api.E("overloaded", "channel_queue_limit")
		}
	}
	if e := q.c.s.reserveBytes(q.c.auth.TenantID, size); e != nil {
		return e
	}
	frame := queuedFrame{b, control, size}
	if control {
		q.control = append(q.control, frame)
		q.controlCount++
		q.controlBytes += size
	} else {
		q.ordinary = append(q.ordinary, frame)
		q.ordinaryCount++
		q.ordinaryBytes += size
	}
	select {
	case q.wake <- struct{}{}:
	default:
	}
	return nil
}
func (q *frameQueue) pop(ctx context.Context) (queuedFrame, error) {
	for {
		q.mu.Lock()
		if len(q.control) > 0 {
			item := q.control[0]
			q.control = q.control[1:]
			q.mu.Unlock()
			return item, nil
		}
		if len(q.ordinary) > 0 {
			item := q.ordinary[0]
			q.ordinary = q.ordinary[1:]
			q.mu.Unlock()
			return item, nil
		}
		closed := q.closed
		q.mu.Unlock()
		if closed {
			return queuedFrame{}, api.E("dependency_unavailable", "channel_closed")
		}
		select {
		case <-q.wake:
		case <-ctx.Done():
			return queuedFrame{}, ctx.Err()
		}
	}
}
func (q *frameQueue) release(item queuedFrame) {
	q.mu.Lock()
	if item.control {
		q.controlCount--
		q.controlBytes -= item.size
	} else {
		q.ordinaryCount--
		q.ordinaryBytes -= item.size
	}
	q.mu.Unlock()
	q.c.s.releaseBytes(q.c.auth.TenantID, item.size)
}
func (q *frameQueue) close() {
	q.mu.Lock()
	q.closed = true
	remaining := append(append([]queuedFrame{}, q.ordinary...), q.control...)
	q.ordinary = nil
	q.control = nil
	q.mu.Unlock()
	for _, item := range remaining {
		q.release(item)
	}
	select {
	case q.wake <- struct{}{}:
	default:
	}
}
func (s *Server) reserveBytes(tenant string, size int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.queuedBytes+size > 64<<20 || s.tenantQueuedBytes[tenant]+size > 32<<20 {
		return api.E("overloaded", "grpc_pool_bytes_limit")
	}
	s.queuedBytes += size
	s.tenantQueuedBytes[tenant] += size
	return nil
}
func (s *Server) releaseBytes(tenant string, size int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queuedBytes -= size
	s.tenantQueuedBytes[tenant] -= size
	if s.tenantQueuedBytes[tenant] == 0 {
		delete(s.tenantQueuedBytes, tenant)
	}
}
func (c *channelSession) reserveIncoming(control bool, size int) error {
	c.s.mu.Lock()
	defer c.s.mu.Unlock()
	if control {
		if c.inControl >= 4 || c.inControlBytes+size > 1<<20 {
			return api.E("overloaded", "channel_control_limit")
		}
	} else {
		if c.inOrd >= 32 || c.inOrdBytes+size > 3<<20 {
			return api.E("overloaded", "channel_request_limit")
		}
	}
	if c.s.queuedBytes+size > 64<<20 || c.s.tenantQueuedBytes[c.auth.TenantID]+size > 32<<20 {
		return api.E("overloaded", "grpc_pool_bytes_limit")
	}
	if control {
		c.inControl++
		c.inControlBytes += size
	} else {
		c.inOrd++
		c.inOrdBytes += size
	}
	c.s.queuedBytes += size
	c.s.tenantQueuedBytes[c.auth.TenantID] += size
	return nil
}
func (c *channelSession) releaseIncoming(control bool, size int) {
	c.s.mu.Lock()
	if control {
		c.inControl--
		c.inControlBytes -= size
	} else {
		c.inOrd--
		c.inOrdBytes -= size
	}
	c.s.queuedBytes -= size
	c.s.tenantQueuedBytes[c.auth.TenantID] -= size
	if c.s.tenantQueuedBytes[c.auth.TenantID] == 0 {
		delete(c.s.tenantQueuedBytes, c.auth.TenantID)
	}
	c.s.mu.Unlock()
}
func (c *channelSession) writeLoop() {
	defer c.s.owned.Done()
	defer close(c.writeDone)
	for {
		item, e := c.queue.pop(c.ctx)
		if e != nil {
			return
		}
		ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
		e = c.gate.lock(ctx)
		if e == nil {
			e = c.current(ctx)
			if e == nil {
				e = c.s.cfg.Identity.CheckCurrent(ctx, c.auth)
			}
			if e == nil {
				e = c.s.cfg.EndpointAuthority.Check(ctx, c.reg)
			}
			if e == nil {
				if frame, err := grpcwire.DecodeFrame(item.body); err != nil {
					e = err
				} else if delivery, ok := frame.(*grpcwire.Delivery); ok {
					e = c.prepareDeliverySend(ctx, *delivery)
				}
			}
			if e == nil {
				c.sendStarted.Store(time.Now().UnixNano())
				e = c.stream.Send(&rpcv1.ChannelFrame{BindingId: c.bind.BindingID, BindingRevision: c.bind.BindingRevision, FrameJson: item.body})
				c.sendStarted.Store(0)
				c.lastSend.Store(time.Now().UnixNano())
			}
			c.gate.unlock()
		}
		cancel()
		c.queue.release(item)
		if e != nil {
			c.stop(e)
			return
		}
	}
}
