package providers_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ruipengliu/lerna/adapters/providers"
	"github.com/ruipengliu/lerna/api"
)

func TestReservedOutputTokensAreTheActualPhysicalLimit(t *testing.T) {
	cfg := configuration(t, "https://provider.example/v1/chat/completions")
	engine, err := providers.NewOpenAI(cfg)
	if err != nil {
		t.Fatal(err)
	}
	original := encoded(t, engine, cfg)
	var wire struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err = json.Unmarshal(original.Body, &wire); err != nil {
		t.Fatal(err)
	}
	var input struct {
		Snapshot api.Snapshot `json:"snapshot"`
		Goal     string       `json:"goal_utf8"`
	}
	if err = json.Unmarshal([]byte(wire.Messages[1].Content), &input); err != nil {
		t.Fatal(err)
	}
	input.Snapshot.ReservedOutputTokens = 20
	enc, err := engine.Encode(context.Background(), input.Snapshot, []byte(input.Goal), engine.Profile())
	if err != nil {
		t.Fatal(err)
	}
	var physical struct {
		Limit uint64 `json:"max_completion_tokens"`
	}
	if err = json.Unmarshal(enc.Body, &physical); err != nil || physical.Limit != 20 {
		t.Fatalf("physical output exceeded reserved budget: %+v %v", physical, err)
	}
	input.Snapshot.ReservedOutputTokens = engine.Profile().MaxOutputTokens + 1
	if _, err = engine.Encode(context.Background(), input.Snapshot, []byte(input.Goal), engine.Profile()); !api.IsCode(err, "invalid_request") {
		t.Fatalf("output beyond configured capability: %v", err)
	}
}

type changingCounter struct{ reference api.ComponentRef }

func (c *changingCounter) Ref() api.ComponentRef { return c.reference }
func (c *changingCounter) Count(ctx context.Context, b []byte) (uint64, string, error) {
	return (providers.UTF8UpperBound{}).Count(ctx, b)
}

func TestTokenizerReferenceRemainsFrozenAfterConstruction(t *testing.T) {
	cfg := configuration(t, "https://provider.example/v1/chat/completions")
	counter := &changingCounter{reference: (providers.UTF8UpperBound{}).Ref()}
	cfg.Tokenizer = counter
	engine, err := providers.NewOpenAI(cfg)
	if err != nil {
		t.Fatal(err)
	}
	enc := encoded(t, engine, cfg)
	original := engine.TokenizerRef()
	counter.reference.Version = "changed"
	if engine.TokenizerRef() != original {
		t.Fatal("a configured mutable port changed the frozen tokenizer identity")
	}
	if _, err = engine.Request(context.Background(), api.NewID("call"), enc); !api.IsCode(err, "revision_conflict") {
		t.Fatalf("drift before send: %v", err)
	}
}

func TestTariffRecipientAndByteLimitsBelongToProfile(t *testing.T) {
	cfg := configuration(t, "https://provider.example/v1/chat/completions")
	engine, err := providers.NewOpenAI(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*providers.OpenAIConfig){
		func(c *providers.OpenAIConfig) { c.OutputUSDPerMillion = "5" },
		func(c *providers.OpenAIConfig) { c.Receiver = "another-provider" },
		func(c *providers.OpenAIConfig) { c.Location = "device" },
		func(c *providers.OpenAIConfig) { c.Guidance = "different durable input protocol" },
		func(c *providers.OpenAIConfig) { c.CredentialID = "rotated-key" },
		func(c *providers.OpenAIConfig) { c.MaxResponseBytes = 1024 },
	} {
		changed := cfg
		changed.Profile = engine.Profile()
		change(&changed)
		if _, err = providers.NewOpenAI(changed); !api.IsCode(err, "revision_conflict") {
			t.Fatalf("frozen profile mismatch: %v", err)
		}
	}
	noKey := cfg
	noKey.APIKey = ""
	if _, err = providers.NewOpenAI(noKey); !api.IsCode(err, "unsupported") {
		t.Fatalf("unconfigured live provider must remain closed: %v", err)
	}
	tooSmall := cfg
	tooSmall.Profile.MaxInputBytes = 1024
	small, err := providers.NewOpenAI(tooSmall)
	if err != nil {
		t.Fatal(err)
	}
	goal := []byte("字节预算")
	ref := api.ContentRef{TenantID: cfg.Scope.TenantID, OwnerID: cfg.Scope.OwnerID, ContentID: api.NewID("content"), Version: 1, Hash: api.Hash(goal), MediaType: "text/plain", ByteLength: uint64(len(goal))}
	snapshot := api.Snapshot{TaskRef: cfg.Scope.Ref(api.NewID("task"), 1), GoalRef: ref, ModelProfileRef: small.Profile().Ref, TokenizerRef: small.TokenizerRef()}
	if _, err = small.Encode(context.Background(), snapshot, goal, small.Profile()); !api.IsCode(err, "invalid_request") {
		t.Fatalf("the complete protocol, schema and goal exceed the wire budget: %v", err)
	}
}
