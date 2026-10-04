//go:build integration

package decisionfixture

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres"
	decisionpg "github.com/ruipengliu/lerna/adapters/postgres/decision_engine"
	decision "github.com/ruipengliu/lerna/components/decision_engine"
	v "github.com/ruipengliu/lerna/contract/v1_1"
	"github.com/ruipengliu/lerna/runtime"
)

//go:embed testdata/final01_proposal_writer_test.go.txt
var final01ProposalDriver []byte

type final01ProposalOutput struct {
	Key       string         `json:"key"`
	Ref       v.ContentRef   `json:"ref"`
	Body      string         `json:"body"`
	Sources   []v.ContentRef `json:"sources"`
	Published bool           `json:"published"`
}
type final01ProposalCase struct {
	Name              string                  `json:"name"`
	SourceSchema      string                  `json:"source_schema"`
	DecisionSchema    string                  `json:"decision_schema"`
	SourceOwner       v.OwnerRef              `json:"source_owner"`
	Request           json.RawMessage         `json:"request"`
	Subject           json.RawMessage         `json:"subject"`
	GetRequest        json.RawMessage         `json:"get_request"`
	CommandGetRequest json.RawMessage         `json:"command_get_request"`
	Receipt           json.RawMessage         `json:"receipt"`
	DecisionBefore    json.RawMessage         `json:"decision_before"`
	LeaseUntil        time.Time               `json:"lease_until"`
	Outputs           []final01ProposalOutput `json:"outputs"`
	WritersDrained    bool                    `json:"writers_drained"`
}
type final01ProposalReady struct {
	Protocol string                `json:"protocol"`
	Cases    []final01ProposalCase `json:"cases"`
}

func TestFinal01PublicPreparedProposalUpgrade(t *testing.T) {
	session := startFrozenWriter(t, restoreFinal01Writer(t, final01ProposalDriver), "TestFrozenFinal01ProposalWriter")
	var ready final01ProposalReady
	decoder := json.NewDecoder(bytes.NewReader(session.Frame))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&ready); err != nil {
		t.Fatal("original proposal frame", err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) || ready.Protocol != "lerna-final01-proposal-upgrade-1" || len(ready.Cases) != 6 {
		t.Fatal("original proposal protocol or bound")
	}
	ack, err := readUpgradeBounded(session.Registry, 4096)
	if err != nil {
		t.Fatal("original successful CREATE acknowledgments", err)
	}
	owned := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(ack)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != "postgres" || !testIdentifier.MatchString(fields[1]) || owned[fields[1]] {
			t.Fatal("original exact acknowledgment")
		}
		owned[fields[1]] = true
	}
	if len(owned) != 12 {
		t.Fatal("original six pairs were not all acknowledged")
	}
	// Preserve the original producer's successful acknowledgments in this
	// consumer's durable ledger before the mechanical helper removes its scope.
	// This is exact ownership handoff, not discovery by schema prefix.
	for schema := range owned {
		if err := RegisterOwnedSchema(schema); err != nil {
			t.Fatal("original scope acknowledgment handoff", err)
		}
	}
	seen := map[string]bool{}
	used := map[string]bool{}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	holders := &final01ProposalHolders{session: session}
	for _, old := range ready.Cases {
		switch old.Name {
		case "accepted", "running", "prepared_waiting", "publication_running", "completed", "failed":
		default:
			t.Fatal("unknown original state")
		}
		if seen[old.Name] || !old.WritersDrained || !owned[old.SourceSchema] || !owned[old.DecisionSchema] || old.SourceSchema == old.DecisionSchema || used[old.SourceSchema] || used[old.DecisionSchema] {
			t.Fatal("original state ownership or actual writer closure")
		}
		seen[old.Name] = true
		used[old.SourceSchema], used[old.DecisionSchema] = true, true
		t.Run(old.Name, func(t *testing.T) { verifyFinal01ProposalUpgrade(t, ctx, old, holders) })
	}
}

// The holder counter controls only cleanup authorization. Business assertions
// below consume public Component facts and independently published Source bytes.
type final01ProposalHolders struct {
	live    int
	failed  bool
	session *frozenWriterSession
}

