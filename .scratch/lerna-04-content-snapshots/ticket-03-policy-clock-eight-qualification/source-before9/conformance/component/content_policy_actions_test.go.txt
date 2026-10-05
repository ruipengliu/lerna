//go:build integration

package component_test

import (
	"context"
	"fmt"
	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	"github.com/ruipengliu/lerna/contract"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
	"github.com/ruipengliu/lerna/runtime"
	"strings"
	"testing"
	"time"
)

// Metadata replacement is a mechanical observation after the real PG record
// lock. The policy, lock wait, public invocation and object bytes are real.
type closureMetadataRepository struct {
	content.Repository
	source  v.ContentRef
	missing bool
}

func (r *closureMetadataRepository) LockVersion(ctx context.Context, tx runtime.Tx, ref v.ContentRef) (*content.Record, error) {
	record, err := r.Repository.LockVersion(ctx, tx, ref)
	if err != nil || ref != r.source {
		return record, err
	}
	if r.missing {
		return nil, nil
	}
	if record != nil {
		copy := *record
		copy.Ref.MediaType = "application/octet-stream"
		return &copy, nil
	}
	return record, nil
}
func TestContentClosureRecordWaitRechecksPolicyBeforeMetadata(t *testing.T) {
	for _, missing := range []bool{false, true} {
		for _, late := range []bool{false, true} {
			t.Run(fmt.Sprintf("missing=%t/late=%t", missing, late), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
				defer cancel()
				w := fixture.New(t, ctx)
				base := contentService(t, w)
				installContentPolicy(t, ctx, w, alphaRef)
				if _, ok := putContentRequest(t, ctx, base, contentPut(t, alphaRef, "metadata-source", "YWxwaGEK")).AsAccepted(); !ok {
					t.Fatal("normal source refused")
				}
				if _, err := base.Step(ctx); err != nil {
					t.Fatal(err)
				}
				assertContentBody(t, ctx, base, alphaRef, nil, "alpha\n")
				target := alphaRef
				target.ContentID = "metadata-output"
				installContentPolicy(t, ctx, w, target)
				until := time.Now().UTC().Add(600 * time.Millisecond).Truncate(time.Microsecond)
				policy := content.FixturePolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: until, RetainUntil: time.Now().Add(time.Hour), Read: true, Process: true, Save: true, Disclose: true}
				if err := w.Store().InstallFixturePolicy(ctx, policy, 1); err != nil {
					t.Fatal(err)
				}
				service := fencedContentService(t, w, &closureMetadataRepository{Repository: w.Store(), source: alphaRef, missing: missing})
				request := contentPut(t, target, "metadata-command", "YWxwaGEK")
				request.Payload.Sources = []v.ContentRef{alphaRef}
				raw, err := v.Encode(request)
				if err != nil {
					t.Fatal(err)
				}
				release, wait := w.HoldVersion(ctx, alphaRef)
				done := make(chan struct {
					out v.TransportOutcome
					err error
				}, 1)
				finished := make(chan struct{})
				go func() {
					defer close(finished)
					out, e := service.Put(ctx, raw, &contentPrincipal)
					done <- struct {
						out v.TransportOutcome
						err error
					}{out, e}
				}()
				joinContentRead(t, w, release, finished)
				if err = wait(ctx); err != nil {
					t.Fatal(err)
				}
				if late {
					waitUntil(t, ctx, until.Add(20*time.Millisecond))
				}
				if err = release(); err != nil {
					t.Fatal(err)
				}
				select {
				case result := <-done:
					if result.err != nil {
						t.Fatal(result.err)
					}
					received, ok := result.out.AsReceived()
					if !ok {
						t.Fatal(result.out)
					}
					rejected, ok := received.Receipt.AsRejected()
					want := v.ErrorCode("source_unavailable")
					if late {
						want = "forbidden"
					}
					if !ok || rejected.Reason != want {
						t.Fatal("record lock exposed metadata after current source authority expired", received.Receipt, "want", want)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			})
		}
	}
}

