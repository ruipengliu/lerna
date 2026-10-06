package command_test

import (
	"bytes"
	"testing"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G4、G5、开始-4
func TestAPIParametersCompileDeterministically(t *testing.T) {
	for _, input := range []string{`{"value":"hello"}`, ` { "quantity":1, "value":"hello" } `, `{"value":"hello","quantity":1}`} {
		got, err := command.CompileAPIParameters([]byte(input))
		if err != nil || !bytes.Equal(got, []byte(`{"quantity":1,"value":"hello"}`)) {
			t.Fatalf("compiled %q: %q %v", input, got, err)
		}
	}
}

// 规则：G4、G7、开始-4
func TestAPIParametersRejectAmbiguousOrInjectedFields(t *testing.T) {
	for _, input := range []string{`{"value":null}`, `{"value":"x","value":"y"}`, `{"value":"x","quantity":"1"}`, `{"value":"x","quantity":1.0}`, `{"value":"x","quantity":1e0}`, `{"value":"x","quantity":-1}`, `{"value":"x","quantity":1000001}`, `{"value":"x","headers":{"Authorization":"evil"}}`, `{"value":"x"} {}`, string([]byte{'{', '"', 'v', 'a', 'l', 'u', 'e', '"', ':', '"', 0xff, '"', '}'})} {
		if got, err := command.CompileAPIParameters([]byte(input)); err == nil {
			t.Fatalf("accepted %q: %q", input, got)
		}
	}
}

// 规则：G4、G5、G7、G8、开始-4、开始-5
func TestAPIDescriptionRejectsUnreviewedVersionAndCrossBoundCredential(t *testing.T) {
	base := &v1.ApiDescriptor{Provider: "lerna-reference", Environment: "synthetic", Version: "1", ProtocolVersion: "lerna-reference-api-v1", Serialization: "reference-json-v1", MediaType: "application/json", Authentication: "BEARER", Binding: &v1.ApiTargetBinding{UserId: "u", Origin: "http://127.0.0.1:12345", Resource: "http://127.0.0.1:12345/records", Account: "synthetic-account", CredentialRef: &v1.Ref{Name: &v1.GlobalName{UserId: "u", AuthorityDomainId: "platform-credentials", ObjectKind: "api-credential", LocalId: "synthetic-reference"}, Revision: 1, SchemaId: "lerna.v1.ApiCredentialReference"}}}
	base.Digest = command.APIDescriptorDigest(base)
	if e := command.ValidateAPIDescriptor(base, "u", base.Binding.Resource); e != nil {
		t.Fatal(e)
	}
	for _, scenario := range []string{"provider", "environment", "version", "protocol", "serialization", "media", "authentication", "digest", "user", "credential-user", "credential-authority", "credential-kind", "credential-version", "credential-schema", "account-empty", "account-header", "origin", "resource", "url-credentials", "url-query", "url-fragment", "url-scheme", "url-external-http", "url-port-zero", "url-port-large", "url-empty-port", "unknown-descriptor", "unknown-binding", "unknown-credential-ref"} {
		t.Run(scenario, func(t *testing.T) {
			d := proto.Clone(base).(*v1.ApiDescriptor)
			target := base.Binding.Resource
			switch scenario {
			case "unknown-descriptor":
				d.ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01})
			case "unknown-binding":
				d.Binding.ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01})
			case "unknown-credential-ref":
				d.Binding.CredentialRef.ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01})
			case "provider":
				d.Provider = "arbitrary-provider"
			case "environment":
				d.Environment = "production"
			case "version":
				d.Version = "2"
			case "protocol":
				d.ProtocolVersion = "unreviewed-v1"
			case "serialization":
				d.Serialization = "arbitrary-json"
			case "media":
				d.MediaType = "application/octet-stream"
			case "authentication":
				d.Authentication = "QUERY_PARAMETER"
			case "user":
				d.Binding.UserId = "another-user"
			case "credential-user":
				d.Binding.CredentialRef.Name.UserId = "another-user"
			case "credential-authority":
				d.Binding.CredentialRef.Name.AuthorityDomainId = "another-store"
			case "credential-kind":
				d.Binding.CredentialRef.Name.ObjectKind = "content"
			case "credential-version":
				d.Binding.CredentialRef.Revision = 2
			case "credential-schema":
				d.Binding.CredentialRef.SchemaId = "lerna.v1.Content"
			case "account-empty":
				d.Binding.Account = ""
			case "account-header":
				d.Binding.Account = "account\r\nAuthorization: secret"
			case "origin":
				d.Binding.Origin = "https://another.invalid"
			case "resource":
				d.Binding.Resource += "/another"
			case "url-credentials":
				target = "https://user:password@example.invalid/records"
				d.Binding.Origin = "https://example.invalid"
			case "url-query":
				target += "?Authorization=secret"
			case "url-fragment":
				target += "#other"
			case "url-scheme":
				target = "file:///records"
				d.Binding.Origin = "file://"
			case "url-external-http":
				target = "http://192.0.2.1/records"
				d.Binding.Origin = "http://192.0.2.1"
			case "url-port-zero":
				target = "http://127.0.0.1:0/records"
				d.Binding.Origin = "http://127.0.0.1:0"
			case "url-port-large":
				target = "https://example.invalid:65536/records"
				d.Binding.Origin = "https://example.invalid:65536"
			case "url-empty-port":
				target = "https://example.invalid:/records"
				d.Binding.Origin = "https://example.invalid:"
			}
			if target != base.Binding.Resource {
				d.Binding.Resource = target
			}
			d.Digest = command.APIDescriptorDigest(d)
			if scenario == "digest" {
				d.Digest = "not-the-fixed-material"
			}
			if e := command.ValidateAPIDescriptor(d, "u", target); e == nil {
				t.Fatalf("accepted changed descriptor %s", scenario)
			}
		})
	}
}
