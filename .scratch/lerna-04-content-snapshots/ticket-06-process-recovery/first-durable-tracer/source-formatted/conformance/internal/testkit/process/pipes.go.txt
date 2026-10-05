// Package process owns finite direct test children and their three physical
// pipes. Business configurations, stages and storage ownership stay with each
// concrete conformance consumer.
package process

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"
)

const FrameLimit = 64 * 1024

type endpoint struct {
	*os.File
	once sync.Once
	err  error
}

func (p *endpoint) Close() error {
	p.once.Do(func() { p.err = p.File.Close() })
	return p.err
}

type stageError struct {
	stage string
	cause error
}

func (e *stageError) Error() string { return e.stage }
func (e *stageError) Unwrap() error { return e.cause }
func cause(stage string, err error) error {
	if err == nil {
		return nil
	}
	return &stageError{stage, err}
}

// physicalIO is the existing02 poller-pipe cancellation mechanism: cancellation
// closes the physical pipe and joins the close callback before returning.
func physicalIO(ctx context.Context, pipe io.Closer, operation func() error) error {
	if ctx == nil {
		return errors.New("process pipe context required")
	}
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("finite process pipe context required")
	}
	done := make(chan struct{})
	var closeErr error
	stop := context.AfterFunc(ctx, func() { closeErr = pipe.Close(); close(done) })
	err := operation()
	if !stop() {
		<-done
	}
	if ctx.Err() != nil {
		return errors.Join(ctx.Err(), cause("process cancellation pipe close unknown", closeErr))
	}
	return err
}

func WriteFrame(ctx context.Context, pipe io.WriteCloser, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return cause("process frame encoding failed", err)
	}
	if len(data) == 0 || len(data) > FrameLimit {
		return errors.New("process frame exceeds limit")
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(data)))
	return physicalIO(ctx, pipe, func() error {
		for _, part := range [][]byte{header[:], data} {
			for len(part) > 0 {
				n, err := pipe.Write(part)
				if err != nil {
					return err
				}
				if n == 0 {
					return io.ErrShortWrite
				}
				part = part[n:]
			}
		}
		return nil
	})
}

// ReadFrame is a private trusted test protocol, not the public strict codec.
func ReadFrame(ctx context.Context, pipe io.ReadCloser, value any) error {
	return physicalIO(ctx, pipe, func() error {
		var header [4]byte
		if _, err := io.ReadFull(pipe, header[:]); err != nil {
			return err
		}
		length := binary.BigEndian.Uint32(header[:])
		if length == 0 || length > FrameLimit {
			return errors.New("process frame exceeds limit")
		}
		data := make([]byte, int(length))
		if _, err := io.ReadFull(pipe, data); err != nil {
			return err
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(value); err != nil {
			return cause("process frame decoding failed", err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return errors.New("process frame has trailing JSON")
		}
		return nil
	})
}
