package platform_test

import (
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"testing"
	"time"
)

func TestES256BindsExactOwnerWindowAndTenant(t *testing.T) {
	tenant, issuer := api.NewID("ten"), api.NewID("srv")
	keys, e := platform.NewDevelopmentKey(tenant, issuer, []string{"control"})
	if e != nil {
		t.Fatal(e)
	}
	now := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	claims := platform.ProofClaims{TenantID: tenant, Issuer: issuer, Audience: api.NewID("srv"), Purpose: "control", ObjectRef: api.ObjectRef{TenantID: tenant, OwnerID: issuer, ObjectID: api.NewID("task"), Revision: 1}, Digest: api.Hash([]byte("exact intent")), WindowID: api.NewID("win"), IssuedAt: api.Time(now), StartBefore: api.Time(now.Add(time.Minute))}
	signed, e := keys.Sign("development-es256", claims)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = keys.Verify(signed, claims, now.Add(time.Second)); e != nil {
		t.Fatal(e)
	}
	wrong := claims
	wrong.Audience = api.NewID("srv")
	if _, e = keys.Verify(signed, wrong, now.Add(time.Second)); e == nil {
		t.Fatal("recipient substitution accepted")
	}
	if _, e = keys.Verify(signed, claims, now.Add(time.Minute)); e == nil {
		t.Fatal("expired window accepted")
	}
	if _, e = keys.Verify(signed+"x", claims, now); e == nil {
		t.Fatal("tampered signature accepted")
	}
}
