//go:build integration

package component_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/ruipengliu/lerna/contract"
	"os"
	"testing"
	"time"

	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
	"github.com/ruipengliu/lerna/runtime"
)

// This finite mechanical fence follows the real scheduler's successful writes.
// It is not a PostgreSQL failure or a substitute for the public business oracle.
type postScheduleFence struct {
	content.Repository
	until time.Time
	late  bool
}

func (r *postScheduleFence) ScheduleRetention(ctx context.Context, tx runtime.Tx, policy *content.FixturePolicy, record content.Record, sources []content.Record, budget time.Duration) error {
	if err := r.Repository.ScheduleRetention(ctx, tx, policy, record, sources, budget); err != nil {
		return err
	}
	if !r.late {
		return nil
	}
	timer := time.NewTimer(time.Until(r.until) + 20*time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func fencedContentService(t *testing.T, w *fixture.World, repository content.Repository) *content.Service {
	t.Helper()
	service, err := content.New(content.Config{Owner: contentOwner, Store: repository, Objects: w.Objects, Limits: content.Limits{MaxPreparingVersions: 16, MaxStagingBytes: 4 * 262144, Lease: time.Minute, WorkTimeout: 5 * time.Second}, PublishBudget: time.Minute, MaxPublicationAttempts: 3, Worker: "post-schedule-conformance"})
	if err != nil {
		t.Fatal(err)
	}
	return service
}
func TestContentPostScheduleAdmissionRollsBackLateVersion(t *testing.T) {
	for _, mode := range []string{"accept_before", "target_policy", "ancestor_policy", "retention"} {
		for _, late := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/late=%t", mode, late), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
				defer cancel()
				w := fixture.New(t, ctx)
				base := contentService(t, w)
				var source v.ContentRef
				if mode == "ancestor_policy" {
					source = alphaRef
					installContentPolicy(t, ctx, w, source)
					if _, ok := putContentRequest(t, ctx, base, contentPut(t, source, "fence-source", "YWxwaGEK")).AsAccepted(); !ok {
						t.Fatal("normal source refused")
					}
					if _, err := base.Step(ctx); err != nil {
						t.Fatal(err)
					}
					assertContentBody(t, ctx, base, source, nil, "alpha\n")
				}
				ref := alphaRef
				ref.ContentID = "fenced-output"
				installContentPolicy(t, ctx, w, ref)
				request := contentPut(t, ref, "fenced-command", "YWxwaGEK")
				if mode == "ancestor_policy" {
					request.Payload.Sources = []v.ContentRef{source}
				}
				until := time.Now().UTC().Add(750 * time.Millisecond).Truncate(time.Microsecond)
				if mode == "accept_before" {
					request.AcceptBefore = v.Time(until.Format("2006-01-02T15:04:05.000000Z"))
				}
				if mode == "retention" {
					request.Payload.RetainUntil = v.Time(until.Format("2006-01-02T15:04:05.000000Z"))
				}
				if mode == "target_policy" || mode == "ancestor_policy" {
					policyRef := ref
					if mode == "ancestor_policy" {
						policyRef = source
					}
					policy := content.FixturePolicy{Ref: policyRef, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: until, RetainUntil: time.Now().Add(time.Hour), Read: true, Process: true, Save: true, Disclose: true}
					if err := w.Store().InstallFixturePolicy(ctx, policy, 1); err != nil {
						t.Fatal(err)
					}
				}
				service := fencedContentService(t, w, &postScheduleFence{Repository: w.Store(), until: until, late: late})
				receipt := putContentRequest(t, ctx, service, request)
				if !late {
					if _, ok := receipt.AsAccepted(); !ok {
						t.Fatal("same scheduler normal rejected", receipt)
					}
					if _, err := service.Step(ctx); err != nil {
						t.Fatal(err)
					}
					assertContentBody(t, ctx, service, ref, nil, "alpha\n")
					return
				}
				reason := v.ErrorCode("expired")
				if mode == "target_policy" || mode == "ancestor_policy" {
					reason = "forbidden"
				}
				requireRejection(t, receipt, reason)
				w.Reopen(ctx)
				service = contentService(t, w)
				command, err := service.GetCommand(ctx, contentCommandGetWire(t, request.CommandID), &contentPrincipal)
				found, ok := command.AsFound()
				rejected, fixed := found.Receipt.AsRejected()
				if err != nil || !ok || !fixed || rejected.Reason != reason {
					t.Fatal("late rejection not durably fixed", err, command)
				}
				view, err := service.Get(ctx, contentGetWire(t, ref, nil), &contentPrincipal)
				// Renew only current target policy to observe absence; the old cutoff and
				// failed transaction are never rewritten into a successful admission.
				if mode == "target_policy" {
					wide := time.Now().Add(time.Hour)
					if err := w.Store().InstallFixturePolicy(ctx, content.FixturePolicy{Ref: ref, Subject: contentPrincipal, Purpose: "verification", Revision: 3, ValidUntil: wide, RetainUntil: wide, Read: true, Process: true, Save: true, Disclose: true}, 2); err != nil {
						t.Fatal(err)
					}
					view, err = service.Get(ctx, contentGetWire(t, ref, nil), &contentPrincipal)
				}
				if _, absent := view.AsNotFound(); err != nil || !absent {
					t.Fatal("late temporary version committed", err, view)
				}
				manager, err := content.NewManager(content.ManagementConfig{Owner: contentOwner, Store: w.Store(), TrustedSubject: contentPrincipal, TrustedUntil: time.Now().Add(time.Hour), PageSize: 2, WorkBudget: time.Minute})
				if err != nil {
					t.Fatal(err)
				}
				observation, _, err := manager.ObserveAdmissionChanges(ctx, &contentPrincipal, ref, "", 2)
				if err != nil || len(observation) != 0 {
					t.Fatal("late admission left inherited responsibility", err, observation)
				}
				if _, err = service.Step(ctx); err != nil {
					t.Fatal(err)
				}
				entries, err := os.ReadDir(w.Directory)
				expected := 0
				if mode == "ancestor_policy" {
					expected = 1
				}
				if err != nil || len(entries) != expected {
					t.Fatal("late transaction published an object", err, len(entries))
				}
			})
		}
	}
}

