package component_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/contract"
)

type queryAuth func(context.Context, contract.SubjectBinding, contract.CommandRef) (bool, error)

func (f queryAuth) AuthorizeCommandRead(c context.Context, s contract.SubjectBinding, r contract.CommandRef) (bool, error) {
	return f(c, s, r)
}

type queryDirectory func(context.Context, contract.OwnerRef) (contract.ResolvedCommandOwner, error)

func (f queryDirectory) ResolveCommandOwner(c context.Context, o contract.OwnerRef) (contract.ResolvedCommandOwner, error) {
	return f(c, o)
}

type queryFacts func(context.Context, contract.CommandRef) (contract.CommandGetResponse, error)

func (f queryFacts) ReadCommand(c context.Context, r contract.CommandRef) (contract.CommandGetResponse, error) {
	return f(c, r)
}

var queryRef = contract.CommandRef{Owner: contract.OwnerRef{TenantID: "t", OwnerID: "o"}, CommandID: "original"}
var querySubject = contract.SubjectBinding{TenantID: "t", SubjectID: "allowed", DelegationChain: []contract.DelegatedSubject{}}

func queryWire(ref contract.CommandRef) []byte {
	data, _ := json.Marshal(contract.CommandGetRequest{ContractVersion: "1.0.0", Profile: "command", Method: "command.get", CommandID: "read", Target: contract.CommandTarget{TenantID: ref.Owner.TenantID, OwnerID: ref.Owner.OwnerID, Kind: "command", ID: ref.CommandID}, Payload: contract.CommandGetPayload{CommandRef: ref}, AcceptBefore: "2026-10-03T01:00:00.000000Z"})
	return data
}
func queryClock() time.Time { return time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC) }
func queryContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), time.Minute)
}
func queryAllowed(_ context.Context, s contract.SubjectBinding, r contract.CommandRef) (bool, error) {
	return s.SubjectID == "allowed" && r.Owner == queryRef.Owner, nil
}
func queryResult(t *testing.T, result contract.CommandGetResponse, err error, want string) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	wire, err := contract.Encode(result)
	if err != nil {
		t.Fatal(err)
	}
	if string(wire) != want {
		t.Fatalf("got %s want %s", wire, want)
	}
}

func TestAuthenticatedQueryOpaqueRefusalAndNormalControl(t *testing.T) {
	ctx, cancel := queryContext()
	defer cancel()
	for _, exists := range []bool{true, false} {
		directory := queryDirectory(func(_ context.Context, o contract.OwnerRef) (contract.ResolvedCommandOwner, error) {
			return contract.ResolvedCommandOwner{Owner: o, Reader: queryFacts(func(_ context.Context, r contract.CommandRef) (contract.CommandGetResponse, error) {
				if exists {
					return contract.NewCommandGetResponseGone(contract.CommandGetResponseGone{CommandRef: r}), nil
				}
				return contract.NewCommandGetResponseNotFound(contract.CommandGetResponseNotFound{CommandRef: r}), nil
			})}, nil
		})
		allowed, err := contract.GetCommand(ctx, queryWire(queryRef), &querySubject, queryAuth(queryAllowed), directory, queryClock)
		expected := `{"status":"not_found","command_ref":{"owner":{"tenant_id":"t","owner_id":"o"},"command_id":"original"}}`
		if exists {
			expected = `{"status":"gone","command_ref":{"owner":{"tenant_id":"t","owner_id":"o"},"command_id":"original"}}`
		}
		queryResult(t, allowed, err, expected)
		denied := querySubject
		denied.SubjectID = "denied"
		result, err := contract.GetCommand(ctx, queryWire(queryRef), &denied, queryAuth(queryAllowed), directory, queryClock)
		queryResult(t, result, err, `{"status":"rejected","reason":"forbidden"}`)
		missing, err := contract.GetCommand(ctx, queryWire(queryRef), nil, queryAuth(queryAllowed), directory, queryClock)
		queryResult(t, missing, err, `{"status":"rejected","reason":"forbidden"}`)
	}
}

func TestAuthenticatedQuerySnapshotsCallerWireAndTrustedBinding(t *testing.T) {
	ctx, cancel := queryContext()
	defer cancel()
	data := queryWire(queryRef)
	subject := contract.SubjectBinding{TenantID: "t", SubjectID: "allowed", DelegationChain: []contract.DelegatedSubject{{TenantID: "t", SubjectID: "delegator"}}}
	originalSubject, _ := contract.Encode(subject)
	auth := queryAuth(func(_ context.Context, s contract.SubjectBinding, r contract.CommandRef) (bool, error) {
		changed := queryRef
		changed.Owner.OwnerID = "p"
		copy(data, queryWire(changed))
		s.DelegationChain[0].SubjectID = "mutated"
		return true, nil
	})
	directory := queryDirectory(func(_ context.Context, o contract.OwnerRef) (contract.ResolvedCommandOwner, error) {
		return contract.ResolvedCommandOwner{Owner: o, Reader: queryFacts(func(_ context.Context, r contract.CommandRef) (contract.CommandGetResponse, error) {
			return contract.NewCommandGetResponseGone(contract.CommandGetResponseGone{CommandRef: r}), nil
		})}, nil
	})
	result, err := contract.GetCommand(ctx, data, &subject, auth, directory, queryClock)
	queryResult(t, result, err, `{"status":"gone","command_ref":{"owner":{"tenant_id":"t","owner_id":"o"},"command_id":"original"}}`)
	after, _ := contract.Encode(subject)
	if string(after) != string(originalSubject) {
		t.Fatalf("trusted binding changed %s", after)
	}
}

