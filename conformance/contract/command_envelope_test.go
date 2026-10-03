package contract_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// 命令与回执各自合法，原身份的持久封套同时保存两份，不能套用单份领域字节上限。
func TestLargeOriginalAndReceiptRemainDurableAfterReopen(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			f := fixture(t, backend, nil)
			ctx := context.Background()
			registry := runtime.NewRegistry()
			schema := api.Object(map[string]any{"value": api.Schema{"type": "string", "maxLength": 200000}}, "value")
			registry.MustRegister(runtime.Method{Contract: api.MethodContract{Name: "test.echo", Owner: "test", Kind: "command", InputSchema: schema, OutputSchema: schema}, Participants: []string{"test"}, Apply: func(_ context.Context, _ runtime.Tx, _ runtime.Auth, c api.Command) (runtime.Outcome, error) {
				var in testRecord
				if e := api.Decode(c.Payload, &in); e != nil {
					return runtime.Outcome{}, e
				}
				return runtime.Applied(in), nil
			}})
			d := runtime.Dispatcher{Store: f.store, OwnerID: f.scope.OwnerID, Registry: registry}
			auth := runtime.Auth{TenantID: f.scope.TenantID, SubjectID: api.NewID("subject"), CredentialGeneration: 1}
			c := command(f.scope, api.NewID("command"), api.NewID("object"))
			c.Method = "test.echo"
			c.Payload = api.Raw(testRecord{Value: strings.Repeat("e", 130<<10)})
			out, e := d.Command(ctx, auth, api.Raw(c))
			if e != nil || out.Stage != "applied" {
				t.Fatalf("large legal command lost its decision: %+v %v", out, e)
			}
			reopened := f.open(nil)
			d.Store = reopened
			receipt, e := d.Lookup(ctx, auth, c.CommandID)
			if e != nil || !api.Equal(api.Raw(receipt), api.Raw(out)) {
				t.Fatalf("original receipt changed after reopen: %v", e)
			}
			original, e := reopened.LookupCommand(ctx, f.scope, c.CommandID)
			if e != nil || !api.Equal(api.Raw(original.Command), api.Raw(c)) {
				t.Fatalf("large original changed after reopen: %v", e)
			}
			// 普通领域记录仍保留单份 256KiB 上限。
			status, e := reopened.Within(ctx, f.scope, []string{"test"}, func(tx runtime.Tx) error {
				return tx.Create(ctx, "test.records", api.NewID("object"), "", testRecord{Value: strings.Repeat("x", 256<<10)})
			})
			if status != runtime.RolledBack || !api.IsCode(e, "invalid_request") {
				t.Fatalf("ordinary record byte limit expanded: %s %v", status, e)
			}
			c.CommandID = api.NewID("command")
			c.Payload = api.Raw(testRecord{Value: strings.Repeat("x", 256<<10)})
			c.ExpiresAt = api.Time(time.Now().Add(time.Minute))
			if _, e = d.Command(ctx, auth, api.Raw(c)); !api.IsCode(e, "invalid_request") {
				t.Fatalf("individual command byte limit expanded: %v", e)
			}
		})
	}
}
