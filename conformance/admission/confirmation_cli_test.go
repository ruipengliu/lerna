package admission_test

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/ruipengliu/lerna/adapters/interaction"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// 规则：G3、G4、G7、准入-9
func TestCLIShowsCoreMatterThenExplicitlyConfirmsAndRevokesGrant(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	cli := interaction.CLI{Grants: f.h.Grants, Confirmations: f.h.Sessions, ConfirmationTasks: f.h.Tasks, Sessions: f.h.Sessions, Tasks: f.h.Tasks, Durable: f.h.Durable, Caller: f.caller, Domain: "d"}
	g, e := f.h.Grants.QueryGrant(f.ctx, f.caller, f.grant)
	if e != nil {
		t.Fatal(e)
	}
	g.Ref = nil
	g.Issuer = nil
	g.Status = ""
	g.UsePoolId = ""
	body, e := protojson.Marshal(g)
	if e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	run := func(args ...string) {
		t.Helper()
		out.Reset()
		if e := cli.Run(f.ctx, args, &out); e != nil {
			t.Fatal(e)
		}
	}
	run("grant-request", "--command", "cli-request", "--json", string(body))
	receipt := &v1.CommandReceipt{}
	if e := protojson.Unmarshal(out.Bytes(), receipt); e != nil {
		t.Fatal(e)
	}
	run("confirmation", receipt.ResultRef.Name.LocalId)
	c := &v1.Confirmation{}
	if e := protojson.Unmarshal(out.Bytes(), c); e != nil {
		t.Fatal(e)
	}
	if c.BindingDigest == "" || !strings.Contains(c.Description, "GRANT_ISSUANCE") {
		t.Fatal("missing core-generated matter")
	}
	run("confirm", "--command", "cli-confirm", "--confirmation", c.Ref.Name.LocalId, "--revision", strconv.FormatUint(c.Ref.Revision, 10), "--digest", c.BindingDigest, "--decision", "APPROVE")
	if e := protojson.Unmarshal(out.Bytes(), receipt); e != nil {
		t.Fatal(e)
	}
	run("grant-issue", "--command", "cli-issue", "--confirmation", c.Ref.Name.LocalId, "--revision", strconv.FormatUint(receipt.ResultRef.Revision, 10))
	if e := protojson.Unmarshal(out.Bytes(), receipt); e != nil {
		t.Fatal(e)
	}
	grantID := receipt.ResultRef.Name.LocalId
	run("grant", grantID)
	if !strings.Contains(out.String(), "ACTIVE") {
		t.Fatal(out.String())
	}
	run("revoke-grant", "--command", "cli-revoke", "--grant", grantID)
	run("grant", grantID)
	if !strings.Contains(out.String(), "REVOKED") || !strings.Contains(out.String(), "COMPLETE") {
		t.Fatal(out.String())
	}
	if f.calls.Load() != 0 {
		t.Fatal("CLI called target")
	}
}