func TestContentClosureActionIntersectionKeepsIndependentFlags(t *testing.T) {
	for _, denied := range []string{"none", "read", "process", "save", "disclose", "sync"} {
		t.Run(denied, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			w := fixture.New(t, ctx)
			service, request, source := publishedDirectContent(t, ctx, w)
			wide := time.Now().Add(time.Hour)
			policy := content.FixturePolicy{Ref: source, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: wide, RetainUntil: wide, Read: denied != "read", Process: denied != "process", Save: denied != "save", Sync: denied != "sync", Disclose: denied != "disclose"}
			if err := w.Store().InstallFixturePolicy(ctx, policy, 1); err != nil {
				t.Fatal(err)
			}
			nextRef := request.Payload.ContentRef
			nextRef.ContentID = "intersection-output"
			next := contentPut(t, nextRef, "intersection-command", "YWxwaGEK")
			next.Payload.Sources = request.Payload.Sources
			installContentPolicy(t, ctx, w, next.Payload.ContentRef)
			receipt := putContentRequest(t, ctx, service, next)
			if denied == "read" || denied == "process" || denied == "save" {
				requireRejection(t, receipt, "forbidden")
			} else if _, ok := receipt.AsAccepted(); !ok {
				t.Fatal("independent allowed admission rejected", receipt)
			}
			view, err := service.Get(ctx, contentGetWire(t, request.Payload.ContentRef, nil), &contentPrincipal)
			if err != nil {
				t.Fatal(err)
			}
			if denied == "read" || denied == "disclose" {
				rejected, ok := view.AsRejected()
				if !ok || rejected.Reason != "forbidden" {
					t.Fatal("disclosure intersection bypassed", view)
				}
			} else {
				assertContentBody(t, ctx, service, request.Payload.ContentRef, nil, "alpha\n")
			}
			err = service.AuthorizeUse(ctx, &contentPrincipal, source, "verification", "sync")
			if denied == "sync" {
				code, ok := err.(*v.ContractError)
				if !ok || code.Code != "forbidden" {
					t.Fatal("sync independently bypassed", err)
				}
			} else if err != nil {
				t.Fatal("unrelated false flag denied sync", err)
			}
		})
	}
}

