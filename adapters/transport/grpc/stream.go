package grpc

import (
	"context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"lerna/execution"
	wire "lerna/gen/harness/v1"
	"lerna/internal/executionwire"
	"lerna/internal/taskwire"
	"time"
)

func (s *Server) Exchange(stream grpc.BidiStreamingServer[wire.ExchangeFrame, wire.ExchangeFrame]) error {
	select {
	case s.streams <- struct{}{}:
		defer func() { <-s.streams }()
	default:
		return status.Error(codes.ResourceExhausted, "stream capacity")
	}
	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()
	incoming := make(chan *wire.ExchangeFrame, 1)
	failed := make(chan error, 1)
	go func() {
		for {
			f, e := stream.Recv()
			if e != nil {
				select {
				case failed <- e:
				case <-ctx.Done():
				}
				return
			}
			select {
			case incoming <- f:
			case <-ctx.Done():
				return
			}
		}
	}()
	timeout := time.NewTimer(s.config.IOTimeout)
	defer timeout.Stop()
	var op string
	select {
	case f := <-incoming:
		if !taskwire.Known(f.ProtoReflect()) || len(f.GetSubscribe()) == 0 || len(f.GetSubscribe()) > 512 {
			return status.Error(codes.InvalidArgument, "subscribe required")
		}
		op = f.GetSubscribe()
	case e := <-failed:
		return e
	case <-timeout.C:
		return status.Error(codes.DeadlineExceeded, "subscribe timeout")
	case <-ctx.Done():
		return ctx.Err()
	}
	timeout.Stop()
	tick := time.NewTicker(s.config.PollInterval)
	defer tick.Stop()
	var waiting, last uint64
	deadline := time.Time{}
	for {
		v, svc, e := s.current(ctx, "invocation.snapshots.v1")
		if e != nil {
			return e
		}
		read, cancelRead := context.WithTimeout(ctx, s.config.IOTimeout)
		record, e := svc.ReadRemoteInvocation(read, op)
		cancelRead()
		if e != nil {
			return rpcError(e)
		}
		key := peerKey(v.p)
		if waiting == 0 {
			bounded, stop := context.WithTimeout(ctx, s.config.IOTimeout)
			pending, e := s.journal.Pending(bounded, key, op)
			stop()
			if e != nil {
				return rpcError(e)
			}
			var update *wire.InvocationUpdate
			if len(pending) > 0 {
				update = pending[0]
			} else if record.Revision > last {
				update = &wire.InvocationUpdate{OperationId: op, Seq: record.Revision, Snapshot: executionwire.Snapshot(record), FullSnapshot: true, Reliable: reliable(record)}
				if update.Reliable {
					bounded, stop = context.WithTimeout(ctx, s.config.IOTimeout)
					_, e = s.journal.Save(bounded, key, update)
					// The first durable publication defines this revision, including its
					// then-current Core application progress. Reuse it on later subscriptions.
					saved, loadErr := s.journal.recorded(bounded, key, op, update.Seq)
					if loadErr == nil && saved != nil {
						update = saved
						e = nil
					} else if loadErr != nil {
						e = loadErr
					}
					stop()
					if e != nil {
						return rpcError(e)
					}
				}
			}
			if update != nil {
				if _, _, e = s.current(ctx, "invocation.snapshots.v1"); e != nil {
					return e
				}
				check, stop := context.WithTimeout(ctx, s.config.IOTimeout)
				_, e = svc.ReadRemoteInvocation(check, op)
				stop()
				if e != nil {
					return rpcError(e)
				}
				frame := &wire.ExchangeFrame{Body: &wire.ExchangeFrame_Update{Update: update}}
				if proto.Size(frame) > int(v.selected.MaxMessageBytes) {
					return status.Error(codes.ResourceExhausted, "snapshot limit")
				}
				// One outstanding write. Returning the handler cancels a blocked Send.
				sent := make(chan error, 1)
				go func() { sent <- stream.Send(frame) }()
				timer := time.NewTimer(s.config.IOTimeout)
				select {
				case e = <-sent:
					timer.Stop()
					if e != nil {
						return e
					}
				case <-timer.C:
					return status.Error(codes.DeadlineExceeded, "slow consumer")
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				}
				last = update.Seq
				if update.Reliable {
					waiting = update.Seq
					deadline = time.Now().Add(s.config.IOTimeout)
				}
			}
		}
		select {
		case f := <-incoming:
			if !taskwire.Known(f.ProtoReflect()) || f.GetPersisted() == 0 || f.GetPersisted() != waiting {
				return status.Error(codes.InvalidArgument, "unexpected acknowledgement")
			}
			bounded, stop := context.WithTimeout(ctx, s.config.IOTimeout)
			e = s.journal.Ack(bounded, key, op, waiting)
			stop()
			if e != nil {
				return rpcError(e)
			}
			waiting = 0
		case e := <-failed:
			return e
		case <-tick.C:
			if waiting != 0 && time.Now().After(deadline) {
				return status.Error(codes.DeadlineExceeded, "receipt timeout")
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
func reliable(r execution.Record) bool {
	return r.Phase == "FINISHED" || r.Phase == "UNKNOWN" || r.ConflictingEvidence
}
