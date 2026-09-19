package extractioncheck

import (
	"context"
	"crypto/sha256"
	"fmt"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	executionlocal "lerna/adapters/execution/local"
	extractionauth "lerna/adapters/extraction/auth"
	extractionexecution "lerna/adapters/extraction/execution"
	localextractionsource "lerna/adapters/extraction/localsource"
	localextraction "lerna/adapters/extraction/rules"
	extractionsourceguard "lerna/adapters/extraction/sourceguard"
	"lerna/extraction"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"lerna/sdk"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestObservedChoicesAutomaticallySaveTentativeInference(t *testing.T) {
	ctx := context.Background()
	h, err := fresh(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	refs := installObservedChoices(t, h)
	store, saver := bindProcessSave(t, h, "")
	h.client = sdk.NewCapabilityClient(executionlocal.Bind(h.exec, "local"), "local")
	r, grant, err := h.request(ctx, []byte(`{"sources":[{"kind":"choice","key":"one","revision":1},{"kind":"choice","key":"two","revision":1},{"kind":"choice","key":"three","revision":1}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.client.Invoke(ctx, r, grant); err != nil {
		t.Fatal(err)
	}
	outcome, err := h.exec.Run(ctx, r.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if err = h.exec.Drain(ctx, 16); err != nil {
		t.Fatal(err)
	}
	task, err := h.core.Get(ctx, h.token, r.Qualification.Ref)
	if err != nil || task.State != "COMPLETED" || outcome.Result != "SUCCESS" {
		t.Fatalf("inference task: %s %v", task.State, err)
	}
	state, err := saver.Inspect(ctx, r.OperationID)
	if err != nil || state.State != "committed" || state.Receipt == nil {
		t.Fatalf("inference save: %s %v", state.State, err)
	}
	row, err := store.Read(ctx, state.Receipt.Ref, state.Receipt.Revision)
	if err != nil {
		t.Fatal(err)
	}
	record := new(wire.MemoryRecord)
	if err = protojson.Unmarshal(row.Document, record); err != nil {
		t.Fatal(err)
	}
	s := record.GetSpec()
	if s.GetKind() != "inference" || s.GetAbout() != "operator" || s.GetConditions() != "report" || string(s.GetContent().GetJson()) != `{"attribute":"format","value":"detailed"}` || s.GetConfidence().GetAssessment() != "tentative" || s.GetConfidence().GetBasis() != "三个独立且一致的报告格式选择" || s.GetConfidence().GetMethod() != "local-rules-v1" || len(s.GetSources()) != 3 {
		t.Fatal("wrong inference content, conditions or confidence")
	}
	for i, source := range s.Sources {
		if !proto.Equal(source.Ref, refs[i]) || source.Method != "observed-choice" || source.GetFragment() != "selection" {
			t.Fatal("lost authenticated choice provenance")
		}
	}
	replay, err := saver.Save(ctx, r.OperationID)
	if err != nil || replay.Receipt == nil || *replay.Receipt != *state.Receipt {
		t.Fatalf("inference replay: %v", err)
	}
	changes, err := store.ReadChanges(ctx, "local", "personal", 0, 16)
	if err != nil || len(changes) != 1 {
		t.Fatalf("duplicate inference save: %v", err)
	}
	later, used := readSavedInLaterTask(t, h, store, *state.Receipt, r.Qualification.Ref)
	if later.Ref == r.Qualification.Ref || len(used.Records) != 1 || !proto.Equal(used.Records[0], record) {
		t.Fatal("later task did not read the original saved inference")
	}
}

func installObservedChoices(t *testing.T, h *harness, createFiles ...bool) []*wire.ContentSource {
	t.Helper()
	if len(createFiles) > 1 {
		t.Fatal("one source installation mode required")
	}
	create := len(createFiles) == 0 || createFiles[0]
	var entries []localextractionsource.Entry
	var refs []*wire.ContentSource
	for _, key := range []string{"one", "two", "three"} {
		body := []byte(fmt.Sprintf(`{"event":%q,"task":"report","format":"detailed"}`, key))
		path := filepath.Join(h.root, "choice-"+key+".json")
		if create {
			if err := os.WriteFile(path, body, 0600); err != nil {
				t.Fatal(err)
			}
		}
		ref := &wire.ContentSource{Kind: "choice", Key: key, Revision: 1}
		refs = append(refs, ref)
		entries = append(entries, localextractionsource.Entry{Ref: ref, Path: path, Encoding: "choice", SHA256: fmt.Sprintf("%x", sha256.Sum256(body)), Speaker: "operator", Method: "observed-choice", Fragment: "selection", Restrictions: extraction.Restrictions{Storage: []string{"local"}, Processing: []string{"local"}, Recipients: []string{"local"}, Purposes: []string{"task"}, RetainUntil: h.now().Add(time.Hour).Unix()}})
	}
	source, err := localextractionsource.New(entries, h.clock)
	if err != nil {
		t.Fatal(err)
	}
	h.source, err = extractionsourceguard.New(source, h.candidates, "local")
	if err != nil {
		t.Fatal(err)
	}
	a, err := extractionauth.New(h.auth, extractionauth.Scope{Namespace: "local", Resource: "root", Purpose: "task", Location: "local"})
	if err != nil {
		t.Fatal(err)
	}
	h.target, err = extractionexecution.New(h.candidates, a, h.source, localextraction.Rules{}, h.clock, memory.Binding{Token: h.token, Namespace: "local", Subject: "operator", Location: "local", Recipient: "local"}, "task")
	if err != nil {
		t.Fatal(err)
	}
	return refs
}
