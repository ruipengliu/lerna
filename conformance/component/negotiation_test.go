package component_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ruipengliu/lerna/contract"
	wire "github.com/ruipengliu/lerna/contract/gen/go"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestNegotiationAdvertisesOnlyCompletedCommandGet(t *testing.T) {
	manifest := contract.SupportedMethods()
	if len(manifest) != 1 {
		t.Fatalf("advertised %d methods", len(manifest))
	}
	method := manifest[0]
	if method.ContractVersion != "1.0.0" || method.Profile != "command" || method.Method != "command.get" || method.InputSchema != "CommandGetRequest" || method.OutputSchema != "CommandGetResponse" {
		t.Fatalf("unexpected support: %+v", method)
	}
}

func TestNegotiationAcceptsExactMethodSchemas(t *testing.T) {
	method := contract.SupportedMethods()[0]
	wire := []byte(`{"contract_version":"1.0.0","profile":"command","method":"command.get","input_schema_digest":"` + string(method.InputSchemaDigest) + `","output_schema_digest":"` + string(method.OutputSchemaDigest) + `"}`)
	got, err := contract.Negotiate(wire)
	if err != nil || got != method {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestNegotiationSharedRefusals(t *testing.T) {
	data, err := os.ReadFile("../fixtures/1.0.0/negotiations.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name  string
		Wire  string
		Valid bool
		Code  contract.ErrorCode
	}
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			got, err := contract.Negotiate([]byte(c.Wire))
			if c.Valid {
				if err != nil || got != contract.SupportedMethods()[0] {
					t.Fatalf("got %+v err %v", got, err)
				}
				return
			}
			var refusal *contract.ContractError
			if !errors.As(err, &refusal) || refusal.Code != c.Code {
				t.Fatalf("want %s got %v", c.Code, err)
			}
		})
	}
}

func TestNegotiationThenAuthenticatedQueryPreservesReceiptAndCurrentProgress(t *testing.T) {
	method := contract.SupportedMethods()[0]
	negotiation, err := contract.Encode(contract.NegotiationRequest{ContractVersion: method.ContractVersion, Profile: method.Profile, Method: method.Method, InputSchemaDigest: method.InputSchemaDigest, OutputSchemaDigest: method.OutputSchemaDigest})
	if err != nil {
		t.Fatal(err)
	}
	supported, err := contract.Negotiate(negotiation)
	if err != nil {
		t.Fatal(err)
	}
	request := contract.CommandGetRequest{ContractVersion: string(supported.ContractVersion), Profile: string(supported.Profile), Method: string(supported.Method), CommandID: "fresh-read", Target: contract.CommandTarget{TenantID: "t", OwnerID: "o", Kind: "command", ID: "original"}, Payload: contract.CommandGetPayload{CommandRef: queryRef}, AcceptBefore: "2026-10-03T01:00:00.000000Z"}
	raw, err := contract.Encode(request)
	if err != nil {
		t.Fatal(err)
	}
	observation := []byte(`{"status":"found","command_ref":{"owner":{"tenant_id":"t","owner_id":"o"},"command_id":"original"},"receipt":{"state":"accepted","command_ref":{"owner":{"tenant_id":"t","owner_id":"o"},"command_id":"original"},"object_ref":{"tenant_id":"t","owner_id":"tasks","kind":"task","id":"task-1"},"revision":"5"},"progress":{"kind":"task","object_ref":{"tenant_id":"t","owner_id":"tasks","kind":"task","id":"task-1"},"revision":"6","status":"succeeded"}}`)
	route := queryDirectory(func(_ context.Context, o contract.OwnerRef) (contract.ResolvedCommandOwner, error) {
		return contract.ResolvedCommandOwner{Owner: o, Reader: queryFacts(func(_ context.Context, _ contract.CommandRef) (contract.CommandGetResponse, error) {
			return contract.DecodeCommandResponse(observation, queryRef)
		})}, nil
	})
	ctx, cancel := queryContext()
	defer cancel()
	result, err := contract.GetCommand(ctx, raw, &querySubject, queryAuth(queryAllowed), route, queryClock)
	if err != nil {
		t.Fatal(err)
	}
	responseWire, err := contract.EncodeCommandResponse(result, queryRef)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := contract.DecodeCommandResponse(responseWire, queryRef)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := decoded.AsFound()
	if !ok {
		t.Fatal("missing observation")
	}
	receipt, ok := found.Receipt.AsAccepted()
	if !ok || receipt.Revision == nil || *receipt.Revision != "5" {
		t.Fatal("acceptance changed")
	}
	progress, ok := found.Progress.AsTask()
	if !ok || progress.Status != "succeeded" || progress.Revision != "6" {
		t.Fatal("current progress lost")
	}
}