// This decorator injects an extra callback-return cause after a real rollback
// path. It does not claim a PostgreSQL native Rollback/Close failure.
type abortCauseRepository struct {
	content.Repository
	cause error
}

func (r *abortCauseRepository) Within(ctx context.Context, owner contract.OwnerRef, fn func(context.Context, runtime.Tx) error) error {
	err := r.Repository.Within(ctx, owner, fn)
	if err != nil {
		return errors.Join(err, r.cause)
	}
	return nil
}
func TestContentLateAdmissionExtraCauseCannotFixRejection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	installContentPolicy(t, ctx, w, alphaRef)
	until := time.Now().UTC().Add(450 * time.Millisecond).Truncate(time.Microsecond)
	request := contentPut(t, alphaRef, "extra-abort-cause", "YWxwaGEK")
	request.AcceptBefore = v.Time(until.Format("2006-01-02T15:04:05.000000Z"))
	injected := errors.New("mechanical extra callback-return cause")
	service := fencedContentService(t, w, &postScheduleFence{Repository: &abortCauseRepository{Repository: w.Store(), cause: injected}, until: until, late: true})
	raw, err := v.Encode(request)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Put(ctx, raw, &contentPrincipal)
	if !errors.Is(err, injected) {
		t.Fatal("unclassified abort cause was washed into a fixed receipt", err)
	}
	w.Reopen(ctx)
	service = contentService(t, w)
	command, err := service.GetCommand(ctx, contentCommandGetWire(t, request.CommandID), &contentPrincipal)
	if _, absent := command.AsNotFound(); err != nil || !absent {
		t.Fatal("extra-cause abort created a rejection transaction", err, command)
	}
	view, err := service.Get(ctx, contentGetWire(t, alphaRef, nil), &contentPrincipal)
	if _, absent := view.AsNotFound(); err != nil || !absent {
		t.Fatal("extra-cause callback committed temporary version", err, view)
	}
}

