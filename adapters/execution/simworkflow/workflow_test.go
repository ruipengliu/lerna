package simworkflow_test

import (
	"context"
	"lerna/adapters/execution/simworkflow"
	"lerna/execution"
	"path/filepath"
	"testing"
)

func TestDurableBusinessTransition(t *testing.T) {
	defs, e := simworkflow.Definitions()
	if e != nil {
		t.Fatal(e)
	}
	if len(defs) != 168 {
		t.Fatal(len(defs))
	}
	d, e := simworkflow.Open(filepath.Join(t.TempDir(), "target.db"), defs[0])
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	ctx := context.Background()
	c := execution.Call{Control: &execution.ControlQualification{Resource: d.Resource(), Authority: simworkflow.Authority, Version: 1}, Request: execution.Request{OperationID: "draft", Capability: defs[0].Kind + ".draft", ResourceVersion: 1}, Input: []byte(`{"record":"order","ordered_units":3,"approved_units":10,"vendor_verified":true}`)}
	if e = d.Start(ctx, c); e != nil {
		t.Fatal(e)
	}
	if e = d.Start(ctx, c); e != nil {
		t.Fatal(e)
	}
	s, e := d.Snapshot(ctx, "order")
	if e != nil || s.State != "draft" || s.Version != 2 || s.Ledger != 1000 {
		t.Fatalf("%+v %v", s, e)
	}
	c.Request.OperationID = "submit"
	c.Request.Capability = defs[0].Kind + ".submit"
	c.Request.ResourceVersion = 2
	c.Input = []byte(`{"record":"order"}`)
	if e = d.Start(ctx, c); e != nil {
		t.Fatal(e)
	}
	c.Request.OperationID = "authorize"
	c.Request.Capability = defs[0].Kind + ".authorize"
	c.Request.ResourceVersion = 3
	if e = d.Start(ctx, c); e != nil {
		t.Fatal(e)
	}
	s, e = d.Snapshot(ctx, "order")
	if e != nil || s.State != "authorized" || s.Ledger != 997 || s.Version != 4 {
		t.Fatalf("%+v %v", s, e)
	}
}
