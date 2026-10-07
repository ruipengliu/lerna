package admission_test

import (
	"testing"
	"time"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/ledger"
	"github.com/ruipengliu/lerna/infra/rules"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G11、R6
func TestAPIHistoryNeedsConfiguredTrustedRulesBeforeRecovery(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	configureAPI(t, f)
	a, _ := prepareStart(t, f)
	before, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	for _, missing := range []ledger.EvidenceRules{nil, (*rules.API)(nil)} {
		f.h.Ledger.WithEvidenceRules(missing)
		if e = f.h.Ledger.CheckStartupCompatibility(f.ctx); e == nil || e.Error() != "missing required dependency: ledger.rules" {
			t.Fatalf("missing original rules: %v", e)
		}
		after, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
		if e != nil || !proto.Equal(before, after) || f.calls.Load() != 0 {
			t.Fatal("rule configuration failure advanced original responsibility")
		}
	}
	f.h.Ledger.WithEvidenceRules(rules.API{})
	if e = f.h.Ledger.CheckStartupCompatibility(f.ctx); e != nil {
		t.Fatalf("supported original rules: %v", e)
	}
}

func referenceAPIDescriptor(target string) *v1.ApiDescriptor {
	d := &v1.ApiDescriptor{Provider: "lerna-reference", Environment: "synthetic", Version: "1", ProtocolVersion: "lerna-reference-api-v1", Serialization: "reference-json-v1", MediaType: "application/json", Authentication: "BEARER", Idempotent: true, Binding: &v1.ApiTargetBinding{UserId: "u", Origin: target, Resource: target, Account: "synthetic-account", CredentialRef: &v1.Ref{Name: &v1.GlobalName{UserId: "u", AuthorityDomainId: "platform-credentials", ObjectKind: "api-credential", LocalId: "synthetic-api"}, Revision: 1, SchemaId: "lerna.v1.ApiCredentialReference"}}}
	d.Digest = command.APIDescriptorDigest(d)
	return d
}

func configureAPIQueryable(t *testing.T, f *fixture, idempotent bool) (*v1.ApiDescriptor, *v1.Ref, *v1.Ref) {
	return configureAPIQueryableAt(t, f, idempotent, "")
}
func configureAPIQueryableAt(t *testing.T, f *fixture, idempotent bool, target string) (*v1.ApiDescriptor, *v1.Ref, *v1.Ref) {
	t.Helper()
	configureAPIAt(t, f, target)
	write, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	if e != nil {
		t.Fatal(e)
	}
	write.Ref = nil
	write.ApprovedBy = nil
	write.ApiDescriptor.Queryable = true
	write.ApiDescriptor.Idempotent = idempotent
	write.ApiDescriptor.Digest = command.APIDescriptorDigest(write.ApiDescriptor)
	r, e := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("api-queryable-write"), Capability: write})
	accepted(t, r, e)
	f.capability = r.ResultRef
	read := proto.Clone(write).(*v1.Capability)
	read.Action = "QUERY"
	read.UseRight = "READ"
	fee := int64(5)
	read.FeeCeiling = &fee
	read.MaxSends = 1
	r, e = f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("api-read-cap"), Capability: read})
	accepted(t, r, e)
	queryCap := r.ResultRef
	r, e = f.h.Grants.Configure(f.ctx, f.caller, &v1.ConfigureGrantCommand{Header: header("api-read-grant"), Grant: &v1.Grant{Subject: f.task.Name, Permissions: []*v1.PermissionClause{{Action: "QUERY", Resource: read.Resource, UseRight: "READ", ProcessingPurpose: "CURRENT_TASK", ExecutorEndpointId: read.ExecutorEndpointId}, {Action: "QUERY", Resource: read.Resource, UseRight: "SAVE", ProcessingPurpose: "CURRENT_TASK", ExecutorEndpointId: read.ExecutorEndpointId}}, ValidFromUnixMs: time.Now().Add(-time.Minute).UnixMilli(), ValidUntilUnixMs: time.Now().Add(time.Hour).UnixMilli(), UseMode: "CONTINUOUS", UsePoolId: "api-read-root"}})
	accepted(t, r, e)
	return write.ApiDescriptor, queryCap, r.ResultRef
}