// Independent verification of embedded machine metadata, with safe integer
// metadata handled separately from the number-free public wire parser.
func schemaGoldenDigest(root string) (string, error) {
	decoder := json.NewDecoder(strings.NewReader(wire.SchemaJSON))
	decoder.UseNumber()
	var source map[string]any
	if err := decoder.Decode(&source); err != nil {
		return "", err
	}
	defs := source["$defs"].(map[string]any)
	reachable := map[string]any{}
	var visit func(any) error
	visit = func(value any) error {
		switch node := value.(type) {
		case []any:
			for _, v := range node {
				if err := visit(v); err != nil {
					return err
				}
			}
		case map[string]any:
			if r, ok := node["$ref"].(string); ok {
				if !strings.HasPrefix(r, "#/$defs/") {
					return fmt.Errorf("non-local ref")
				}
				name := strings.TrimPrefix(r, "#/$defs/")
				definition, exists := defs[name]
				if !exists {
					return fmt.Errorf("missing ref")
				}
				if _, seen := reachable[name]; !seen {
					reachable[name] = definition
					if err := visit(definition); err != nil {
						return err
					}
				}
			}
			for _, v := range node {
				if err := visit(v); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := visit(map[string]any{"$ref": "#/$defs/" + root}); err != nil {
		return "", err
	}
	bundle := map[string]any{"$schema": source["$schema"], "$id": source["$id"], "$ref": "#/$defs/" + root, "$defs": reachable}
	canonical, err := schemaGoldenCanonical(bundle)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256([]byte("lerna-schema-digest-1\n" + canonical))
	return "sha256:" + hex.EncodeToString(h[:]), nil
}
func schemaGoldenCanonical(value any) (string, error) {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			a, b := utf16.Encode([]rune(keys[i])), utf16.Encode([]rune(keys[j]))
			for x := 0; x < len(a) && x < len(b); x++ {
				if a[x] != b[x] {
					return a[x] < b[x]
				}
			}
			return len(a) < len(b)
		})
		parts := []string{}
		for _, k := range keys {
			key, _ := schemaGoldenCanonical(k)
			item, err := schemaGoldenCanonical(v[k])
			if err != nil {
				return "", err
			}
			parts = append(parts, key+":"+item)
		}
		return "{" + strings.Join(parts, ",") + "}", nil
	case []any:
		parts := []string{}
		for _, item := range v {
			s, err := schemaGoldenCanonical(item)
			if err != nil {
				return "", err
			}
			parts = append(parts, s)
		}
		return "[" + strings.Join(parts, ",") + "]", nil
	case json.Number:
		n, err := strconv.ParseUint(string(v), 10, 64)
		if err != nil || n > 9007199254740991 {
			return "", fmt.Errorf("unsafe schema integer")
		}
		return strconv.FormatUint(n, 10), nil
	default:
		var out strings.Builder
		encoder := json.NewEncoder(&out)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(value); err != nil {
			return "", err
		}
		// Remove Go's mandatory U+2028/U+2029 escapes while preserving a
		// literal backslash-u sequence (a pair of escaped backslashes).
		encoded := strings.TrimSuffix(out.String(), "\n")
		var scalar strings.Builder
		for i := 0; i < len(encoded); {
			if encoded[i] != '\\' {
				scalar.WriteByte(encoded[i])
				i++
				continue
			}
			if i+6 <= len(encoded) && (encoded[i:i+6] == `\u2028` || encoded[i:i+6] == `\u2029`) {
				if encoded[i:i+6] == `\u2028` {
					scalar.WriteRune('\u2028')
				} else {
					scalar.WriteRune('\u2029')
				}
				i += 6
				continue
			}
			scalar.WriteString(encoded[i : i+2])
			i += 2
		}
		return scalar.String(), nil
	}
}
func TestNegotiationSchemaDigestIndependentGoldens(t *testing.T) {
	data, err := os.ReadFile("../fixtures/1.0.0/schema-digests.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct{ Algorithm, Root, Digest string }
	if err = json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	method := contract.SupportedMethods()[0]
	for _, f := range fixtures {
		actual, err := schemaGoldenDigest(f.Root)
		if err != nil {
			t.Fatal(err)
		}
		if f.Algorithm != "lerna-schema-digest-1" || actual != f.Digest {
			t.Fatalf("root %s got %s want %s", f.Root, actual, f.Digest)
		}
		var advertised contract.SchemaDigest
		switch f.Root {
		case "CommandGetRequest":
			advertised = method.InputSchemaDigest
		case "CommandGetResponse":
			advertised = method.OutputSchemaDigest
		default:
			t.Fatal("unknown fixture root")
		}
		if string(advertised) != f.Digest {
			t.Fatal("generated support detached from machine schema")
		}
	}
	copy := contract.SupportedMethods()
	copy[0].Method = "task.submit"
	if reflect.DeepEqual(copy, contract.SupportedMethods()) {
		t.Fatal("caller mutated support")
	}
}

func TestNegotiationDoesNotOpenDesignedBusinessMethods(t *testing.T) {
	ctx, cancel := queryContext()
	defer cancel()
	for _, method := range []string{"task.submit", "memory.query", "delegation.create", "schedule.create", "environment.install"} {
		raw := strings.Replace(string(queryWire(queryRef)), "command.get", method, 1)
		_, err := contract.GetCommand(ctx, []byte(raw), nil, nil, nil, queryClock)
		var rejected *contract.ContractError
		if !errors.As(err, &rejected) || rejected.Code != "unsupported" {
			t.Fatalf("%s: %v", method, err)
		}
	}
}