// Invalid action vectors exercise the private adapter boundary with a real
// current policy; public consumers only construct fixed valid action sets.
func TestContentPolicyActionVectorsFailClosed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	installContentPolicy(t, ctx, w, alphaRef)
	for _, actions := range [][]string{{"read", "process", "save"}, {"read", "disclose"}, nil, {}, {"read", "read"}, {"unknown"}, {"read", "process", "save", "sync", "disclose", "read"}} {
		t.Run(fmt.Sprint(actions), func(t *testing.T) {
			err := w.Store().Within(ctx, contract.OwnerRef{TenantID: contract.ID(contentOwner.TenantID), OwnerID: contract.ID(contentOwner.OwnerID)}, func(ctx context.Context, tx runtime.Tx) error {
				now, err := w.Store().Now(ctx, tx)
				if err != nil {
					return err
				}
				policy, err := w.Store().CheckPolicy(ctx, tx, contentPrincipal, alphaRef, "verification", actions, now)
				if err != nil {
					return err
				}
				valid := len(actions) == 3 || len(actions) == 2 && actions[1] == "disclose"
				if valid && policy == nil {
					t.Fatal("normal action intersection refused")
				}
				if !valid && policy != nil {
					t.Fatal("invalid action vector acquired authority", actions)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestContentClosurePolicyIntersectionUsesClockAfterRealPolicyLock(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(fmt.Sprintf("late=%t", late), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			w := fixture.New(t, ctx)
			service := contentService(t, w)
			installContentPolicy(t, ctx, w, alphaRef)
			if _, ok := putContentRequest(t, ctx, service, contentPut(t, alphaRef, "locked-source", "YWxwaGEK")).AsAccepted(); !ok {
				t.Fatal("normal source refused")
			}
			if _, err := service.Step(ctx); err != nil {
				t.Fatal(err)
			}
			assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
			target := alphaRef
			target.ContentID = "policy-locked-output"
			installContentPolicy(t, ctx, w, target)
			until := time.Now().UTC().Add(600 * time.Millisecond).Truncate(time.Microsecond)
			policy := content.FixturePolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: until, RetainUntil: time.Now().Add(time.Hour), Read: true, Process: true, Save: true, Disclose: true}
			if err := w.Store().InstallFixturePolicy(ctx, policy, 1); err != nil {
				t.Fatal(err)
			}
			request := contentPut(t, target, "locked-output-command", "YWxwaGEK")
			request.Payload.Sources = []v.ContentRef{alphaRef}
			raw, err := v.Encode(request)
			if err != nil {
				t.Fatal(err)
			}
			release, wait := w.HoldPolicy(ctx, alphaRef)
			done := make(chan struct {
				out v.TransportOutcome
				err error
			}, 1)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				out, e := service.Put(ctx, raw, &contentPrincipal)
				done <- struct {
					out v.TransportOutcome
					err error
				}{out, e}
			}()
			joinContentRead(t, w, release, finished)
			if err = wait(ctx); err != nil {
				t.Fatal(err)
			}
			if late {
				waitUntil(t, ctx, until.Add(20*time.Millisecond))
			}
			if err = release(); err != nil {
				t.Fatal(err)
			}
			select {
			case result := <-done:
				if result.err != nil {
					t.Fatal(result.err)
				}
				received, ok := result.out.AsReceived()
				if !ok {
					t.Fatal(result.out)
				}
				if late {
					requireRejection(t, received.Receipt, "forbidden")
				} else {
					if _, ok := received.Receipt.AsAccepted(); !ok {
						t.Fatal("normal intersection refused", received.Receipt)
					}
					if _, err = service.Step(ctx); err != nil {
						t.Fatal(err)
					}
					assertContentBody(t, ctx, service, target, nil, "alpha\n")
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		})
	}
}

// The adapter's own trusted time must follow the real policy lock. This direct
// boundary prevents later domain clocks from hiding an early SQL clock sample.
func TestContentPolicyPortClockFollowsLockedRow(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(fmt.Sprintf("late=%t", late), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			w := fixture.New(t, ctx)
			installContentPolicy(t, ctx, w, alphaRef)
			until := time.Now().UTC().Add(600 * time.Millisecond).Truncate(time.Microsecond)
			policy := content.FixturePolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: until, RetainUntil: time.Now().Add(time.Hour), Read: true, Process: true, Save: true, Disclose: true}
			if err := w.Store().InstallFixturePolicy(ctx, policy, 1); err != nil {
				t.Fatal(err)
			}
			release, wait := w.HoldPolicy(ctx, alphaRef)
			done := make(chan struct {
				policy *content.FixturePolicy
				err    error
			}, 1)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				var p *content.FixturePolicy
				err := w.Store().Within(ctx, contract.OwnerRef{TenantID: contract.ID(contentOwner.TenantID), OwnerID: contract.ID(contentOwner.OwnerID)}, func(ctx context.Context, tx runtime.Tx) error {
					now, err := w.Store().Now(ctx, tx)
					if err != nil {
						return err
					}
					p, err = w.Store().CheckPolicy(ctx, tx, contentPrincipal, alphaRef, "verification", []string{"read", "process", "save"}, now)
					return err
				})
				done <- struct {
					policy *content.FixturePolicy
					err    error
				}{p, err}
			}()
			joinContentRead(t, w, release, finished)
			if err := wait(ctx); err != nil {
				t.Fatal(err)
			}
			if late {
				waitUntil(t, ctx, until.Add(20*time.Millisecond))
			}
			if err := release(); err != nil {
				t.Fatal(err)
			}
			select {
			case result := <-done:
				if result.err != nil {
					t.Fatal(result.err)
				}
				if late && result.policy != nil {
					t.Fatal("port reused trusted time sampled before real row lock")
				}
				if !late && (result.policy == nil || result.policy.Ref != alphaRef) {
					t.Fatal("normal locked policy refused")
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			plan, err := w.PolicyClockPlan(ctx, policy)
			if err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(plan, "\n")
			t.Log("mechanical PG execution plan (conditions omitted):\n" + joined)
			if !strings.Contains(joined, "CTE Scan on locked_policy") || !strings.Contains(joined, "LockRows") || !strings.Contains(joined, "locked_policy.body, clock_timestamp()") {
				t.Fatal("expected materialized locked row and outer clock projection", joined)
			}
		})
	}
}
