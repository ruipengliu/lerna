package memorycheck

import (
	"context"
	"fmt"
	"google.golang.org/protobuf/proto"
	memorylocal "lerna/adapters/memory/local"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"lerna/sdk"
	"os"
	"time"
)

func (f *fixture) sign(ctx context.Context, intent memory.ReadIntent) (string, error) {
	op, e := f.auth.NewOperation(ctx, f.config.Token)
	if e != nil {
		return "", e
	}
	view, e := f.auth.GetPolicy(ctx, f.config.Token)
	if e != nil {
		return "", e
	}
	p := peer()
	grant, e := f.grants.Mutate(ctx, f.config.Token, &wire.GrantMutation{OperationId: op, Kind: "ISSUE", ExpectedRevision: view.Revision, Spec: &wire.SignedGrantSpec{Subject: "alice", Audience: p.Audience, Presenter: p.Presenter, CertificateSha256: p.CertificateSHA256, Scope: scope(), NotBefore: 1900000000, Units: 1, Mode: "single", OperationBinding: intent.ID, SemanticSha256: intent.SemanticSHA256}})
	if e != nil {
		return "", e
	}
	return grant.Material, nil
}
func Check(ctx context.Context, mode string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	f, e := newFixture(ctx)
	if e != nil {
		return e
	}
	defer os.RemoveAll(f.config.Root)
	defer f.close()
	switch mode {
	case "fact", "preference", "inference", "experience":
		request := f.write(f.config.WriteID, 0, "concise")
		request.Write.Spec.Kind = mode
		accepted, e := f.client.Exchange(ctx, request)
		if e != nil {
			return e
		}
		retry, e := f.client.Exchange(ctx, request)
		if e != nil || !proto.Equal(accepted.Receipt, retry.Receipt) {
			return fmt.Errorf("original SDK receipt did not replay")
		}
		result, e := f.read(ctx)
		if e != nil {
			return e
		}
		return require(len(result.Records) == 1 && result.Records[0].Spec.Kind == mode && result.Records[0].Spec.Sources[0].Ref.Revision == 1 && result.Records[0].Revision == 1, "typed memory lost source or revision")
	case "schema-rejection":
		request := f.write(f.config.WriteID, 0, "concise")
		request.Write.Spec.Content.Json = []byte(`{"text":"concise","unapproved":"value"}`)
		if _, e = f.client.Exchange(ctx, request); e != memory.Invalid {
			return fmt.Errorf("unapproved field was not rejected")
		}
		_, e = f.store.LookupOperation(ctx, "local", f.config.WriteID)
		return require(e == memory.Missing, "invalid schema left a receipt")
	case "residency":
		binding := f.binding
		binding.Location = "cloud"
		service, e := memory.New(f.store, f.authority, f.schemas, clock{}, memory.Config{Location: "cloud", Timeout: time.Second})
		if e != nil {
			return e
		}
		reader, e := memory.NewReader(service, f.permits)
		if e != nil {
			return e
		}
		client := sdk.NewMemoryClient(memorylocal.Bind(service, reader, binding), "local")
		if _, e = client.Exchange(ctx, f.write(f.config.WriteID, 0, "concise")); e != memory.Denied {
			return fmt.Errorf("unapproved storage location was accepted")
		}
		rows, e := f.store.Scan(ctx, "local", "personal")
		return require(e == nil && len(rows) == 0, "denied storage retained memory")
	case "revocation":
		if e = f.put(ctx); e != nil {
			return e
		}
		if _, e = f.read(ctx); e != nil {
			return e
		}
		op, e := f.auth.NewOperation(ctx, f.config.Token)
		if e != nil {
			return e
		}
		view, e := f.auth.GetPolicy(ctx, f.config.Token)
		if e != nil {
			return e
		}
		_, e = f.auth.Execute(ctx, f.config.Token, authorization.Mutation{Namespace: "local", OperationID: op, Command: &wire.AuthorizationCommand{ExpectedRevision: view.Revision, Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "deny", Scope: scope(), Deny: true}}}}}})
		if e != nil {
			return e
		}
		_, e = f.read(ctx)
		return require(e == memory.Denied, "revoked policy disclosed an old result")
	case "empty-binding":
		empty, e := f.read(ctx)
		if e != nil || len(empty.Records) != 0 || empty.Coverage != "complete" {
			return fmt.Errorf("empty result is not complete")
		}
		if e = f.put(ctx); e != nil {
			return e
		}
		again, e := f.read(ctx)
		return require(e == nil && len(again.Records) == 0, "empty result expanded on retry")
	case "budget":
		if e = f.put(ctx); e != nil {
			return e
		}
		id, e := f.auth.NewOperation(ctx, f.config.Token)
		if e != nil {
			return e
		}
		q := f.query()
		q.ReadId = id
		q.MaxBytes = 1
		intent, e := memory.DescribeQuery(f.binding, q)
		if e != nil {
			return e
		}
		grant, e := f.sign(ctx, intent)
		if e != nil {
			return e
		}
		out, e := f.client.Exchange(ctx, &wire.MemoryRequest{Method: "QUERY", Query: q, GrantMaterial: grant})
		return require(e == nil && len(out.GetResult().GetRecords()) == 0 && out.GetResult().GetCoverage() == "budget_exhausted", "budget exhaustion was confused with zero matches")
	case "exact-history":
		if e = f.put(ctx); e != nil {
			return e
		}
		if e = f.correct(ctx); e != nil {
			return e
		}
		id, e := f.auth.NewOperation(ctx, f.config.Token)
		if e != nil {
			return e
		}
		q := &wire.MemoryGet{ReadId: id, Ref: &wire.MemoryRef{Namespace: "local", Collection: "personal", Key: "format"}, Revision: 1, Purpose: "assist"}
		intent, e := memory.DescribeGet(f.binding, q)
		if e != nil {
			return e
		}
		grant, e := f.sign(ctx, intent)
		if e != nil {
			return e
		}
		out, e := f.client.Exchange(ctx, &wire.MemoryRequest{Method: "GET", Get: q, GrantMaterial: grant})
		if e != nil {
			return e
		}
		return require(len(out.Result.Records) == 1 && out.Result.Records[0].Revision == 1 && string(out.Result.Records[0].Spec.Content.Json) == `{"text":"concise"}`, "Get substituted current content for history")
	case "concurrent-read":
		if e = f.put(ctx); e != nil {
			return e
		}
		type answer struct {
			result *wire.MemoryReadResult
			err    error
		}
		results := make(chan answer, 2)
		for i := 0; i < 2; i++ {
			go func() { r, e := f.read(ctx); results <- answer{r, e} }()
		}
		correction := f.correct(ctx)
		first, second := <-results, <-results
		if correction != nil {
			return correction
		}
		if first.err != nil {
			return first.err
		}
		if second.err != nil {
			return second.err
		}
		if len(first.result.Records) != 1 || !proto.Equal(first.result, second.result) {
			return fmt.Errorf("concurrent reads bound different selections")
		}
		intent, e := memory.DescribeQuery(f.binding, f.query())
		if e != nil {
			return e
		}
		p := peer()
		use, e := f.grants.LookupUse(ctx, authorization.GrantPresentation{Namespace: "local", Subject: "alice", Audience: p.Audience, Presenter: p.Presenter, CertificateSHA256: p.CertificateSHA256, OperationID: intent.ID, SemanticSHA256: intent.SemanticSHA256})
		if e != nil {
			return e
		}
		grant, e := f.grants.Get(ctx, f.config.Token, use.GrantID)
		return require(e == nil && grant.Allocated == 1, "competing reads reused the single allocation")
	case "invalid-operation":
		_, e = f.client.Exchange(ctx, f.write("not-issued-by-authority", 0, "concise"))
		return require(e == memory.Denied, "unissued operation entered memory")
	}
	return fmt.Errorf("unknown memory profile case")
}
