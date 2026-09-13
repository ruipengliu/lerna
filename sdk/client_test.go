package sdk_test

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/protobuf/proto"
	"lerna/contractfixture"
	wire "lerna/gen/harness/v1"
	"lerna/protocol"
	"lerna/schema"
	"lerna/sdk"
)

func TestTaskRoundTrip(t *testing.T) {
	client := sdk.NewClient(contractfixture.New(nil), nil)
	response, err := client.Submit(context.Background(), sdk.Submission{
		Namespace: "sample", MessageID: "msg-1", OperationID: "op-1", Goal: "Explain the fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.ReplyTo != "msg-1" || response.MessageID == "" || response.MessageID == "msg-1" {
		t.Fatalf("uncorrelated response: %+v", response)
	}
	if response.TaskID == "" || response.Evidence != "contract_fixture" {
		t.Fatalf("missing task or explicit fixture evidence: %+v", response)
	}
}

func TestOperationIdentitySurvivesNewMessages(t *testing.T) {
	client := sdk.NewClient(contractfixture.New(nil), nil)
	in := sdk.Submission{Namespace: "sample", MessageID: "m1", OperationID: "o1", Goal: "original"}
	first, err := client.Submit(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	repeat, err := client.Submit(context.Background(), in)
	if err != nil || repeat != first {
		t.Fatalf("original resend: %+v %v", repeat, err)
	}
	in.MessageID = "m2"
	second, err := client.Submit(context.Background(), in)
	if err != nil || second.TaskID != first.TaskID || second.ReplyTo != "m2" || second.MessageID == first.MessageID {
		t.Fatalf("new message, same operation: %+v %v", second, err)
	}
	in.Goal = "changed"
	in.MessageID = "m3"
	_, err = client.Submit(context.Background(), in)
	var conflict *protocol.Error
	if !errors.As(err, &conflict) || conflict.Code != wire.ErrorCode_ERROR_CODE_IDENTITY_CONFLICT {
		t.Fatalf("wanted identity conflict, got %v", err)
	}
	in.OperationID = "o2"
	in.MessageID = "m4"
	third, err := client.Submit(context.Background(), in)
	if err != nil || third.TaskID == first.TaskID {
		t.Fatalf("new business action: %+v %v", third, err)
	}
}

func TestDynamicIntentUsesSemanticEquality(t *testing.T) {
	registry, err := schema.New([]schema.Resource{contractfixture.SampleResource()})
	if err != nil {
		t.Fatal(err)
	}
	client := sdk.NewClient(contractfixture.New(registry), registry)
	in := sdk.Submission{Namespace: "sample", MessageID: "m1", OperationID: "o1", Goal: "same intent", Input: contractfixture.SamplePayload(`{"count":1,"note":null}`)}
	first, err := client.Submit(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	in.MessageID = "m2"
	in.Input = contractfixture.SamplePayload(`{ "note": null, "count": 1e0 }`)
	second, err := client.Submit(context.Background(), in)
	if err != nil || first.TaskID != second.TaskID {
		t.Fatalf("equivalent intent: %+v %v", second, err)
	}
	in.Input = contractfixture.SamplePayload(`{"count":1}`)
	in.MessageID = "m3"
	if _, err := client.Submit(context.Background(), in); err == nil {
		t.Fatal("absent and null intent treated as equal")
	}
}

type corruptTransport struct{ mutate func(*wire.Envelope) }

func (c corruptTransport) Exchange(ctx context.Context, data []byte) ([]byte, error) {
	raw, err := contractfixture.New(nil).Exchange(ctx, data)
	if err != nil {
		return nil, err
	}
	var response wire.Envelope
	if err := proto.Unmarshal(raw, &response); err != nil {
		return nil, err
	}
	c.mutate(&response)
	return proto.Marshal(&response)
}

func TestClientRejectsUncorrelatedOrUnprovenResponses(t *testing.T) {
	for name, mutate := range map[string]func(*wire.Envelope){
		"reply":            func(e *wire.Envelope) { e.ReplyTo = proto.String("other-request") },
		"namespace":        func(e *wire.Envelope) { e.Namespace = "other-namespace" },
		"operation":        func(e *wire.Envelope) { e.OperationId = "other-operation" },
		"message identity": func(e *wire.Envelope) { e.MessageId = "m1" },
		"evidence":         func(e *wire.Envelope) { e.GetResponse().Evidence = wire.Evidence_EVIDENCE_UNSPECIFIED },
		"task namespace":   func(e *wire.Envelope) { e.GetResponse().GetTask().Namespace = "other-namespace" },
		"missing result":   func(e *wire.Envelope) { e.GetResponse().Result = nil },
	} {
		t.Run(name, func(t *testing.T) {
			client := sdk.NewClient(corruptTransport{mutate}, nil)
			if _, err := client.Submit(context.Background(), sdk.Submission{Namespace: "sample", MessageID: "m1", OperationID: "o1", Goal: "sample"}); err == nil {
				t.Fatal("corrupted response accepted")
			}
		})
	}
}

func TestClientAndReceiverBothValidateRequests(t *testing.T) {
	ctx := context.Background()
	in := sdk.Submission{Namespace: "sample", MessageID: "m1", OperationID: "o1", Goal: " "}
	if _, err := sdk.NewClient(nil, nil).Submit(ctx, in); err == nil {
		t.Fatal("client accepted blank goal")
	}
	fixture := contractfixture.New(nil)
	request := &wire.Envelope{ProtocolMajor: 1, Namespace: "sample", MessageId: "m1", OperationId: "o1", Body: &wire.Envelope_Request{Request: &wire.SubmitRequest{}}}
	for name, mutate := range map[string]func(*wire.Envelope){
		"missing presence":          func(e *wire.Envelope) {},
		"response field on request": func(e *wire.Envelope) { e.GetRequest().Goal = proto.String("valid"); e.ReplyTo = proto.String("") },
		"unknown body":              func(e *wire.Envelope) { e.Body = nil },
		"unknown major":             func(e *wire.Envelope) { e.ProtocolMajor = 99 },
	} {
		t.Run(name, func(t *testing.T) {
			e := proto.Clone(request).(*wire.Envelope)
			mutate(e)
			raw, err := proto.Marshal(e)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.Exchange(ctx, raw); err == nil {
				t.Fatal("receiver accepted invalid request")
			}
		})
	}
}

func TestDistinctConflictResponseDoesNotReuseSuccessMessageIdentity(t *testing.T) {
	fixture := contractfixture.New(nil)
	request := &wire.Envelope{ProtocolMajor: 1, Namespace: "sample", MessageId: "m1", OperationId: "o1", Body: &wire.Envelope_Request{Request: &wire.SubmitRequest{Goal: proto.String("original")}}}
	exchange := func(e *wire.Envelope) *wire.Envelope {
		t.Helper()
		raw, err := proto.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		raw, err = fixture.Exchange(context.Background(), raw)
		if err != nil {
			t.Fatal(err)
		}
		result, err := protocol.Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	first := exchange(request)
	request.GetRequest().Goal = proto.String("changed")
	conflict := exchange(request)
	if conflict.GetResponse().GetFailure().GetCode() != wire.ErrorCode_ERROR_CODE_IDENTITY_CONFLICT {
		t.Fatal("expected identity conflict")
	}
	if first.MessageId == conflict.MessageId {
		t.Fatal("different responses reuse message identity")
	}
	request.GetRequest().Goal = proto.String("original")
	if resent := exchange(request); !proto.Equal(first, resent) {
		t.Fatal("original message resend changed response")
	}
}
