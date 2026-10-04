package component_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	contract "github.com/ruipengliu/lerna/contract/v1_1"
)

type query11Auth func(context.Context, contract.SubjectBinding, contract.CommandRef) (bool, error)

func (f query11Auth) AuthorizeCommandRead(c context.Context, s contract.SubjectBinding, r contract.CommandRef) (bool, error) {
	return f(c, s, r)
}

type query11Directory func(context.Context, contract.OwnerRef) (contract.ResolvedCommandOwner, error)

func (f query11Directory) ResolveCommandOwner(c context.Context, o contract.OwnerRef) (contract.ResolvedCommandOwner, error) {
	return f(c, o)
}

type query11Facts func(context.Context, contract.CommandRef) (contract.CommandGetResponse, error)

func (f query11Facts) ReadCommand(c context.Context, r contract.CommandRef) (contract.CommandGetResponse, error) {
	return f(c, r)
}

var query11Ref = contract.CommandRef{Owner: contract.OwnerRef{TenantID: "t", OwnerID: "o"}, CommandID: "original"}
var query11Subject = contract.SubjectBinding{TenantID: "t", SubjectID: "allowed", DelegationChain: []contract.DelegatedSubject{}}

func query11Wire(ref contract.CommandRef) []byte {
	data, _ := json.Marshal(contract.CommandGetRequest{ContractVersion: "1.1.0", Profile: "command", Method: "command.get", CommandID: "read", Target: contract.CommandTarget{TenantID: ref.Owner.TenantID, OwnerID: ref.Owner.OwnerID, Kind: "command", ID: ref.CommandID}, Payload: contract.CommandGetPayload{CommandRef: ref}, AcceptBefore: "2026-10-03T01:00:00.000000Z"})
	return data
}
func query11Clock() time.Time { return time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC) }
func query11Context() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), time.Minute)
}
func query11Allowed(_ context.Context, s contract.SubjectBinding, r contract.CommandRef) (bool, error) {
	return s.SubjectID == "allowed" && r.Owner == query11Ref.Owner, nil
}
func query11Result(t *testing.T, result contract.CommandGetResponse, err error, want string) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	wire, err := contract.Encode(result)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := contract.ParseJSON(wire)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := contract.ParseJSON([]byte(want))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("got %s want %s", wire, want)
	}
}

type authenticatedQuery11Fixture struct {
	Name          string                   `json:"name"`
	Request       string                   `json:"request"`
	Trusted       *contract.SubjectBinding `json:"trusted_subject"`
	Authorization string                   `json:"authorization"`
	Directory     string                   `json:"directory"`
	Observation   json.RawMessage          `json:"observation"`
	Expected      json.RawMessage          `json:"expected"`
	Error         contract.ErrorCode       `json:"error"`
}

func TestAuthenticatedQuery11SharedFixtures(t *testing.T) {
	data, err := os.ReadFile("../fixtures/1.1.0/queries.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []authenticatedQuery11Fixture
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range cases {
		t.Run(fixture.Name, func(t *testing.T) {
			ctx, cancel := query11Context()
			defer cancel()
			auth := query11Auth(func(_ context.Context, s contract.SubjectBinding, r contract.CommandRef) (bool, error) {
				if fixture.Authorization == "failure" {
					return false, errors.New("SECRET authentication infrastructure")
				}
				delegated := len(s.DelegationChain) == 0 || len(s.DelegationChain) == 1 && s.DelegationChain[0] == (contract.DelegatedSubject{TenantID: "t", SubjectID: "delegator"})
				return s.TenantID == "t" && s.SubjectID == "allowed" && delegated && r.Owner == query11Ref.Owner && (r.CommandID == "original" || r.CommandID == "absent"), nil
			})
			directory := query11Directory(func(_ context.Context, o contract.OwnerRef) (contract.ResolvedCommandOwner, error) {
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
				var reader contract.CommandFactReader = query11Facts(func(_ context.Context, r contract.CommandRef) (contract.CommandGetResponse, error) {
					return contract.DecodeCommandResponse(fixture.Observation, r)
				})
				if fixture.Directory == "missing_reader" {
					reader = nil
				}
				return contract.ResolvedCommandOwner{Owner: returned, Reader: reader}, nil
			})
			result, err := contract.GetCommand(ctx, []byte(fixture.Request), fixture.Trusted, auth, directory, query11Clock)
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
