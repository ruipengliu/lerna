package providers_test

import (
	"testing"

	"github.com/ruipengliu/lerna/adapters/providers"
	"github.com/ruipengliu/lerna/api"
)

// Native consumer derives contracts without creating a Store, source authority or signer.
func TestForeignSourceContractsExposeActualBoundedWire(t *testing.T) {
	contracts := providers.ForeignSourceContracts()
	if len(contracts) != 4 {
		t.Fatalf("foreign source methods: %d", len(contracts))
	}
	byName := map[string]api.MethodContract{}
	for _, c := range contracts {
		if _, exists := byName[c.Name]; exists {
			t.Fatal("duplicate source contract")
		}
		byName[c.Name] = c
		if _, err := api.Digest(c); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"content.foreign.register", "content.foreign.release", "content.foreign.current", "content.foreign.get"} {
		if _, ok := byName[name]; !ok {
			t.Fatalf("missing actual source method %s", name)
		}
	}
	current := byName["content.foreign.current"].OutputSchema["properties"].(map[string]any)
	get := byName["content.foreign.get"].OutputSchema["properties"].(map[string]any)
	proof := current["proof"].(api.Schema)
	body := get["data_base64"].(api.Schema)
	if proof["maxLength"] != 32768 || body["maxLength"] != 87384 {
		t.Fatalf("bounded native wire proof=%v body=%v", proof, body)
	}
	proof["maxLength"] = 1
	fresh := providers.ForeignSourceContracts()[2].OutputSchema["properties"].(map[string]any)["proof"].(api.Schema)
	if fresh["maxLength"] != 32768 {
		t.Fatal("caller mutation changed authoritative contracts")
	}
}