func TestContentPostScheduleAliasPreservesOriginalCapOnRollback(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(fmt.Sprintf("late=%t", late), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			w := fixture.New(t, ctx)
			installContentPolicy(t, ctx, w, alphaRef)
			service := contentService(t, w)
			request := contentPut(t, alphaRef, "original-before-alias", "YWxwaGEK")
			original := putContentRequest(t, ctx, service, request)
			if _, ok := original.AsAccepted(); !ok {
				t.Fatal("normal original refused")
			}
			if _, err := service.Step(ctx); err != nil {
				t.Fatal(err)
			}
			assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
			other := contentPrincipal
			other.DelegationChain = []v.DelegatedSubject{{TenantID: contentOwner.TenantID, SubjectID: "alias-delegating-actor"}}
			wide := time.Now().Add(time.Hour)
			narrow := time.Now().UTC().Add(1500 * time.Millisecond).Truncate(time.Microsecond)
			policy := content.FixturePolicy{Ref: alphaRef, Subject: other, Purpose: "verification", Revision: 1, ValidUntil: wide, RetainUntil: narrow, Read: true, Process: true, Save: true, Disclose: true}
			if err := w.Store().InstallFixturePolicy(ctx, policy, 0); err != nil {
				t.Fatal(err)
			}
			if err := w.Store().InstallFixtureCommandReader(ctx, contentOwner, other, wide); err != nil {
				t.Fatal(err)
			}
			until := time.Now().UTC().Add(450 * time.Millisecond).Truncate(time.Microsecond)
			alias := request
			alias.CommandID = "fenced-alias"
			alias.AcceptBefore = v.Time(until.Format("2006-01-02T15:04:05.000000Z"))
			service = fencedContentService(t, w, &postScheduleFence{Repository: w.Store(), until: until, late: late})
			raw, err := v.Encode(alias)
			if err != nil {
				t.Fatal(err)
			}
			outcome, err := service.Put(ctx, raw, &other)
			received, ok := outcome.AsReceived()
			if err != nil || !ok {
				t.Fatal(err, outcome)
			}
			if late {
				requireRejection(t, received.Receipt, "expired")
			} else if _, ok := received.Receipt.AsAccepted(); !ok {
				t.Fatal("same alias normal refused", received.Receipt)
			}
			w.Reopen(ctx)
			service = contentService(t, w)
			manager := trustedContentManager(t, w, 2)
			obligations, _, err := manager.ObserveAdmissionChanges(ctx, &contentPrincipal, alphaRef, "", 2)
			if err != nil {
				t.Fatal(err)
			}
			if late && len(obligations) != 0 {
				t.Fatal("aborted alias left target cap obligation", obligations)
			}
			if !late {
				if len(obligations) != 1 || !obligations[0].Due.Equal(narrow) {
					t.Fatal("normal alias did not fix original cap obligation", obligations)
				}
				a, _ := v.Encode(obligations[0].Policy.Subject)
				b, _ := v.Encode(contentPrincipal)
				if string(a) != string(b) {
					t.Fatal("alias used requesting subject as original saving basis")
				}
			}
			fixed, err := service.GetCommand(ctx, contentCommandGetWire(t, request.CommandID), &contentPrincipal)
			found, ok := fixed.AsFound()
			progress, published := found.Progress.AsContent()
			a, _ := v.Encode(found.Receipt)
			b, _ := v.Encode(original)
			if err != nil || !ok || !published || progress.Publication != "published" || string(a) != string(b) {
				t.Fatal("alias rollback changed original fixed history", err, fixed)
			}
			waitUntil(t, ctx, narrow.Add(20*time.Millisecond))
			if late {
				assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
			} else {
				view, err := service.Get(ctx, contentGetWire(t, alphaRef, nil), &contentPrincipal)
				denied, ok := view.AsRejected()
				if err != nil || !ok || denied.Reason != "expired" {
					t.Fatal("normal alias cap was not durable", err, view)
				}
			}
			entries, err := os.ReadDir(w.Directory)
			if err != nil || len(entries) != 1 {
				t.Fatal("alias changed exact original object scope", err)
			}
			body, err := os.ReadFile(w.Directory + "/" + entries[0].Name())
			if err != nil || string(body) != "alpha\n" {
				t.Fatal("alias erased or changed original bytes", err)
			}
		})
	}
}