type authenticatedQueryFixture struct {
	Name          string                   `json:"name"`
	Request       string                   `json:"request"`
	Trusted       *contract.SubjectBinding `json:"trusted_subject"`
	Authorization string                   `json:"authorization"`
	Directory     string                   `json:"directory"`
	Observation   json.RawMessage          `json:"observation"`
	Expected      json.RawMessage          `json:"expected"`
	Error         contract.ErrorCode       `json:"error"`
}

func TestAuthenticatedQuerySharedFixtures(t *testing.T) {
	data, err := os.ReadFile("../fixtures/1.0.0/queries.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []authenticatedQueryFixture
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range cases {
		t.Run(fixture.Name, func(t *testing.T) {
			ctx, cancel := queryContext()
			defer cancel()
			auth := queryAuth(func(_ context.Context, s contract.SubjectBinding, r contract.CommandRef) (bool, error) {
				if fixture.Authorization == "failure" {
					return false, errors.New("SECRET authentication infrastructure")
				}
				delegated := len(s.DelegationChain) == 0 || len(s.DelegationChain) == 1 && s.DelegationChain[0] == (contract.DelegatedSubject{TenantID: "t", SubjectID: "delegator"})
				return s.TenantID == "t" && s.SubjectID == "allowed" && delegated && r.Owner == queryRef.Owner && (r.CommandID == "original" || r.CommandID == "absent"), nil
			})
			directory := queryDirectory(func(_ context.Context, o contract.OwnerRef) (contract.ResolvedCommandOwner, error) {
				if fixture.Directory == "failure" {
					return contract.ResolvedCommandOwner{}, errors.New("SECRET directory")
				}
				returned := o
				if fixture.Directory == "wrong_owner" {
					returned.OwnerID = "other"
				}
				if fixture.Directory == "wrong_tenant" {
					returned.TenantID = "other"
				}
				var reader contract.CommandFactReader = queryFacts(func(_ context.Context, r contract.CommandRef) (contract.CommandGetResponse, error) {
					return contract.DecodeCommandResponse(fixture.Observation, r)
				})
				if fixture.Directory == "missing_reader" {
					reader = nil
				}
				return contract.ResolvedCommandOwner{Owner: returned, Reader: reader}, nil
			})
			result, err := contract.GetCommand(ctx, []byte(fixture.Request), fixture.Trusted, auth, directory, queryClock)
			if fixture.Error != "" {
				var refusal *contract.ContractError
				if !errors.As(err, &refusal) || refusal.Code != fixture.Error {
					t.Fatalf("expected %s got %v", fixture.Error, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			actual, err := contract.Encode(result)
			if err != nil {
				t.Fatal(err)
			}
			var got, want interface{}
			if err = json.Unmarshal(actual, &got); err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(fixture.Expected, &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %s expected %s", actual, fixture.Expected)
			}
		})
	}
}

func TestAuthenticatedQueryCancellationIsAReadFaultNotAuthorizationRefusal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ctx, deadlineCancel := context.WithTimeout(ctx, time.Minute)
	defer deadlineCancel()
	auth := queryAuth(func(c context.Context, _ contract.SubjectBinding, _ contract.CommandRef) (bool, error) {
		cancel()
		<-c.Done()
		return false, c.Err()
	})
	result, err := contract.GetCommand(ctx, queryWire(queryRef), &querySubject, auth, nil, queryClock)
	queryResult(t, result, err, `{"status":"unavailable","command_ref":{"owner":{"tenant_id":"t","owner_id":"o"},"command_id":"original"},"reason":"dependency_unavailable"}`)
}

func TestAuthenticatedQueryFixedOwnerSurvivesDirectoryAndDefaultChangesReadOnly(t *testing.T) {
	ctx, cancel := queryContext()
	defer cancel()
	fixed := contract.NewCommandReceiptAccepted(contract.CommandReceiptAccepted{CommandRef: queryRef, ObjectRef: contract.ObjectRef{TenantID: "t", OwnerID: "tasks", Kind: "task", ID: "task-1"}})
	store := struct {
		Receipt          contract.CommandReceipt
		OriginalDeadline string
		Jobs             []string
		Effects          []string
	}{fixed, "2025-01-01T00:00:00.000000Z", []string{"already-existing"}, []string{"unknown"}}
	before, _ := json.Marshal(store)
	physical := "old-process"
	defaultOwner := contract.OwnerRef{TenantID: "t", OwnerID: "o"}
	available := true
	routes := map[string]contract.CommandFactReader{}
	for _, address := range []string{"old-process", "replacement-process"} {
		routes[address] = queryFacts(func(_ context.Context, r contract.CommandRef) (contract.CommandGetResponse, error) {
			return contract.NewCommandGetResponseFound(contract.CommandGetResponseFound{CommandRef: r, Receipt: store.Receipt, Progress: contract.NewCommandProgressNone(contract.CommandProgressNone{})}), nil
		})
	}
	directory := queryDirectory(func(_ context.Context, o contract.OwnerRef) (contract.ResolvedCommandOwner, error) {
		if o == defaultOwner && o != queryRef.Owner {
			return contract.ResolvedCommandOwner{Owner: o, Reader: queryFacts(func(_ context.Context, r contract.CommandRef) (contract.CommandGetResponse, error) {
				return contract.NewCommandGetResponseNotFound(contract.CommandGetResponseNotFound{CommandRef: r}), nil
			})}, nil
		}
		if o != queryRef.Owner || !available {
			return contract.ResolvedCommandOwner{}, errors.New("owner offline")
		}
		return contract.ResolvedCommandOwner{Owner: o, Reader: routes[physical]}, nil
	})
	for _, changed := range []bool{false, true} {
		if changed {
			physical = "replacement-process"
			defaultOwner = contract.OwnerRef{TenantID: "t", OwnerID: "new-default"}
		}
		got, err := contract.GetCommand(ctx, queryWire(queryRef), &querySubject, queryAuth(queryAllowed), directory, queryClock)
		if err != nil {
			t.Fatal(err)
		}
		found, ok := got.AsFound()
		if !ok || found.CommandRef != queryRef {
			t.Fatal("original identity lost")
		}
		receipt, _ := contract.Encode(found.Receipt)
		original, _ := contract.Encode(fixed)
		if string(receipt) != string(original) {
			t.Fatal("fixed receipt changed")
		}
	}
	// A fresh read chosen from the new host default resolves a different owner;
	// the old original read above still stayed with its fixed owner.
	fresh := queryRef
	fresh.Owner = defaultOwner
	freshAuth := queryAuth(func(_ context.Context, s contract.SubjectBinding, r contract.CommandRef) (bool, error) {
		return s.SubjectID == "allowed" && r == fresh, nil
	})
	freshResult, freshErr := contract.GetCommand(ctx, queryWire(fresh), &querySubject, freshAuth, directory, queryClock)
	queryResult(t, freshResult, freshErr, `{"status":"not_found","command_ref":{"owner":{"tenant_id":"t","owner_id":"new-default"},"command_id":"original"}}`)
	available = false
	fault, err := contract.GetCommand(ctx, queryWire(queryRef), &querySubject, queryAuth(queryAllowed), directory, queryClock)
	queryResult(t, fault, err, `{"status":"unavailable","command_ref":{"owner":{"tenant_id":"t","owner_id":"o"},"command_id":"original"},"reason":"dependency_unavailable"}`)
	after, _ := json.Marshal(store)
	if string(before) != string(after) {
		t.Fatalf("query changed public facts: %s", after)
	}
}

func TestAuthenticatedQueryRequiresFiniteContextAndKeepsStartCutoff(t *testing.T) {
	directory := queryDirectory(func(_ context.Context, o contract.OwnerRef) (contract.ResolvedCommandOwner, error) {
		return contract.ResolvedCommandOwner{Owner: o, Reader: queryFacts(func(c context.Context, r contract.CommandRef) (contract.CommandGetResponse, error) {
			if _, ok := c.Deadline(); !ok {
				t.Fatal("provider had unbounded context")
			}
			return contract.NewCommandGetResponseGone(contract.CommandGetResponseGone{CommandRef: r}), nil
		})}, nil
	})
	unavailable := `{"status":"unavailable","command_ref":{"owner":{"tenant_id":"t","owner_id":"o"},"command_id":"original"},"reason":"dependency_unavailable"}`
	unbounded, err := contract.GetCommand(context.Background(), queryWire(queryRef), &querySubject, queryAuth(queryAllowed), directory, queryClock)
	queryResult(t, unbounded, err, unavailable)
	canceled, cancel := queryContext()
	cancel()
	fault, err := contract.GetCommand(canceled, queryWire(queryRef), &querySubject, queryAuth(queryAllowed), directory, queryClock)
	queryResult(t, fault, err, unavailable)
	ctx, finish := queryContext()
	defer finish()
	instant := queryClock()
	auth := queryAuth(func(c context.Context, s contract.SubjectBinding, r contract.CommandRef) (bool, error) {
		instant = instant.Add(2 * time.Hour)
		return queryAllowed(c, s, r)
	})
	result, err := contract.GetCommand(ctx, queryWire(queryRef), &querySubject, auth, directory, func() time.Time { return instant })
	queryResult(t, result, err, `{"status":"gone","command_ref":{"owner":{"tenant_id":"t","owner_id":"o"},"command_id":"original"}}`)
}
