package memorycheck

import (
	"context"
	"testing"

	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"lerna/sdk"
)

func recoveredSDKFixture(t *testing.T) (*fixture, memory.QueryStore, *sdk.MemoryClient) {
	t.Helper()
	r, err := openRecoveryFixture(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.close)
	return r.source, r.view, r.client
}

func TestRecoveredSDKQueryUsesCurrentAuthorization(t *testing.T) {
	ctx := context.Background()
	f, view, client := recoveredSDKFixture(t)
	query := &wire.MemoryRequest{Method: "QUERY", Query: f.query(), GrantMaterial: f.config.Grant}
	out, err := client.Exchange(ctx, query)
	if err != nil || len(out.GetResult().GetRecords()) != 1 || out.Result.Records[0].Ref.Key != "format" || string(out.Result.Records[0].Spec.Content.Json) != `{"text":"concise"}` {
		t.Fatalf("recovered SDK query: %v", err)
	}
	if _, err = view.Commit(ctx, memory.Change{}); err != memory.Quarantined {
		t.Fatalf("recovered view acquired mutation rights: %v", err)
	}
	// Current policy comes from the active authorization service, not the backup.
	policy, err := f.auth.GetPolicy(ctx, f.config.Token)
	if err != nil {
		t.Fatal(err)
	}
	op, err := f.auth.NewOperation(ctx, f.config.Token)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.auth.Execute(ctx, f.config.Token, authorization.Mutation{Namespace: "local", OperationID: op, Command: &wire.AuthorizationCommand{ExpectedRevision: policy.Revision, Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if out, err = client.Exchange(ctx, query); err == nil || out != nil {
		t.Fatalf("backup reused revoked query grant: %v", err)
	}
}

// Inject only at the public permit revalidation seam immediately before the
// Reader's final storage liveness check, after actual body reconstruction.
type recoveryReleasePermits struct {
	memory.ReadPermits
	before func(context.Context) error
	calls  int
}

func (p *recoveryReleasePermits) Validate(ctx context.Context, b memory.Binding, i memory.ReadIntent, material, permit string) error {
	if err := p.ReadPermits.Validate(ctx, b, i, material, permit); err != nil {
		return err
	}
	p.calls++
	if p.calls == 3 {
		return p.before(ctx)
	}
	return nil
}
func TestRecoveredSDKBlocksDeletionAtFinalRelease(t *testing.T) {
	ctx := context.Background()
	f, view, _ := recoveredSDKFixture(t)
	deletionID, err := f.auth.NewOperation(ctx, f.config.Token)
	if err != nil {
		t.Fatal(err)
	}
	hook := &recoveryReleasePermits{ReadPermits: f.permits, before: func(ctx context.Context) error {
		_, err := f.client.Exchange(ctx, &wire.MemoryRequest{Method: "DELETE", Delete: &wire.MemoryDelete{OperationId: deletionID, Ref: f.write(f.config.WriteID, 0, "concise").Write.Ref, ExpectedRevision: 1, Purpose: "assist"}})
		return err
	}}
	client, err := f.bind(view, hook)
	if err != nil {
		t.Fatal(err)
	}
	out, err := client.Exchange(ctx, &wire.MemoryRequest{Method: "QUERY", Query: f.query(), GrantMaterial: f.config.Grant})
	if hook.calls != 3 || err == nil || out != nil {
		t.Fatalf("final SDK release ignored deletion: validations=%d err=%v", hook.calls, err)
	}
	receipt, err := f.store.LookupOperation(ctx, "local", deletionID)
	if err != nil || receipt.Revision != 2 || receipt.Position != 4 {
		t.Fatalf("deletion injection did not commit: %+v %v", receipt, err)
	}
}

func TestRecoveredSDKEnforcesCurrentRecipientResidency(t *testing.T) {
	ctx := context.Background()
	f, view, _ := recoveredSDKFixture(t)
	for _, recipient := range []string{"cloud", "device-a"} {
		f.binding.Recipient = recipient
		id, err := f.auth.NewOperation(ctx, f.config.Token)
		if err != nil {
			t.Fatal(err)
		}
		get := &wire.MemoryGet{ReadId: id, Ref: f.write(f.config.WriteID, 0, "concise").Write.Ref, Revision: 1, Purpose: "assist"}
		intent, err := memory.DescribeGet(f.binding, get)
		if err != nil {
			t.Fatal(err)
		}
		material, err := f.sign(ctx, intent)
		if err != nil {
			t.Fatal(err)
		}
		client, err := f.bind(view, f.permits)
		if err != nil {
			t.Fatal(err)
		}
		out, err := client.Exchange(ctx, &wire.MemoryRequest{Method: "GET", Get: get, GrantMaterial: material})
		if recipient == "cloud" {
			if err != memory.Denied || out != nil {
				t.Fatalf("live proof and valid read grant bypassed recipient residency: %v", err)
			}
		} else if err != nil || len(out.GetResult().GetRecords()) != 1 {
			t.Fatalf("permitted recipient cannot read retained data: %v", err)
		}
	}
}