func (h *final01ProposalHolders) own(t *testing.T, close func() error) func() {
	h.live++
	h.session.CurrentClosed = false
	active := true
	finish := func() {
		if !active {
			return
		}
		if err := close(); err != nil {
			h.failed = true
			h.session.CurrentClosed = false
			t.Error(upgradeCause("current proposal holder close", err))
			return
		}
		active = false
		h.live--
		h.session.CurrentClosed = h.live == 0 && !h.failed
	}
	t.Cleanup(finish)
	return finish
}

func verifyFinal01ProposalUpgrade(t *testing.T, ctx context.Context, old final01ProposalCase, holders *final01ProposalHolders) {
	t.Helper()
	subject, err := v.Decode[v.SubjectBinding](old.Subject)
	if err != nil {
		t.Fatal(err)
	}
	request, err := v.Decode[v.DecisionDecideRequest](old.Request)
	if err != nil {
		t.Fatal(err)
	}
	before, err := v.Decode[v.Decision](old.DecisionBefore)
	if err != nil {
		t.Fatal(err)
	}
	open := func() (*decision.Service, *Store, func()) {
		cfg := postgres.Config{DSN: os.Getenv("LERNA_TEST_POSTGRES_DSN"), Schema: old.DecisionSchema, MaxOpenConnections: 4, TransactionTimeout: 3 * time.Second, StatementTimeout: 2 * time.Second, LockTimeout: time.Second}
		store, err := decisionpg.Open(ctx, cfg)
		var closeStore, closeSource func()
		if store != nil {
			closeStore = holders.own(t, store.Close)
		}
		if err != nil {
			t.Fatal(upgradeCause("current Decision open", err))
		}
		if err = store.Migrate(ctx); err != nil {
			t.Fatal(upgradeCause("current Decision migrate", err))
		}
		cfg.Schema = old.SourceSchema
		source, err := Open(ctx, cfg, old.SourceOwner)
		if source != nil {
			closeSource = holders.own(t, source.Close)
		}
		if err != nil {
			t.Fatal(upgradeCause("current Source open", err))
		}
		if err = source.Migrate(ctx); err != nil {
			t.Fatal(upgradeCause("current Source migrate", err))
		}
		service, err := decision.New(decision.Config{Owner: v.OwnerRef{TenantID: request.Target.TenantID, OwnerID: request.Target.OwnerID}, Store: store, Source: source, Authority: source, Publisher: source, Component: request.Payload.ComponentRef, Worker: "current-final01-proposal-consumer", Lease: time.Second, PoolControl: true})
		if err != nil {
			t.Fatal(err)
		}
		return service, source, func() { closeStore(); closeSource() }
	}
	get := func(service *decision.Service) v.Decision {
		view, err := service.Get(ctx, old.GetRequest, &subject)
		if err != nil {
			t.Fatal(err)
		}
		found, ok := view.AsFound()
		if !ok {
			t.Fatal("original public Decision unavailable")
		}
		return found.Decision
	}
	service, source, closeFirst := open()
	initial := get(service)
	if !reflect.DeepEqual(initial, before) {
		t.Fatal("migration changed original public Decision")
	}
	permit, err := source.Authorize(ctx, subject, request.Target, "get", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, output := range old.Outputs {
		body, err := source.ReadPublished(ctx, output.Ref, permit)
		if output.Published {
			if err != nil || string(body) != output.Body {
				t.Fatal("original committed publication changed", err)
			}
		} else if !errors.Is(err, decision.ErrForbidden) {
			t.Fatal("original unpublished plan was presented as committed", err)
		}
	}
	wake := old.LeaseUntil
	if waiting, ok := initial.AsWaiting(); ok {
		wake, err = time.Parse(time.RFC3339Nano, string(waiting.WakeAt))
		if err != nil {
			t.Fatal(err)
		}
	}
	if !wake.IsZero() && wake.After(time.Now()) {
		if err := (runtime.WallTimer{}).Wait(ctx, time.Until(wake)+time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	step, err := service.Step(ctx)
	if err != nil {
		t.Fatal("current bounded upgrade step", err)
	}
	terminal := old.Name == "completed" || old.Name == "failed"
	if terminal && step.Processed != 0 || !terminal && step.Processed != 1 {
		t.Fatal("original durable responsibility lost or terminal job revived")
	}
	after := get(service)
	if terminal && !reflect.DeepEqual(before, after) {
		t.Fatal("original terminal fact changed")
	}
	if old.Name != "failed" {
		completed, ok := after.AsCompleted()
		if !ok || !reflect.DeepEqual(completed.Input, request.Payload) || len(completed.ArtifactRefs) != 1 {
			t.Fatal("original candidate no longer completed its exact input")
		}
		starts, measured := v.Revision("1"), true
		if old.Name == "running" {
			starts, measured = "2", false
		}
		if completed.Usage.RuleStarts != starts || completed.Usage.Cost.IntegerValue != starts || completed.Usage.RuleSteps != "1" || completed.Usage.ModelRequests != "0" || completed.Usage.MeasurementsComplete != measured {
			t.Fatal("upgrade reset original conservative allowance/fee", completed.Usage)
		}
		var originalFields, completedFields map[string]json.RawMessage
		originalBytes, _ := v.Encode(before)
		completedBytes, _ := v.Encode(after)
		if err := json.Unmarshal(originalBytes, &originalFields); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(completedBytes, &completedFields); err != nil {
			t.Fatal(err)
		}
		if !sameUpgradeJSON(originalFields["input_digest"], completedFields["input_digest"]) {
			t.Fatal("upgrade rebound original input digest")
		}
		if old.Name == "prepared_waiting" || old.Name == "publication_running" || old.Name == "completed" {
			if len(old.Outputs) != 2 || !sameUpgradeJSON(originalFields["usage"], completedFields["usage"]) {
				t.Fatal("saved original prepared output was recalculated or rebilled")
			}
			for _, output := range old.Outputs {
				ref, err := source.PlanPublication(ctx, output.Key, []byte(output.Body), output.Sources, permit)
				if err != nil || ref != output.Ref {
					t.Fatal("original key/bytes/sources no longer retain original identity", err)
				}
				body, err := source.ReadPublished(ctx, output.Ref, permit)
				if err != nil || string(body) != output.Body {
					t.Fatal("old prepared original publication unreadable", err)
				}
				if strings.HasSuffix(output.Key, "/artifact") {
					if completed.ArtifactRefs[0] != output.Ref {
						t.Fatal("old artifact identity changed")
					}
				} else if completed.ProposalRef != output.Ref {
					t.Fatal("old Proposal identity changed")
				}
			}
		}
		proposalBody, err := source.ReadPublished(ctx, completed.ProposalRef, permit)
		if err != nil {
			t.Fatal(err)
		}
		proposal, err := v.Decode[v.Proposal](proposalBody)
		if err != nil || !reflect.DeepEqual(proposal, completed.Proposal) {
			t.Fatal("original Proposal body differs from completed public fact", err)
		}
	}
	closeFirst()
	service, _, closeSecond := open()
	if !reflect.DeepEqual(get(service), after) {
		t.Fatal("new owner reopen lost upgraded terminal")
	}
	command, err := service.GetCommand(ctx, old.CommandGetRequest, &subject)
	if err != nil {
		t.Fatal(err)
	}
	fixed, ok := command.AsFound()
	if !ok {
		t.Fatal("original accepted command unavailable")
	}
	receipt, err := v.Encode(fixed.Receipt)
	if err != nil || !sameUpgradeJSON(receipt, old.Receipt) {
		t.Fatal("upgrade changed original accepted receipt", err)
	}
	replayed, err := service.Decide(ctx, old.Request, &subject)
	if err != nil {
		t.Fatal(err)
	}
	received, ok := replayed.AsReceived()
	if !ok {
		t.Fatal("original replay unavailable")
	}
	receipt, err = v.Encode(received.Receipt)
	if err != nil || !sameUpgradeJSON(receipt, old.Receipt) {
		t.Fatal("replay invented another command receipt", err)
	}
	if step, err := service.Step(ctx); err != nil || step.Processed != 0 {
		t.Fatal("upgrade retained runnable work or charged new repair", err)
	}
	closeSecond()
}
