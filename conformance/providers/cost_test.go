package providers_test

import (
	"github.com/ruipengliu/lerna/adapters/providers"
	"github.com/ruipengliu/lerna/api"
	"testing"
)

func TestFrozenCostBoundUsesWholeReservationAndCeilsSixDecimalUSD(t *testing.T) {
	cfg := configuration(t, "https://provider.example/v1/chat/completions")
	engine, err := providers.NewOpenAI(cfg)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := engine.CostBound(100, 20)
	if err != nil || len(bound) != 1 || bound[0] != (api.Amount{Unit: "USD", Value: "0.00028"}) {
		t.Fatalf("noncached complete bound: %+v %v", bound, err)
	}
	cfg.InputUSDPerMillion = "0.3"
	cfg.CachedInputUSDPerMillion = "0.1"
	cfg.OutputUSDPerMillion = "0.6"
	engine, err = providers.NewOpenAI(cfg)
	if err != nil {
		t.Fatal(err)
	}
	bound, err = engine.CostBound(1, 1)
	if err != nil || len(bound) != 1 || bound[0] != (api.Amount{Unit: "USD", Value: "0.000001"}) {
		t.Fatalf("six decimal ceiling: %+v %v", bound, err)
	}
	cfg.CachedInputUSDPerMillion = "3"
	engine, err = providers.NewOpenAI(cfg)
	if err != nil {
		t.Fatal(err)
	}
	bound, err = engine.CostBound(100, 20)
	if err != nil || bound[0] != (api.Amount{Unit: "USD", Value: "0.000312"}) {
		t.Fatalf("higher cached rate also bound: %+v %v", bound, err)
	}
	if _, err = engine.CostBound(api.MaxSafeInteger, 20); !api.IsCode(err, "invalid_request") {
		t.Fatalf("budget bound outside frozen profile: %v", err)
	}
}
