package egress_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ruipengliu/lerna/contracts/errs"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ports"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/core/egress"
)

type adjudication struct {
	decision lernav1.Decision
	starts   int
}

func (a *adjudication) Execute(_ context.Context, env *lernav1.CommandEnvelope) (*lernav1.Receipt, error) {
	a.starts++
	r := &lernav1.Receipt{Identity: env.GetIdentity(), Decision: a.decision, Phase: lernav1.CommandPhase_COMMAND_PHASE_DECIDED}
	if a.decision == lernav1.Decision_DECISION_REJECTED {
		r.Rejection = errs.New(lernav1.ErrorCode_ERROR_CODE_STALE_GENERATION, "control moved").E
	}
	return r, nil
}

func (a *adjudication) QueryReceipt(context.Context, *lernav1.CommandIdentity) (*lernav1.ReceiptQueryResponse, error) {
	return &lernav1.ReceiptQueryResponse{Outcome: lernav1.ReceiptQueryOutcome_RECEIPT_QUERY_OUTCOME_NOT_FOUND}, nil
}

type counter struct{ calls int }

func (c *counter) AdapterID() string                              { return "x" }
func (c *counter) Declarations() []*lernav1.CapabilityDeclaration { return nil }
func (c *counter) Execute(context.Context, *lernav1.ExecuteRequest) (*lernav1.ExecutionReport, error) {
	c.calls++
	return nil, errors.New("connection reset")
}
func (c *counter) Query(context.Context, *lernav1.QueryRequest) (*lernav1.ExecutionReport, error) {
	return nil, nil
}

func gate(decision lernav1.Decision) (*egress.Gate, *adjudication, *counter) {
	a := &adjudication{decision: decision}
	r := durable.NewRouter()
	r.Register("adj", a)
	c := &counter{}
	return &egress.Gate{Router: r, Executor: func(string) (ports.Executor, bool) { return c, true }}, a, c
}

func send() egress.Send {
	return egress.Send{
		Issuer: "ledger", AdjudicationDomain: "adj", AdapterID: "x",
		Start:   &lernav1.StartSendCommand{UserId: "u", AttemptId: "a", SendSeq: 1, Purpose: lernav1.SendPurpose_SEND_PURPOSE_EXECUTE},
		Execute: &lernav1.ExecuteRequest{},
	}
}

// 没有 P5 持久化成功不得执行外部 I/O；开始门禁拒绝时也不得执行。
//
// 规则：G4、G5、开始-1
func TestNoIOWithoutStartReceiptAndDispatchPossible(t *testing.T) {
	g, _, c := gate(lernav1.Decision_DECISION_REJECTED)
	err := g.Call(context.Background(), send(), egress.Hooks{
		DispatchPossible: func(context.Context, *lernav1.Receipt) error {
			t.Fatal("P5 must not run after a rejection")
			return nil
		},
		Observe: func(context.Context, *lernav1.ExecutionReport, error) error { return nil },
	})
	var rej *egress.ErrStartRejected
	if !errors.As(err, &rej) || c.calls != 0 {
		t.Fatalf("rejected start must not reach the target: err=%v calls=%d", err, c.calls)
	}
	g, _, c = gate(lernav1.Decision_DECISION_ACCEPTED)
	p5 := errors.New("sealed")
	err = g.Call(context.Background(), send(), egress.Hooks{
		DispatchPossible: func(context.Context, *lernav1.Receipt) error { return p5 },
		Observe:          func(context.Context, *lernav1.ExecutionReport, error) error { return nil },
	})
	if !errors.Is(err, p5) || c.calls != 0 {
		t.Fatalf("without a persisted DISPATCH_POSSIBLE there must be no I/O: err=%v calls=%d", err, c.calls)
	}
}

// 一次放行只调用适配器一次：传输错误回报为无法确定，出口闸门不重发。
//
// 规则：G5、G1
func TestOneCallPerStartAndErrorsAreIndeterminate(t *testing.T) {
	g, a, c := gate(lernav1.Decision_DECISION_ACCEPTED)
	var observed error
	err := g.Call(context.Background(), send(), egress.Hooks{
		DispatchPossible: func(context.Context, *lernav1.Receipt) error { return nil },
		Observe: func(_ context.Context, _ *lernav1.ExecutionReport, ioErr error) error {
			observed = ioErr
			return nil
		},
	})
	if err != nil || c.calls != 1 || a.starts != 1 {
		t.Fatalf("err=%v calls=%d starts=%d", err, c.calls, a.starts)
	}
	if observed == nil {
		t.Fatal("the I/O error must be passed to the ledger as an unknown outcome")
	}
}