// afterAbort introduces a precise competing transaction after the real first
// callback aborted and its original key lock was released. Unknown is an
// injected return after a successful real Commit, not a PG network fault.
type commandBoundaryRepository struct {
	content.Repository
	afterAbort func() error
	unknown    bool
}

func (r *commandBoundaryRepository) Within(ctx context.Context, owner contract.OwnerRef, fn func(context.Context, runtime.Tx) error) error {
	err := r.Repository.Within(ctx, owner, fn)
	if err != nil && r.afterAbort != nil {
		callback := r.afterAbort
		r.afterAbort = nil
		if extra := callback(); extra != nil {
			return errors.Join(err, extra)
		}
	}
	if err == nil && r.unknown {
		r.unknown = false
		return runtime.ErrCommitUnknown
	}
	return err
}
func TestContentLateAdmissionRespectsCompetingOriginalCommand(t *testing.T) {
	for _, different := range []bool{false, true} {
		t.Run(fmt.Sprintf("different_digest=%t", different), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			w := fixture.New(t, ctx)
			installContentPolicy(t, ctx, w, alphaRef)
			base := contentService(t, w)
			until := time.Now().UTC().Add(450 * time.Millisecond).Truncate(time.Microsecond)
			wide := time.Now().Add(time.Hour)
			policy := content.FixturePolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: until, RetainUntil: wide, Read: true, Process: true, Save: true, Disclose: true}
			if err := w.Store().InstallFixturePolicy(ctx, policy, 1); err != nil {
				t.Fatal(err)
			}
			request := contentPut(t, alphaRef, "competing-key", "YWxwaGEK")
			winner := request
			if different {
				winner.AcceptBefore = v.Time(time.Now().UTC().Add(25 * time.Minute).Truncate(time.Microsecond).Format("2006-01-02T15:04:05.000000Z"))
			}
			var winnerReceipt v.CommandReceipt
			boundary := &commandBoundaryRepository{Repository: w.Store(), afterAbort: func() error {
				policy.Revision = 3
				policy.ValidUntil = wide
				if err := w.Store().InstallFixturePolicy(ctx, policy, 2); err != nil {
					return err
				}
				raw, err := v.Encode(winner)
				if err != nil {
					return err
				}
				outcome, err := base.Put(ctx, raw, &contentPrincipal)
				if err != nil {
					return err
				}
				received, ok := outcome.AsReceived()
				if !ok {
					return errors.New("competing request had no fixed receipt")
				}
				if _, accepted := received.Receipt.AsAccepted(); !accepted {
					return errors.New("legitimate competing request refused")
				}
				winnerReceipt = received.Receipt
				return nil
			}}
			service := fencedContentService(t, w, &postScheduleFence{Repository: boundary, until: until, late: true})
			raw, err := v.Encode(request)
			if err != nil {
				t.Fatal(err)
			}
			result, err := service.Put(ctx, raw, &contentPrincipal)
			if different {
				var classified *v.ContractError
				if !errors.As(err, &classified) || classified.Code != "idempotency_conflict" {
					t.Fatal("late rejection overwrote competing digest", err, result)
				}
			} else {
				received, ok := result.AsReceived()
				a, _ := v.Encode(received.Receipt)
				b, _ := v.Encode(winnerReceipt)
				if err != nil || !ok || string(a) != string(b) {
					t.Fatal("late rejection overwrote real winner", err, result)
				}
			}
			w.Reopen(ctx)
			base = contentService(t, w)
			command, err := base.GetCommand(ctx, contentCommandGetWire(t, request.CommandID), &contentPrincipal)
			found, ok := command.AsFound()
			a, _ := v.Encode(found.Receipt)
			b, _ := v.Encode(winnerReceipt)
			if err != nil || !ok || string(a) != string(b) {
				t.Fatal("winner not durably preserved", err, command)
			}
			if _, err := base.Step(ctx); err != nil {
				t.Fatal(err)
			}
			assertContentBody(t, ctx, base, alphaRef, nil, "alpha\n")
		})
	}
}
func TestContentCommitUnknownKeepsAcceptedAndLateRejectedTruth(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(fmt.Sprintf("late_rejection=%t", late), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			w := fixture.New(t, ctx)
			installContentPolicy(t, ctx, w, alphaRef)
			until := time.Now().UTC().Add(450 * time.Millisecond).Truncate(time.Microsecond)
			request := contentPut(t, alphaRef, "mechanical-commit-unknown", "YWxwaGEK")
			if late {
				request.AcceptBefore = v.Time(until.Format("2006-01-02T15:04:05.000000Z"))
			}
			boundary := &commandBoundaryRepository{Repository: w.Store(), unknown: true}
			service := fencedContentService(t, w, &postScheduleFence{Repository: boundary, until: until, late: late})
			raw, err := v.Encode(request)
			if err != nil {
				t.Fatal(err)
			}
			outcome, err := service.Put(ctx, raw, &contentPrincipal)
			unknown, ok := outcome.AsCommitUnknown()
			if err != nil || !ok || unknown.CommandRef.CommandID != request.CommandID {
				t.Fatal("injected uncertain Commit became definite receipt", err, outcome)
			}
			w.Reopen(ctx)
			service = contentService(t, w)
			command, err := service.GetCommand(ctx, contentCommandGetWire(t, request.CommandID), &contentPrincipal)
			found, ok := command.AsFound()
			if err != nil || !ok {
				t.Fatal("real committed original cannot be recovered", err, command)
			}
			if late {
				requireRejection(t, found.Receipt, "expired")
				view, err := service.Get(ctx, contentGetWire(t, alphaRef, nil), &contentPrincipal)
				if _, absent := view.AsNotFound(); err != nil || !absent {
					t.Fatal("uncertain rejected Commit left temporary version", err, view)
				}
			} else {
				if _, accepted := found.Receipt.AsAccepted(); !accepted {
					t.Fatal("first uncertain accepted Commit was replaced by rejection", found.Receipt)
				}
				if _, err := service.Step(ctx); err != nil {
					t.Fatal(err)
				}
				assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
			}
		})
	}
}

