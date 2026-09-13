package executioncheck

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"lerna/authorization"
	"lerna/tasks"
	"reflect"
)

//go:embed testdata/09-runtime.gob
var legacyRuntime []byte

//go:embed testdata/09-ref.json
var legacyRef []byte

func upgrade(ctx context.Context) error {
	h, e := fresh(ctx)
	if e != nil {
		return e
	}
	defer h.destroy()
	if e = h.auth.UpdateRuntime(ctx, func(tx authorization.RuntimeTransaction) error { tx.SetData(legacyRuntime); return nil }); e != nil {
		return e
	}
	h.core, e = tasks.New(h.auth, tasks.Config{Namespace: "local", Resource: "root", Owner: "local-owner", MaxTasks: 100, MaxPage: 10})
	if e != nil {
		return e
	}
	h.work, e = h.core.BindWorker(tasks.WorkerBinding{Token: h.token, Subject: "operator", WorkerID: "api-worker", AllowEffectEvidence: true}, limits())
	if e != nil {
		return e
	}
	if e = h.replace(h.target, h.work); e != nil {
		return e
	}
	var ref tasks.Ref
	if e = json.Unmarshal(legacyRef, &ref); e != nil {
		return e
	}
	old, e := h.core.Load(ctx, ref)
	if e != nil {
		return e
	}
	if len(old.Task.InputFacts) != 1 || len(old.Task.InputRefs) != 2 || len(old.ExecutionReports) != 0 || old.Work[0].ExecutionOperation != "" {
		return fmt.Errorf("legacy input facts changed")
	}
	r, m, e := h.request(ctx)
	if e != nil {
		return e
	}
	if _, e = h.client.Invoke(ctx, r, m); e != nil {
		return e
	}
	if _, e = h.exec.Run(ctx, r.OperationID); e != nil {
		return e
	}
	if e = h.exec.Drain(ctx, 16); e != nil {
		return e
	}
	after, e := h.core.Load(ctx, ref)
	if e != nil {
		return e
	}
	return require(reflect.DeepEqual(old, after), "execution overwrote runtime partition")
}
