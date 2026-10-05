package sessions_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/contracts/errs"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/core/sessions"
	"github.com/ruipengliu/lerna/infra/sqlite"
)

type clock struct{ t time.Time }

func (c *clock) Now() time.Time { return c.t }

func domain(t *testing.T) *durable.Domain {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "adj.db"), "adjudication", sqlite.LocalProfile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return durable.NewDomain("adjudication", db, &clock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
}

func write(t *testing.T, d *durable.Domain, fn func(tx *durable.Tx) error) error {
	t.Helper()
	return d.Write(context.Background(), "t", fn)
}

// 一个确认只能被消费一次；绑定不一致、类型不一致的消费都被拒绝。
//
// 规则：准入-9、G4、G7
func TestConfirmationConsumedOnceWithMatchingBindingAndKind(t *testing.T) {
	d := domain(t)
	c := &lernav1.Confirmation{UserId: "u", ConfirmationId: "c1", SessionId: "s", TaskId: "t", ProposalId: "p", StepId: "s1",
		SubjectKind: lernav1.ConfirmationSubjectKind_CONFIRMATION_SUBJECT_KIND_OPERATION_ADMISSION, IntentFingerprint: []byte("fp"), Description: "d"}
	if err := write(t, d, func(tx *durable.Tx) error {
		if err := sessions.CreateConfirmation(tx, c); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE confirmations SET status = ? WHERE confirmation_id = 'c1'`, int32(lernav1.ConfirmationStatus_CONFIRMATION_STATUS_APPROVED))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	err := write(t, d, func(tx *durable.Tx) error {
		_, err := sessions.ConsumeForAdmission(tx, "u", "p", "s1", []byte("other"), "a0")
		return err
	})
	if !errs.Is(err, lernav1.ErrorCode_ERROR_CODE_CONFIRMATION_INVALID) {
		t.Fatalf("different binding must be refused: %v", err)
	}
	err = write(t, d, func(tx *durable.Tx) error { return sessions.ConsumeForGrant(tx, "u", "c1", []byte("fp"), "g1") })
	if !errs.Is(err, lernav1.ErrorCode_ERROR_CODE_CONFIRMATION_INVALID) {
		t.Fatalf("an admission confirmation cannot issue a grant: %v", err)
	}
	var got *lernav1.Confirmation
	if err := write(t, d, func(tx *durable.Tx) error {
		var err error
		got, err = sessions.ConsumeForAdmission(tx, "u", "p", "s1", []byte("fp"), "a1")
		return err
	}); err != nil || got == nil {
		t.Fatalf("matching consumption: %v %v", got, err)
	}
	if err := write(t, d, func(tx *durable.Tx) error {
		var err error
		got, err = sessions.ConsumeForAdmission(tx, "u", "p", "s1", []byte("fp"), "a2")
		return err
	}); err != nil || got != nil {
		t.Fatalf("a consumed confirmation cannot be consumed again: %v %v", got, err)
	}
}
