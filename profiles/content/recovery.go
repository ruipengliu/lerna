package content

import (
	"bytes"
	"context"
	"encoding/json"
	"google.golang.org/protobuf/encoding/protojson"
	"lerna/artifacts"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

var crashPoints = []string{"partial-stage", "file-ready", "publication-committed", "delete-committed", "body-removed"}

type plan struct {
	Token   string
	Request json.RawMessage
}

func RunProbe(ctx context.Context, root, point string) error {
	data, err := os.ReadFile(filepath.Join(root, "plan.json"))
	if err != nil {
		return err
	}
	var p plan
	if json.Unmarshal(data, &p) != nil {
		return artifacts.Error("INVALID_ARGUMENT")
	}
	in := new(wire.ContentRequest)
	if protojson.Unmarshal(p.Request, in) != nil {
		return artifacts.Error("INVALID_ARGUMENT")
	}
	h, err := open(root, p.Token)
	if err != nil {
		return err
	}
	defer h.close()
	switch point {
	case "partial-stage", "file-ready", "body-removed":
		h.bind(h.auth, &crashBlobs{Blobs: h.blob, root: root, point: point})
	case "publication-committed":
		h.bind(&crashAuthority{Authority: h.auth, at: 2}, h.blob)
	case "delete-committed":
		h.bind(&crashAuthority{Authority: h.auth, at: 1}, h.blob)
	default:
		return artifacts.Error("INVALID_ARGUMENT")
	}
	if point == "body-removed" {
		return h.service.Clean(ctx)
	}
	_, err = h.client.Call(ctx, in)
	return err
}

type crashBlobs struct {
	artifacts.Blobs
	root, point string
}

func (b *crashBlobs) Put(ctx context.Context, key string, data []byte) error {
	if b.point == "partial-stage" {
		// Fault adapter models termination during a bounded file write, with a real
		// durable partial staging file. It does not claim deterministic syscall kill.
		f, err := os.OpenFile(filepath.Join(b.root, "content", key+".tmp"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		if _, err = f.Write(data[:7]); err != nil {
			f.Close()
			return err
		}
		if err = f.Sync(); err != nil {
			f.Close()
			return err
		}
		f.Close()
		os.Exit(73)
	}
	if err := b.Blobs.Put(ctx, key, data); err != nil {
		return err
	}
	if b.point == "file-ready" {
		os.Exit(73)
	}
	return nil
}
func (b *crashBlobs) Remove(ctx context.Context, key string) error {
	if err := b.Blobs.Remove(ctx, key); err != nil {
		return err
	}
	if b.point == "body-removed" {
		os.Exit(73)
	}
	return nil
}

type crashAuthority struct {
	artifacts.Authority
	n, at int
}

func (a *crashAuthority) UpdateContent(ctx context.Context, fn func(authorization.ContentTransaction) error) error {
	err := a.Authority.UpdateContent(ctx, fn)
	a.n++
	if err == nil && a.n == a.at {
		os.Exit(73)
	}
	return err
}
func recovery(ctx context.Context, executable, point string) error {
	h, err := fresh(ctx)
	if err != nil {
		return err
	}
	defer h.destroy()
	in, err := h.request(ctx, bytes.Repeat([]byte("q"), 100))
	if err != nil {
		return err
	}
	var ref *wire.ContentRef
	if point == "delete-committed" || point == "body-removed" {
		out, err := h.client.Call(ctx, in)
		if err != nil {
			return err
		}
		ref = out.Record.Ref
		op, err := h.auth.NewOperation(ctx, h.token)
		if err != nil {
			return err
		}
		in = &wire.ContentRequest{Method: "DELETE", Ref: ref, OperationId: op, ExpectedRevision: 1, Purpose: "research"}
		if point == "body-removed" {
			if _, err = h.client.Call(ctx, in); err != nil {
				return err
			}
		}
	}
	request, _ := protojson.Marshal(in)
	data, _ := json.Marshal(plan{h.token, request})
	if err = os.WriteFile(filepath.Join(h.root, "plan.json"), data, 0600); err != nil {
		return err
	}
	childCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(childCtx, executable, "content-crash-probe", h.root, point)
	err = cmd.Run()
	var exit *exec.ExitError
	if e, ok := err.(*exec.ExitError); ok {
		exit = e
	}
	if exit == nil || exit.ExitCode() != 73 {
		return artifacts.Error("CHILD_DID_NOT_REACH_BOUNDARY")
	}
	other, err := open(h.root, h.token)
	if err != nil {
		return err
	}
	defer other.close()
	result, lookupErr := other.client.Call(ctx, &wire.ContentRequest{Method: "LOOKUP", OperationId: in.OperationId, Purpose: "research"})
	switch point {
	case "partial-stage", "file-ready":
		if lookupErr == nil {
			return artifacts.Error("FALSE_PUBLICATION")
		}
		if err = other.service.Clean(ctx); err != nil {
			return err
		}
		unlock, err := other.blob.Lock(ctx)
		if err != nil {
			return err
		}
		files, err := other.blob.List(ctx, 64)
		unlock()
		if err != nil || len(files) != 0 {
			return artifacts.Error("ORPHAN_REMAINS")
		}
		out, err := other.client.Call(ctx, in)
		if err != nil {
			return err
		}
		_, err = other.read(ctx, out.Record.Ref, 5)
		return err
	case "publication-committed":
		if lookupErr != nil {
			return lookupErr
		}
		out, err := other.client.Call(ctx, in)
		if err != nil {
			return err
		}
		if out.Record.Ref.Key != result.Record.Ref.Key {
			return artifacts.Error("DUPLICATE_PUBLICATION")
		}
		r, err := other.read(ctx, out.Record.Ref, 5)
		if err != nil {
			return err
		}
		return demand(string(r.Data) == "qqqqq")
	default:
		if lookupErr != nil {
			return lookupErr
		}
		if result.Record.State != "cleaning" {
			return artifacts.Error("FALSE_CLEANUP")
		}
		if _, err = other.read(ctx, ref, 5); artifacts.Code(err) != "CONTENT_UNAVAILABLE" {
			return artifacts.Error("DELETED_CONTENT_READABLE")
		}
		if err = other.service.Clean(ctx); err != nil {
			return err
		}
		result, err = other.client.Call(ctx, in)
		if err != nil {
			return err
		}
		return demand(result.Record.State == "cleaned")
	}
}
