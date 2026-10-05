// Package egressio 是实际 socket 的受信边界；每个调用最多写入一个 HTTP 请求。
package egressio

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

type HTTP struct{}

func (HTTP) Perform(ctx context.Context, c *v1.PhysicalIORequest) (*v1.PhysicalIOResult, error) {
	d := c.GetCallDescriptor()
	if d == nil || d.Protocol != "HTTP" || d.Method != "POST" || c.Send == nil || c.Send.Phase != "DISPATCH_POSSIBLE" || c.Attempt == nil {
		return nil, command.Fail("INVALID_IO_INTENT")
	}
	target, e := url.Parse(d.Target)
	if e != nil || target.Scheme != "http" || target.User != nil || net.ParseIP(target.Hostname()) == nil || !net.ParseIP(target.Hostname()).IsLoopback() {
		return nil, command.Fail("TARGET_SCOPE_MISMATCH")
	}
	observation := &v1.RawObservation{Ref: c.Send.ObservationRef, UserId: c.OperationId.UserId, TaskId: c.TaskId, OperationId: c.OperationId, AttemptId: c.Attempt.Ref.Name, SendRef: c.Send.Ref, SendSeq: c.Send.SendSeq, Target: d.Target, ExecutorEndpointId: c.ExecutorEndpointId, Protocol: "HTTP", StartedAtUnixMs: time.Now().UnixMilli(), ExternalKey: c.Attempt.ExternalKey, Source: "TRUSTED_IO"}
	result := &v1.PhysicalIOResult{Observation: observation}
	finish := func(err error) (*v1.PhysicalIOResult, error) {
		observation.FinishedAtUnixMs = time.Now().UnixMilli()
		if err != nil {
			observation.TransportError = err.Error()
		}
		return result, nil
	}
	port := target.Port()
	if port == "" {
		port = "80"
	}
	conn, e := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(target.Hostname(), port))
	if e != nil {
		return finish(e)
	}
	defer conn.Close()
	observation.ActualAddress = conn.RemoteAddr().String()
	deadline := time.Now().Add(5 * time.Second)
	if until, ok := ctx.Deadline(); ok && until.Before(deadline) {
		deadline = until
	}
	if e = conn.SetDeadline(deadline); e != nil {
		return finish(e)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	request, e := http.NewRequestWithContext(ctx, "POST", d.Target, bytes.NewReader(c.Body))
	if e != nil {
		return finish(e)
	}
	request.Close = true
	request.Header.Set("Idempotency-Key", d.ExternalKey)
	request.Header.Set("Lerna-Attempt", c.Attempt.Ref.Name.LocalId)
	request.Header.Set("Lerna-Send-Id", c.Send.Ref.Name.LocalId)
	request.Header.Set("Lerna-Send", strconv.FormatUint(uint64(c.Send.SendSeq), 10))
	request.Header.Set("Lerna-User", c.OperationId.UserId)
	request.Header.Set("Lerna-Operation", c.OperationId.LocalId)
	// 不使用 Client/Transport；不提供重定向、连接池、重放或回退路径。
	if e = request.Write(conn); e != nil {
		return finish(e)
	}
	response, e := http.ReadResponse(bufio.NewReader(conn), request)
	if e != nil {
		return finish(e)
	}
	defer response.Body.Close()
	observation.StatusCode = int32(response.StatusCode)
	observation.ProviderRequestId = response.Header.Get("X-Request-ID")
	result.Body, e = io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if len(result.Body) > 1<<20 {
		e = errors.New("response exceeds maximum size")
	}
	return finish(e)
}
