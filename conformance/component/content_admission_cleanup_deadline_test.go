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

func TestContentOriginalAdmissionCleanupDeadlineSurvivesCurrentTargetPolicyLockWait(t *testing.T) {
	for _, crossing := range []bool{false, true} {
		name := "release-within-original-admission-budget"
		if crossing {
			name = "release-after-original-admission-deadline"
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
				l, err := content.NewLifecycle(content.LifecycleConfig{ManagementConfig: management(), PrimaryHolderID: "primary", Objects: w.Objects, Worker: "admission-deadline-cleanup"})
				if err != nil {
					t.Fatal(err)
				}
				return l
			}
			m := manager()
			policy := content.FixturePolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 1, ValidUntil: wide, RetainUntil: wide, Read: true, Process: true, Save: true, Sync: true, Disclose: true}
			if _, err := m.InstallPolicy(ctx, &contentPrincipal, policy, 0); err != nil {
				t.Fatal(err)
			}
			if err := w.Store().InstallFixtureCommandReader(ctx, contentOwner, contentPrincipal, wide); err != nil {
				t.Fatal(err)
			}
			cap := time.Now().UTC().Add(2 * time.Second).Truncate(time.Microsecond)
			service := contentService(t, w)
			rootRequest := contentPut(t, alphaRef, "admission-deadline-original-root", "YWxwaGEK")
			rootRequest.Payload.RetainUntil = v.Time(cap.Format("2006-01-02T15:04:05.000000Z"))
			rootReceipt := putContentRequest(t, ctx, service, rootRequest)
			if _, ok := rootReceipt.AsAccepted(); !ok {
				t.Fatal("normal short-cap source refused")
			}
			if _, err := service.Step(ctx); err != nil {
				t.Fatal(err)
			}
			assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
			policy.Revision = 2
			if _, err := m.InstallPolicy(ctx, &contentPrincipal, policy, 1); err != nil {
				t.Fatal(err)
			}
			if _, err := service.Step(ctx); err != nil {
				t.Fatal(err)
			}
			target := alphaRef
			target.ContentID = "admission-deadline-target"
			targetPolicy := policy
			targetPolicy.Ref, targetPolicy.Revision = target, 1
			if _, err := m.InstallPolicy(ctx, &contentPrincipal, targetPolicy, 0); err != nil {
				t.Fatal(err)
			}
			request := contentPut(t, target, "admission-deadline-original-target", "YWxwaGEK")
			request.Payload.Sources = []v.ContentRef{alphaRef}
			receipt := putContentRequest(t, ctx, service, request)
			accepted, ok := receipt.AsAccepted()
			if !ok || accepted.RetainUntil != rootRequest.Payload.RetainUntil {
				t.Fatal("actual admission did not retain the source cap", receipt)
			}
			if _, err := service.Step(ctx); err != nil {
				t.Fatal(err)
			}
			assertContentBody(t, ctx, service, target, nil, "alpha\n")
			if err := service.AuthorizeUse(ctx, &contentPrincipal, target, "verification", "save"); err != nil {
				t.Fatal("normal original saving closure refused", err)
			}
			requests := []v.ContentPutRequest{rootRequest, request}
			receipts := []v.CommandReceipt{rootReceipt, receipt}
			fixed := make([][]byte, len(receipts))
			for i, receipt := range receipts {
				var err error
				fixed[i], err = v.Encode(receipt)
				if err != nil {
					t.Fatal(err)
				}
			}
			version2 := alphaRef
			version2.Version, version2.Hash, version2.ByteLength = "2", "sha256:f2c82decdd7181cf98945929a62598db7e6b477e11f6e0eb0ae97020eff151ad", "5"
			independent := targetPolicy
			independent.Ref = version2
			if _, err := m.InstallPolicy(ctx, &contentPrincipal, independent, 0); err != nil {
				t.Fatal(err)
			}
			if _, ok := putContentRequest(t, ctx, service, contentPut(t, version2, "admission-deadline-independent", "YmV0YQo=")).AsAccepted(); !ok {
				t.Fatal("normal independent Version2 refused")
			}
			if _, err := service.Step(ctx); err != nil {
				t.Fatal(err)
			}
			assertContentBody(t, ctx, service, version2, nil, "beta\n")
			var admission *content.PolicyChange
			cursor := ""
			for i := 0; i < 4; i++ {
				changes, next, err := m.ObserveAdmissionChanges(ctx, &contentPrincipal, target, cursor, 2)
				if err != nil {
					t.Fatal(err)
				}
				for _, change := range changes {
					if change.Policy.Ref != alphaRef || !change.ExpiryDue.Equal(cap) {
						continue
					}
					if admission != nil || change.AdmissionTarget == nil || *change.AdmissionTarget != target || !change.ExpiryDeadline.Equal(cap.Add(700*time.Millisecond)) || change.Policy.Revision != 2 || !reflect.DeepEqual(change.Policy.Subject, contentPrincipal) || change.Policy.Purpose != "verification" {
						t.Fatal("exact admission original 700ms window not fixed", change)
					}
					copy := change
					admission = &copy
				}
				cursor = next
				if cursor == "" {
					break
				}
			}
			if admission == nil || cursor != "" {
				t.Fatal("finite admission pages missed original target duty", admission, cursor)
			}
			waitUntil(t, ctx, cap.Add(20*time.Millisecond))
			var original content.PropagationObservation
			found := false
			for i := 0; i < 12; i++ {
				if _, err := m.Step(ctx, &contentPrincipal); err != nil {
					t.Fatal(err)
				}
				var err error
				original, err = m.ObserveChange(ctx, &contentPrincipal, admission.Key, "", 2)
				if err != nil {
					t.Fatal(err)
				}
				if len(original.Responsibilities) == 1 && original.Responsibilities[0].BodyCleanup == "pending" {
					found = true
					break
				}
			}
			if !found {
				t.Fatal("actual original due did not register the target duty", original)
			}
			duty := original.Responsibilities[0]
			if duty.Ref != target || !reflect.DeepEqual(duty.Subject, contentPrincipal) || duty.Purpose != "verification" || duty.Reason != "accepted_retention_expired" || duty.Residual != "holder_unconfirmed" || !duty.ObjectHolder || !duty.Deadline.Equal(admission.ExpiryDeadline) || !time.Now().Before(duty.Deadline) {
				t.Fatal("original live admission responsibility missing", duty)
			}
			for _, ref := range []v.ContentRef{alphaRef, target} {
				view, err := service.Get(ctx, contentGetWire(t, ref, nil), &contentPrincipal)
				denied, ok := view.AsRejected()
				if err != nil || !ok || denied.Reason != "expired" {
					t.Fatal("wide renewal revived inherited cap", view, err)
				}
			}
			_, key, err := content.VersionIdentity(target)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(w.Directory, key)
			body, err := os.ReadFile(path)
			if err != nil || string(body) != "alpha\n" {
				t.Fatal("pending target body not actually present", err)
			}
			l := lifecycle()
			release, waitBlocked := w.HoldPolicy(ctx, target)
			done := make(chan error, 1)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				_, err := l.ConsumePolicyCleanup(ctx, &contentPrincipal, admission.Key, "")
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
				t.Fatal("original locker closure unconfirmed", err)
			}
			select {
			case err = <-done:
				if err != nil {
					t.Fatal("bounded admission consumer failed", err)
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
				after, err := m.ObserveChange(ctx, &contentPrincipal, admission.Key, "", 2)
				if err != nil || !reflect.DeepEqual(after, original) {
					t.Fatal("late original admission tuple changed", after, err)
				}
				if _, err = l.Observe(ctx, &contentPrincipal, target, ""); !errors.Is(err, content.ErrUnavailable) {
					t.Fatal("expired original admission created a seal", err)
				}
				if worked, err := l.Step(ctx, &contentPrincipal); err != nil || worked {
					t.Fatal("late admission created executable cleanup", worked, err)
				}
				body, err = os.ReadFile(path)
				if err != nil || string(body) != "alpha\n" {
					t.Fatal("late lock release erased target bytes", err)
				}
			} else {
				sealed, err := l.Observe(ctx, &contentPrincipal, target, "")
				if err != nil || sealed.Seal.PolicyChangeKey != admission.Key || !sealed.Seal.Deadline.Equal(duty.Deadline) {
					t.Fatal("same 700ms normal admission did not seal original duty", sealed, err)
				}
				for i := 0; i < 3; i++ {
					if _, err = l.Step(ctx, &contentPrincipal); err != nil {
						t.Fatal(err)
					}
				}
				all, err := l.Observe(ctx, &contentPrincipal, target, "")
				if err != nil || !all.CleanupComplete || all.Seal.PolicyChangeKey != admission.Key || !all.Seal.Deadline.Equal(duty.Deadline) {
					t.Fatal("same original 700ms normal admission did not allACK", all, err)
				}
				if _, err = os.ReadFile(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("target allACK lacks independent physical absence", err)
				}
			}
			w.Reopen(ctx)
			service = contentService(t, w)
			assertContentBody(t, ctx, service, version2, nil, "beta\n")
			observed, err := manager().ObserveChange(ctx, &contentPrincipal, admission.Key, "", 2)
			if err != nil || len(observed.Responsibilities) != 1 || !observed.Responsibilities[0].Deadline.Equal(duty.Deadline) || observed.Change.Watermark != original.Change.Watermark {
				t.Fatal("reopen changed original admission identity", observed, err)
			}
			if crossing {
				if !reflect.DeepEqual(observed, original) {
					t.Fatal("reopen lost original pending admission", observed)
				}
				if _, err = lifecycle().Observe(ctx, &contentPrincipal, target, ""); !errors.Is(err, content.ErrUnavailable) {
					t.Fatal("reopen introduced a late target seal", err)
				}
				body, err = os.ReadFile(path)
				if err != nil || string(body) != "alpha\n" {
					t.Fatal("reopen lost refused target body", err)
				}
			} else {
				all, err := lifecycle().Observe(ctx, &contentPrincipal, target, "")
				if err != nil || !all.CleanupComplete || observed.Responsibilities[0].BodyCleanup != "erased" || observed.Responsibilities[0].Reason != duty.Reason {
					t.Fatal("reopen lost exact original target allACK", all, observed, err)
				}
			}
			_, rootKey, err := content.VersionIdentity(alphaRef)
			if err != nil {
				t.Fatal(err)
			}
			body, err = os.ReadFile(filepath.Join(w.Directory, rootKey))
			if err != nil || string(body) != "alpha\n" {
				t.Fatal("target admission cleanup erased distinct source bytes", err)
			}
			if _, err = lifecycle().Observe(ctx, &contentPrincipal, alphaRef, ""); !errors.Is(err, content.ErrUnavailable) {
				t.Fatal("target admission created a source seal", err)
			}
			for i, request := range requests {
				replay, err := v.Encode(putContentRequest(t, ctx, service, request))
				if err != nil || string(replay) != string(fixed[i]) {
					t.Fatal("lock wait changed original fixed receipt", err)
				}
				command, err := service.GetCommand(ctx, contentCommandGetWire(t, request.CommandID), &contentPrincipal)
				found, ok := command.AsFound()
				progress, published := found.Progress.AsContent()
				if err != nil || !ok || !published || progress.Publication != "published" {
					t.Fatal("lock wait changed original publication history", command, err)
				}
			}
		})
	}
}
