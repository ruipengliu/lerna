package fetchoutput

import (
	"context"
	"encoding/json"
	"google.golang.org/protobuf/proto"
	"lerna/artifacts"
	"lerna/execution"
	"lerna/fetch"
	wire "lerna/gen/harness/v1"
	"lerna/internal/jsonvalue"
	"sync"
)

// FailureReader is bound by the host to the same current Content authority as
// the invocation output. A reference alone grants no disclosure permission.
type FailureReader struct {
	mu         sync.RWMutex
	cache      map[string]cachedFailure
	adapter    *Adapter
	token      string
	capability execution.Capability
}

func (a *Adapter) Failures(token string, capability execution.Capability) *FailureReader {
	return &FailureReader{adapter: a, token: token, capability: capability}
}
func (r *FailureReader) ReadFailure(ctx context.Context, ref string) (fetch.Outcome, error) {
	if out, known, err := r.readCached(ctx, ref); known {
		return out, err
	}
	invalid := artifacts.Error("CONTENT_UNAVAILABLE")
	b := r.adapter.binding
	b.Token = r.token
	b.Recipient = r.capability.Location
	m, err := r.adapter.metadata(ctx, b, ref, r.capability)
	if err != nil {
		return fetch.Outcome{}, err
	}
	if m.Spec.Kind != "artifact" || m.Spec.Size > 32768 {
		return fetch.Outcome{}, invalid
	}
	// Reuse the metadata just checked above rather than issuing a second GET
	// through the general input reader. Each chunk still checks current Content
	// authority and returns the current metadata checked with those bytes.
	raw := []byte{}
	for offset := uint64(0); offset < m.Spec.Size; {
		n := min(uint64(16384), m.Spec.Size-offset)
		chunk, err := r.adapter.content.Call(ctx, b, &wire.ContentRequest{Method: "READ", Ref: m.Ref, Purpose: r.capability.Purpose, Offset: offset, Limit: uint32(n)})
		if err != nil {
			return fetch.Outcome{}, err
		}
		if uint64(len(chunk.GetData())) != n || chunk.GetRecord().GetState() != "available" || !proto.Equal(chunk.GetRecord().GetRef(), m.Ref) || !proto.Equal(chunk.GetRecord().GetSpec(), m.Spec) {
			return fetch.Outcome{}, invalid
		}
		raw = append(raw, chunk.Data...)
		offset += n
	}
	value, err := jsonvalue.Decode(raw)
	envelope, ok := value.(map[string]any)
	if err != nil || !ok || len(envelope) != 2 {
		return fetch.Outcome{}, invalid
	}
	output, ok := envelope["output"].(map[string]any)
	if !ok || len(output) != 2 || output["reference"] != "" {
		return fetch.Outcome{}, invalid
	}
	status, ok := output["status"].(string)
	if !ok || !fetch.IsFailureStatus(status) {
		return fetch.Outcome{}, invalid
	}
	evidence, ok := envelope["evidence"].(map[string]any)
	if !ok || len(evidence) < 2 || len(evidence) > 3 || evidence["status"] != status {
		return fetch.Outcome{}, invalid
	}
	mode := ""
	for key := range evidence {
		if key != "mode" && key != "status" && key != "requests" {
			return fetch.Outcome{}, invalid
		}
	}
	if value, exists := evidence["mode"]; exists {
		mode, ok = value.(string)
		if !ok || (mode != "http" && mode != "fixed-replay") {
			return fetch.Outcome{}, invalid
		}
	}
	number, ok := evidence["requests"].(json.Number)
	if !ok {
		return fetch.Outcome{}, invalid
	}
	count, err := number.Int64()
	if err != nil || count < 0 || count > 5 || (mode == "fixed-replay" && count != 0) {
		return fetch.Outcome{}, invalid
	}
	// The final READ already checked current process/disclose authority and
	// metadata with the complete stored-content digest. Only bounded local
	// parsing follows it; a discovery-only GET would duplicate that observation.
	out := fetch.Outcome{Status: status, Mode: mode, Requests: uint32(count)}
	r.remember(ref, cachedFailure{outcome: out, sha: m.Spec.Sha256, size: m.Spec.Size})
	return out, nil
}
