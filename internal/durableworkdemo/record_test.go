package durableworkdemo_test

import (
	"encoding/json"
	"strings"
	"testing"

	durablework "github.com/ruipengliu/lerna/internal/durableworkdemo"
)

func recordBody(text string) []byte {
	value := map[string]any{"contract_version": "host-durable-work-1", "profile": "host", "method": "durable_work.record", "command_id": "cmd-one", "target": map[string]any{"tenant_id": "tenant-one", "owner_id": "owner-one", "kind": "durable_work", "id": "object-one"}, "payload": map[string]any{"text": text}, "accept_before": "2099-01-01T00:00:00.000000Z"}
	data, _ := json.Marshal(value)
	return data
}
func TestRecordHasDedicatedClosedSchema(t *testing.T) {
	for _, text := range []string{"", "  e\u0301🌍  ", strings.Repeat("🌍", 65536)} {
		got, err := durablework.DecodeRecord(recordBody(text))
		if err != nil || got.Text != text {
			t.Fatalf("exact text rejected/changed: %v", err)
		}
	}
	for _, data := range [][]byte{
		recordBody(strings.Repeat("a", 65537)),
		[]byte(strings.Replace(string(recordBody("x")), `"text":"x"`, `"text":"x","extra":"no"`, 1)),
		[]byte(strings.Replace(string(recordBody("x")), `"kind":"durable_work"`, `"kind":"durable_work","revision":"1"`, 1)),
		[]byte(strings.Replace(string(recordBody("x")), `"host-durable-work-1"`, `"1.0.0"`, 1)),
		[]byte(strings.Replace(string(recordBody("x")), `"text":"x"`, `"text":"x","text":"y"`, 1)),
	} {
		if _, err := durablework.DecodeRecord(data); err == nil {
			t.Fatal("invalid internal record accepted")
		}
	}
}
