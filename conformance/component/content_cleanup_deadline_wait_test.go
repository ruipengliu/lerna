//go:build integration

package component_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
)

func TestContentOriginalCleanupDeadlineCannotBeExtendedByCurrentPolicyLockWait(t *testing.T) {
	for _, crossing := range []bool{false, true} {
		name := "release-within-original-budget"
		if crossing {
			name = "release-after-original-deadline"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			w := fixture.New(t, ctx)
			wide := time.Now().Add(time.Hour)
			management := func() content.ManagementConfig {
				return content.ManagementConfig{Owner: contentOwner, Store: w.Store(), TrustedSubject: contentPrincipal, TrustedUntil: wide, PageSize: 2, WorkBudget: 700 * time.Millisecond}
			}
			manager := func() *content.Manager {
				t.Helper()
				m, err := content.NewManager(management())
				if err != nil {
					t.Fatal(err)
				}
				return m
			}
			lifecycle := func() *content.Lifecycle {
				t.Helper()
				l, err := content.NewLifecycle(content.LifecycleConfig{ManagementConfig: management(), PrimaryHolderID: "primary", Objects: w.Objects, Worker: "original-deadline-cleanup"})
				if err != nil {
					t.Fatal(err)
				}
				return l
			}
			m := manager()
			policy := content.FixturePolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 1, ValidUntil: wide, RetainUntil: wide, Read: true, Process: true, Save: true, Sync: true, Disclose: true}
			change, err := m.InstallPolicy(ctx, &contentPrincipal, policy, 0)
			if err != nil {
				t.Fatal(err)
			}
			if err = w.Store().InstallFixtureCommandReader(ctx, contentOwner, contentPrincipal, wide); err != nil {
				t.Fatal(err)
			}
			cap := time.Now().UTC().Add(2 * time.Second).Truncate(time.Microsecond)
			request := contentPut(t, alphaRef, "deadline-original", "YWxwaGEK")
			request.Payload.RetainUntil = v.Time(cap.Format("2006-01-02T15:04:05.000000Z"))
			service := contentService(t, w)
			receipt := putContentRequest(t, ctx, service, request)
			if _, ok := receipt.AsAccepted(); !ok {
				t.Fatal("normal short-cap original refused")
			}
			if _, err = service.Step(ctx); err != nil {
				t.Fatal(err)
			}
			assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
			fixedReceipt, err := v.Encode(receipt)
			if err != nil {
				t.Fatal(err)
			}
			scheduled, err := m.ObserveChange(ctx, &contentPrincipal, change.Key, "", 2)
			if err != nil || !scheduled.Change.ExpiryDue.Equal(cap) || !scheduled.Change.ExpiryDeadline.Equal(cap.Add(700*time.Millisecond)) {
				t.Fatal("original 700ms budget was not fixed at acceptance", scheduled, err)
			}
			policy.Revision = 2
			if _, err = m.InstallPolicy(ctx, &contentPrincipal, policy, 1); err != nil {
				t.Fatal(err)
			}
			if _, err = service.Step(ctx); err != nil {
				t.Fatal(err)
			}
			version2 := alphaRef
			version2.Version = "2"
			version2.Hash = "sha256:f2c82decdd7181cf98945929a62598db7e6b477e11f6e0eb0ae97020eff151ad"
			version2.ByteLength = "5"
			independent := policy
			independent.Ref = version2
			independent.Revision = 1
			if _, err = m.InstallPolicy(ctx, &contentPrincipal, independent, 0); err != nil {
				t.Fatal(err)
			}
			if _, ok := putContentRequest(t, ctx, service, contentPut(t, version2, "deadline-independent", "YmV0YQo=")).AsAccepted(); !ok {
				t.Fatal("normal independent Version2 refused")
			}
			if _, err = service.Step(ctx); err != nil {
				t.Fatal(err)
			}
			assertContentBody(t, ctx, service, version2, nil, "beta\n")
			waitUntil(t, ctx, cap.Add(20*time.Millisecond))
			if worked, err := m.Step(ctx, &contentPrincipal); err != nil || !worked {
				t.Fatal("original real due did not execute", worked, err)
			}
			original, err := m.ObserveChange(ctx, &contentPrincipal, change.Key, "", 2)
			if err != nil || len(original.Responsibilities) != 1 {
				t.Fatal(original, err)
			}
			duty := original.Responsibilities[0]
			if duty.Ref != alphaRef || duty.BodyCleanup != "pending" || duty.Reason != "accepted_retention_expired" || !duty.ObjectHolder || !duty.Deadline.Equal(cap.Add(700*time.Millisecond)) || !time.Now().Before(duty.Deadline) {
				t.Fatal("original live finite responsibility missing", duty)
			}
			deniedView, err := service.Get(ctx, contentGetWire(t, alphaRef, nil), &contentPrincipal)
			denied, ok := deniedView.AsRejected()
			if err != nil || !ok || denied.Reason != "expired" {
				t.Fatal("current wide policy revived accepted cap", deniedView, err)
			}
			_, key, err := content.VersionIdentity(alphaRef)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(w.Directory, key)
			body, err := os.ReadFile(path)
			if err != nil || string(body) != "alpha\n" {
				t.Fatal("original pending body not actually present", err)
			}
			l := lifecycle()
			release, waitBlocked := w.HoldPolicy(ctx, alphaRef)
			done := make(chan error, 1)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				_, err := l.ConsumePolicyCleanup(ctx, &contentPrincipal, change.Key, "")
				done <- err
			}()
			joinContentRead(t, w, release, finished)
			if err = waitBlocked(ctx); err != nil {
				t.Fatal(err)
			}
			if crossing {
				waitUntil(t, ctx, duty.Deadline.Add(20*time.Millisecond))
			}
			if err = release(); err != nil {
				t.Fatal("original locker Close/rollback unconfirmed", err)
			}
			select {
			case err = <-done:
				if err != nil {
					t.Fatal("bounded actual consumer failed", err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			select {
			case <-finished:
			case <-ctx.Done():
				t.Fatal("actual consumer did not finish", ctx.Err())
			}
			if crossing {
				after, err := m.ObserveChange(ctx, &contentPrincipal, change.Key, "", 2)
				if err != nil || !reflect.DeepEqual(after.Responsibilities, original.Responsibilities) {
					t.Fatal("expired original duty changed after policy wait", after, err)
				}
				if _, err = l.Observe(ctx, &contentPrincipal, alphaRef, ""); !errors.Is(err, content.ErrUnavailable) {
					t.Fatal("expired original execution budget created a seal", err)
				}
				if worked, err := l.Step(ctx, &contentPrincipal); err != nil || worked {
					t.Fatal("expired original budget created executable cleanup", worked, err)
				}
				body, err = os.ReadFile(path)
				if err != nil || string(body) != "alpha\n" {
					t.Fatal("late lock release erased original bytes", err)
				}
			} else {
				sealed, err := l.Observe(ctx, &contentPrincipal, alphaRef, "")
				if err != nil || sealed.Seal.PolicyChangeKey != change.Key || !sealed.Seal.Deadline.Equal(duty.Deadline) {
					t.Fatal("same 700ms normal wait did not seal original duty", sealed, err)
				}
				for i := 0; i < 3; i++ {
					if _, err = l.Step(ctx, &contentPrincipal); err != nil {
						t.Fatal(err)
					}
				}
				all, err := l.Observe(ctx, &contentPrincipal, alphaRef, "")
				if err != nil || !all.CleanupComplete || !all.Seal.Deadline.Equal(duty.Deadline) {
					t.Fatal("same original 700ms normal cleanup did not allACK", all, err)
				}
				if _, err = os.ReadFile(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("normal allACK lacks independent physical absence", err)
				}
				ack, err := m.ObserveChange(ctx, &contentPrincipal, change.Key, "", 2)
				if err != nil || len(ack.Responsibilities) != 1 || ack.Responsibilities[0].BodyCleanup != "erased" || !ack.Responsibilities[0].Deadline.Equal(duty.Deadline) {
					t.Fatal("normal allACK lost original duty", ack, err)
				}
			}
			w.Reopen(ctx)
			service = contentService(t, w)
			assertContentBody(t, ctx, service, version2, nil, "beta\n")
			replay, err := v.Encode(putContentRequest(t, ctx, service, request))
			if err != nil || string(replay) != string(fixedReceipt) {
				t.Fatal("wait changed original fixed receipt", err)
			}
			observed, err := manager().ObserveChange(ctx, &contentPrincipal, change.Key, "", 2)
			if err != nil || len(observed.Responsibilities) != 1 || !observed.Responsibilities[0].Deadline.Equal(duty.Deadline) {
				t.Fatal("reopen changed original deadline", observed, err)
			}
			if crossing {
				if !reflect.DeepEqual(observed.Responsibilities, original.Responsibilities) {
					t.Fatal("reopen lost exact original pending duty", observed)
				}
				if _, err = lifecycle().Observe(ctx, &contentPrincipal, alphaRef, ""); !errors.Is(err, content.ErrUnavailable) {
					t.Fatal("reopen introduced late seal", err)
				}
				body, err = os.ReadFile(path)
				if err != nil || string(body) != "alpha\n" {
					t.Fatal("reopen lost refused cleanup body", err)
				}
			} else {
				all, err := lifecycle().Observe(ctx, &contentPrincipal, alphaRef, "")
				if err != nil || !all.CleanupComplete || observed.Responsibilities[0].BodyCleanup != "erased" {
					t.Fatal("reopen lost original normal allACK", all, observed, err)
				}
			}
		})
	}
}