// The second ledger write is real before this finite mechanical scheduling
// delay; CheckCommandReader still samples actual PG time after the delay.
type rejectedSaveFence struct {
	content.Repository
	until time.Time
	late  bool
}

func (r *rejectedSaveFence) SaveCommand(ctx context.Context, tx runtime.Tx, ref v.CommandRef, record content.CommandRecord) error {
	if err := r.Repository.SaveCommand(ctx, tx, ref, record); err != nil {
		return err
	}
	if _, rejected := record.Receipt.AsRejected(); !rejected || !r.late {
		return nil
	}
	timer := time.NewTimer(time.Until(r.until) + 20*time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func TestContentAdmissionReaderExpiryCannotDiscloseAfterWait(t *testing.T) {
	for _, stage := range []string{"original_key", "second_save"} {
		for _, late := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/late=%t", stage, late), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
				defer cancel()
				w := fixture.New(t, ctx)
				installContentPolicy(t, ctx, w, alphaRef)
				until := time.Now().UTC().Add(900 * time.Millisecond).Truncate(time.Microsecond)
				if err := w.Store().InstallFixtureCommandReader(ctx, contentOwner, contentPrincipal, until); err != nil {
					t.Fatal(err)
				}
				request := contentPut(t, alphaRef, "expiring-reader", "YWxwaGEK")
				service := contentService(t, w)
				raw, err := v.Encode(request)
				if err != nil {
					t.Fatal(err)
				}
				var result v.TransportOutcome
				if stage == "original_key" && late {
					release, wait := w.HoldCommand(ctx, v.CommandRef{Owner: contentOwner, CommandID: request.CommandID})
					done := make(chan struct {
						out v.TransportOutcome
						err error
					}, 1)
					finished := make(chan struct{})
					go func() {
						defer close(finished)
						out, err := service.Put(ctx, raw, &contentPrincipal)
						done <- struct {
							out v.TransportOutcome
							err error
						}{out, err}
					}()
					joinContentRead(t, w, release, finished)
					if err = wait(ctx); err != nil {
						t.Fatal(err)
					}
					waitUntil(t, ctx, until)
					if err = release(); err != nil {
						t.Fatal(err)
					}
					select {
					case completed := <-done:
						result, err = completed.out, completed.err
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
				} else if stage == "second_save" {
					early := time.Now().UTC().Add(250 * time.Millisecond).Truncate(time.Microsecond)
					request.AcceptBefore = v.Time(early.Format("2006-01-02T15:04:05.000000Z"))
					raw, err = v.Encode(request)
					if err != nil {
						t.Fatal(err)
					}
					service = fencedContentService(t, w, &postScheduleFence{Repository: &rejectedSaveFence{Repository: w.Store(), until: until, late: late}, until: early, late: true})
					result, err = service.Put(ctx, raw, &contentPrincipal)
				} else {
					result, err = service.Put(ctx, raw, &contentPrincipal)
				}
				if late {
					var classified *v.ContractError
					if !errors.As(err, &classified) || classified.Code != "forbidden" {
						t.Fatal("expired reader received command facts", err, result)
					}
				} else {
					received, ok := result.AsReceived()
					if err != nil || !ok {
						t.Fatal("normal reader refused", err, result)
					}
					if stage == "second_save" {
						requireRejection(t, received.Receipt, "expired")
					} else if _, ok := received.Receipt.AsAccepted(); !ok {
						t.Fatal("normal admission refused")
					}
				}
				// Restore only query authority to inspect the durable outcome after reopen.
				if err := w.Store().InstallFixtureCommandReader(ctx, contentOwner, contentPrincipal, time.Now().Add(time.Hour)); err != nil {
					t.Fatal(err)
				}
				w.Reopen(ctx)
				service = contentService(t, w)
				command, err := service.GetCommand(ctx, contentCommandGetWire(t, request.CommandID), &contentPrincipal)
				if late {
					if _, absent := command.AsNotFound(); err != nil || !absent {
						t.Fatal("unauthorized ledger write committed", err, command)
					}
				} else if _, found := command.AsFound(); err != nil || !found {
					t.Fatal("normal fixed receipt absent", err, command)
				}
				if late || stage == "second_save" {
					view, err := service.Get(ctx, contentGetWire(t, alphaRef, nil), &contentPrincipal)
					if _, absent := view.AsNotFound(); err != nil || !absent {
						t.Fatal("reader failure left temporary version", err, view)
					}
				} else {
					if _, err := service.Step(ctx); err != nil {
						t.Fatal(err)
					}
					assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
				}
			})
		}
	}
}
