package development

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
)

// 通过实际宿主的公开 Command/Query 与不可变 Content，不预填 Skill 状态。
func TestSkillCatalogValidatesExactPublishedBytesThroughPublicHost(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL DSN required")
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			root := t.TempDir()
			cfg, err := InitializeConfig(ctx, filepath.Join(root, "config.json"), root, driver)
			if err != nil {
				t.Fatal(err)
			}
			cfg.TenantID, cfg.OwnerID, cfg.SubjectID = api.NewID("tenant"), api.NewID("owner"), api.NewID("subject")
			a, err := OpenApp(ctx, cfg, true)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			skill, body := registerPublishedKnowledgeSkill(t, ctx, a)
			raw, err := a.query(ctx, "skill.load", skill.SkillRef.ComponentID, governance.SkillReference{SkillRef: skill.SkillRef})
			var loaded governance.LoadedSkill
			if err != nil || api.Decode(raw, &loaded) != nil || loaded.Body != body || !api.Equal(loaded.Definition, skill) || len(loaded.Usage.Counterexamples) != 1 || len(loaded.Usage.ExitRules) != 1 {
				t.Fatalf("original published Skill bytes and contract changed: %v %+v", err, loaded)
			}
		})
	}
}

func registerPublishedKnowledgeSkill(t *testing.T, ctx context.Context, a *App) (governance.SkillDefinition, string) {
	t.Helper()
	body := "Inspect original saved bytes before proposing completion. Ordinary knowledge cannot issue a Grant or decide Task success."
	publish := func(media string, b []byte) api.ContentRef {
		t.Helper()
		ref, err := a.Publish(ctx, a.Scope, a.UserAuth, api.NewID("content"), media, b, []api.ContentRef{}, []api.ContentRef{})
		if err != nil {
			t.Fatal(err)
		}
		return ref
	}
	usage := governance.SkillUsageContract{Purpose: "report", Preconditions: []string{"The original target is disclosed and currently permitted."}, Counterexamples: []string{"A write receipt alone does not prove a successful readback."}, ToolDependencies: []api.ComponentRef{}, EvidenceDependencies: []api.ContentRef{}, Conflicts: []api.ComponentRef{}, ExitRules: []string{"Stop when the original Effect remains unknown."}}
	skill, err := governance.SealSkill(governance.SkillDefinition{SkillRef: api.ComponentRef{ComponentID: api.NewID("skill"), Version: "1"}, BodyRef: publish("text/plain", []byte(body)), UsageContractRef: publish("application/json", api.Raw(usage)), SourceRefs: []api.ContentRef{}})
	if err != nil {
		t.Fatal(err)
	}
	r := knowledgePublicCommand(t, ctx, a, "skill.register", skill.SkillRef.ComponentID, skill, nil)
	if r.Stage != "accepted" {
		t.Fatalf("Skill original validation was not durably accepted: %+v", r)
	}
	if err = runtime.Drain(ctx, a.Store, a.Scope, a.Registry, 200); err != nil {
		t.Fatal(err)
	}
	raw, err := a.query(ctx, "skill.get", skill.SkillRef.ComponentID, governance.SkillReference{SkillRef: skill.SkillRef})
	var current governance.SkillRecord
	if err != nil || api.Decode(raw, &current) != nil || current.State != "active" || current.Usage == nil || !api.Equal(current.Definition, skill) {
		t.Fatalf("original Skill validation did not finish: %v %+v", err, current)
	}
	return skill, body
}

func knowledgePublicCommand(t *testing.T, ctx context.Context, a *App, method, target string, payload any, expected *uint64) api.Receipt {
	t.Helper()
	r, err := a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), TargetID: target, ExpectedRevision: expected, Method: method, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(payload)}))
	if err != nil || r.Error != nil {
		t.Fatalf("%s: %v %+v", method, err, r.Error)
	}
	return r
}
