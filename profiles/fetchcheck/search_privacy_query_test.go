package fetchcheck

import (
	"context"
	"fmt"
	"lerna/adapters/executionlocal"
	"lerna/adapters/searchprivacy"
	"lerna/artifacts"
	"lerna/execution"
	"lerna/sdk"
	"lerna/tasks"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// This delegate adds actual source checks at the admitted driver boundary;
// Core, Content, acquisition and SDK execution remain their real implementations.
type privacyCheckedDriver struct {
	execution.Driver
	privacy     *searchprivacy.Guard
	rejectFirst bool
}

func (d privacyCheckedDriver) Start(ctx context.Context, call execution.Call) error {
	if d.rejectFirst {
		rejected := call
		rejected.Input = []byte(`{"different":"input"}`)
		if err := d.privacy.Check(ctx, rejected); err == nil {
			return fmt.Errorf("privacy accepted different input")
		}
	}
	for range 2 {
		if err := d.privacy.Check(ctx, call); err != nil {
			return err
		}
	}
	return d.Driver.Start(ctx, call)
}

func TestExecutionSearchPrivacyReadsConsumeOriginalQueries(t *testing.T) {
	t.Run("successful_checks", func(t *testing.T) { checkSearchPrivacyQueries(t, false) })
	t.Run("rejected_read_is_charged", func(t *testing.T) { checkSearchPrivacyQueries(t, true) })
}

func checkSearchPrivacyQueries(t *testing.T, rejectFirst bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Actual evidence."))
	}))
	defer server.Close()
	h, err := fresh(ctx, []string{server.URL + "/start", server.URL + "/final"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	observed := &observedFailureContent{content: h.content}
	var privacy *searchprivacy.Guard
	run, _ := prepareActionAnswerProcess(t, ctx, h, func(port *tasks.ActionPort) {
		privacy, err = searchprivacy.New(observed, artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}, h.clock, h.cap, "local")
		if err != nil {
			t.Fatal(err)
		}
		privacy, err = privacy.WithQueries(port)
		if err != nil {
			t.Fatal(err)
		}
		content, err := h.access.WithQueries(port)
		if err != nil {
			t.Fatal(err)
		}
		h.exec, err = execution.New(h.grants, h.work, content, privacyCheckedDriver{Driver: h.target, privacy: privacy, rejectFirst: rejectFirst}, h.binding, h.cap, config(), h.operation)
		if err != nil {
			t.Fatal(err)
		}
		h.client = sdk.NewCapabilityClient(executionlocal.Bind(h.exec, "local"), "local")
	})
	reads := 0
	for _, q := range run.Actions.Queries {
		if strings.HasPrefix(q, "content/") {
			reads++
		}
	}
	// Four execution observations plus the first privacy GET/READ and a fresh
	// second privacy READ at the same processing/disclosure location.
	expected := 7
	if rejectFirst {
		expected = 8
	} // Rejected GET/READ is charged; later successful reads still perform I/O.
	if reads != expected {
		t.Fatalf("privacy observations absent from original quota: got %d, want %d", reads, expected)
	}
	record, err := h.exec.GetInvocation(ctx, run.Actions.Actions[0].OperationID)
	if err != nil {
		t.Fatal(err)
	}
	before := observed.calls.Load()
	if err = privacy.Check(ctx, execution.Call{Request: record.Request, Input: []byte("{}")}); err == nil {
		t.Fatal("settled action accepted a new privacy read")
	}
	if observed.calls.Load() != before {
		t.Fatal("stale action reached Content before qualification rejection")
	}
	after, err := h.core.Load(ctx, run.Task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Actions.Queries) != len(run.Actions.Queries) {
		t.Fatal("stale action charged current worker")
	}
}