func configureAPI(t *testing.T, f *fixture) *v1.ApiDescriptor {
	return configureAPIAt(t, f, "")
}
func configureAPIAt(t *testing.T, f *fixture, target string) *v1.ApiDescriptor {
	t.Helper()
	cap, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	if e != nil {
		t.Fatal(e)
	}
	cap.Ref = nil
	cap.ApprovedBy = nil
	cap.AdapterRef.Name.LocalId = "api-reference-v1"
	if target != "" {
		cap.Resource = target
	}
	cap.ApiDescriptor = referenceAPIDescriptor(cap.Resource)
	r, e := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("api-capability"), Capability: cap})
	accepted(t, r, e)
	f.capability = r.ResultRef
	if target != "" {
		grant, e := f.h.Grants.QueryGrant(f.ctx, f.caller, f.grant)
		if e != nil {
			t.Fatal(e)
		}
		grant.Ref = nil
		grant.Issuer = nil
		grant.Status = ""
		grant.Permissions[0].Resource = target
		r, e = f.h.Grants.Configure(f.ctx, f.caller, &v1.ConfigureGrantCommand{Header: header("api-target-grant"), Grant: grant})
		accepted(t, r, e)
		f.grant = r.ResultRef
	}
	f.parameters, e = f.h.Content.Stage(f.ctx, f.caller, &v1.SubmitGoalCommand{Identity: header("api-parameters").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: ` { "value":"hello" } `})
	if e != nil {
		t.Fatal(e)
	}
	return cap.ApiDescriptor
}

// 规则：G3、G4、G5、G7、开始-4
func TestAPIFixedDescriptorCompilesCanonicalBody(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	d := configureAPI(t, f)
	a, c := prepareStart(t, f)
	if c.CallDescriptor.ApiDescriptor.GetDigest() != d.Digest || c.CallDescriptor.BodyDigest != command.BytesDigest([]byte(`{"quantity":1,"value":"hello"}`)) || c.CallDescriptor.ParametersDigest != command.BytesDigest([]byte(` { "value":"hello" } `)) || c.CallDescriptor.ExternalKey != c.Binding.AttemptId.LocalId {
		t.Fatalf("descriptor: %v", c.CallDescriptor)
	}
	x, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil || x.Attempt.Capabilities.ProtocolVersion != "lerna-reference-api-v1" || f.calls.Load() != 0 {
		t.Fatalf("execution %v %v", x, e)
	}
}

// 规则：H1、G3、G4、G7、G8
func TestAPICapabilityRejectsUnknownNestedContractWithoutTargetUse(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	configureAPI(t, f)
	original, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	if e != nil {
		t.Fatal(e)
	}
	for _, part := range []string{"descriptor", "binding", "credential-ref"} {
		t.Run(part, func(t *testing.T) {
			changed := proto.Clone(original).(*v1.Capability)
			changed.Ref, changed.ApprovedBy = nil, nil
			var nested proto.Message = changed.ApiDescriptor
			if part == "binding" {
				nested = changed.ApiDescriptor.Binding
			}
			if part == "credential-ref" {
				nested = changed.ApiDescriptor.Binding.CredentialRef
			}
			nested.ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01})
			body, e := proto.Marshal(changed)
			if e != nil {
				t.Fatal(e)
			}
			forwarded := new(v1.Capability)
			if e = proto.Unmarshal(body, forwarded); e != nil {
				t.Fatal(e)
			}
			r, e := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("api-unknown-" + part), Capability: forwarded})
			if e == nil || r != nil {
				t.Fatal("unknown nested API authority was accepted")
			}
			retained, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, original.Ref)
			if e != nil || !proto.Equal(retained, original) || f.calls.Load() != 0 {
				t.Fatal("unsupported contract changed known capability or target")
			}
		})
	}
}
