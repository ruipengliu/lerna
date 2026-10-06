//go:build darwin && cgo

package keys_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/keys"
	"google.golang.org/protobuf/proto"
)

func binding() *v1.ApiTargetBinding {
	return &v1.ApiTargetBinding{UserId: "u", Origin: "https://synthetic.invalid", Account: "synthetic", Resource: "https://synthetic.invalid/action", CredentialRef: command.NewRef("u", "platform-credentials", "api-credential", "lerna.v1.ApiCredentialReference")}
}

// 规则：G5、G7、G8、G12
func TestNativeKeychainReadsOnlyExactSyntheticBinding(t *testing.T) {
	fixture, e := keys.NewSyntheticKeychain()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := fixture.Close(); e != nil {
			t.Error(e)
		}
	})
	b := binding()
	secret := []byte("synthetic-native-canary-6bcb2a6e")
	if e = fixture.Put(b, secret); e != nil {
		t.Fatal(e)
	}
	got, e := fixture.Store().Resolve(context.Background(), b)
	if e != nil || !bytes.Equal(got, secret) {
		t.Fatalf("native roundtrip failed: %v", e)
	}
	clear(got)
	for _, kind := range []string{"user", "account", "origin", "resource", "ref", "ref-version", "ref-schema"} {
		t.Run(kind, func(t *testing.T) {
			different := proto.Clone(b).(*v1.ApiTargetBinding)
			switch kind {
			case "user":
				different.UserId = "other"
			case "account":
				different.Account = "other"
			case "origin":
				different.Origin = "https://other.invalid"
			case "resource":
				different.Resource = "https://synthetic.invalid/another-action"
			case "ref":
				different.CredentialRef.Name.LocalId = "missing"
			case "ref-version":
				different.CredentialRef.Revision++
			case "ref-schema":
				different.CredentialRef.SchemaId = "lerna.v1.Content"
			}
			got, e := fixture.Store().Resolve(context.Background(), different)
			if e == nil || len(got) != 0 {
				t.Fatalf("unbound item returned for %s", kind)
			}
		})
	}
}

// 规则：G5、G7、G8、G12
func TestNativeCredentialLookupCannotFallBackToAnotherKeychain(t *testing.T) {
	first, e := keys.NewSyntheticKeychain()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := first.Close(); e != nil {
			t.Error(e)
		}
	})
	second, e := keys.NewSyntheticKeychain()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := second.Close(); e != nil {
			t.Error(e)
		}
	})
	b := binding()
	if e = second.Put(b, []byte("synthetic-other-store-canary-145ab7de")); e != nil {
		t.Fatal(e)
	}
	got, e := first.Store().Resolve(context.Background(), b)
	defer clear(got)
	if e == nil || len(got) != 0 {
		t.Fatal("native lookup fell back outside explicit store")
	}
}
