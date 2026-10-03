package development

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
	harness "github.com/ruipengliu/lerna/sdk/go"
)

func TestGrantListPublicTLSDiscoveryAndGoSDKUseCurrentMetadataGate(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" {
				if os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
					t.Skip("requires actual HARNESS_TEST_POSTGRES_DSN")
				}
				t.Setenv("HARNESS_DATABASE_DSN", os.Getenv("HARNESS_TEST_POSTGRES_DSN"))
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			root := t.TempDir()
			c, err := InitializeConfig(ctx, filepath.Join(root, "config.json"), root, driver)
			if err != nil {
				t.Fatal(err)
			}
			c.TenantID, c.OwnerID, c.SubjectID = api.NewID("tenant"), api.NewID("owner"), api.NewID("subject")
			c.UserRoles = []string{"trusted_renderer"}
			a, err := OpenAppForRole(ctx, c, true, "management")
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := a.Close(); err != nil {
					t.Error(err)
				}
			}()
			own := map[string]bool{}
			status, err := a.Store.Within(ctx, a.Scope, []string{"governance"}, func(tx runtime.Tx) error {
				for i := 0; i < 2; i++ {
					id := api.NewID("grant")
					g := api.Grant{GrantID: id, OwnerID: a.Scope.OwnerID, Revision: 1, SubjectRef: a.UserAuth.Ref(a.Scope.OwnerID), Resources: []string{"document"}, Actions: []string{"read"}, Purposes: []string{"review"}, Recipients: []string{"renderer"}, Locations: []string{"cloud"}, Mode: "continuous", State: "active", NotBefore: api.Time(time.Now().Add(-time.Minute)), ExpiresAt: api.Time(time.Now().Add(time.Hour)), Limits: []api.Amount{}}
					// Explicit trusted management initial grants, not user confirmations.
					if err := a.Governance.ProvisionGrantTx(ctx, tx, a.ServiceAuth, g); err != nil {
						return err
					}
					own[id] = true
				}
				return nil
			})
			if err != nil || status != runtime.Committed {
				t.Fatal(err)
			}
			gateway, err := a.Gateway()
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewTLSServer(gateway.Handler())
			defer server.Close()
			token, err := os.ReadFile(c.TokenFile)
			if err != nil {
				t.Fatal(err)
			}
			transport := &harness.HTTPTransport{BaseURL: server.URL, Token: strings.TrimSpace(string(token)), HTTP: server.Client()}
			discovery, err := transport.Discover(ctx)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, method := range discovery.Methods {
				if method.Name == "grant.list" {
					found = method.Owner == "governance" && method.Kind == "query" && method.SchemaDigest != ""
				}
			}
			if !found {
				t.Fatal("same-version authenticated discovery omitted the closed grant list")
			}
			journal, err := harness.OpenJournal(filepath.Join(root, "sdk-journal"), discovery.IdentityScope)
			if err != nil {
				t.Fatal(err)
			}
			defer journal.Close()
			client, err := harness.NewClient(transport, journal, discovery)
			if err != nil {
				t.Fatal(err)
			}
			q := api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, QueryID: api.NewID("query"), Method: "grant.list", TargetID: a.Scope.OwnerID, Payload: api.Raw(api.ListInput{Limit: 1})}
			firstQueryID := q.QueryID
			raw, err := client.Query(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			var first api.Page[governance.GrantRecord]
			if err = api.Decode(raw, &first); err != nil || len(first.Items) != 1 || !own[first.Items[0].Grant.GrantID] || first.NextCursor == "" {
				t.Fatal("ordinary TLS caller saw another subject's grant or incomplete paging")
			}
			q.QueryID = api.NewID("query")
			nextQueryID := q.QueryID
			q.Payload = api.Raw(api.ListInput{Limit: 1, Cursor: first.NextCursor})
			raw, err = client.Query(ctx, q)
			var second api.Page[governance.GrantRecord]
			if err != nil || api.Decode(raw, &second) != nil || len(second.Items) != 1 || !own[second.Items[0].Grant.GrantID] || first.Items[0].Grant.GrantID == second.Items[0].Grant.GrantID || !second.Exhausted {
				t.Fatalf("SDK continuation did not retain the original metadata scope: %v", err)
			}
			if err = a.Identity.Revoke(ctx, a.UserAuth); err != nil {
				t.Fatal(err)
			}
			for _, cursor := range []string{"", first.NextCursor} {
				q.QueryID, q.Payload = api.NewID("query"), api.Raw(api.ListInput{Limit: 1, Cursor: cursor})
				if raw, err = client.Query(ctx, q); len(raw) != 0 || !api.IsCode(err, "forbidden") {
					t.Fatalf("current credential revocation passed TLS/SDK list gate: %v", err)
				}
			}
			t.Logf("PUBLIC_GRANT_LIST_EVIDENCE %s", api.Raw(struct {
				Scope    runtime.Scope `json:"scope"`
				QueryID  string        `json:"query_id"`
				NextID   string        `json:"next_query_id"`
				Revision uint64        `json:"collection_revision"`
			}{a.Scope, firstQueryID, nextQueryID, first.CollectionRevision}))
		})
	}
}
