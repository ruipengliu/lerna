//go:build integration

package component_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	"github.com/ruipengliu/lerna/contract"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
	"github.com/ruipengliu/lerna/runtime"
)

// This is a Host / actual PostgreSQL port qualification. Controlled stored-row
// corruption is not a legitimate Content business case or a business red.
func TestContentCleanupScanKeepsMalformedDeadlineParseCauseBeforePage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	service := contentService(t, w)
	const primary = "主介质 \" \\ <>& \u2028"
	if len(primary) > 128 {
		t.Fatal("normal primary identity exceeds the original interface bound")
	}
	newLifecycle := func() *content.Lifecycle {
		l, err := content.NewLifecycle(content.LifecycleConfig{ManagementConfig: content.ManagementConfig{Owner: contentOwner, Store: w.Store(), TrustedSubject: contentPrincipal, TrustedUntil: time.Now().Add(time.Hour), PageSize: 2, WorkBudget: time.Minute}, PrimaryHolderID: primary, Objects: w.Objects, Worker: "scan-deadline-cleanup"})
		if err != nil {
			t.Fatal("normal legitimate primary identity refused", err)
		}
		return l
	}
	lifecycle := newLifecycle()
	refs := []v.ContentRef{alphaRef, alphaRef}
	refs[0].ContentID = "scan-deadline-first"
	refs[1].ContentID = "scan-deadline-next"
	refByObject := map[string]v.ContentRef{}
	original := make([]content.BodyCleanupObservation, len(refs))
	requests := make([]v.ContentPutRequest, len(refs))
	receipts := make([][]byte, len(refs))
	for i, ref := range refs {
		installContentPolicy(t, ctx, w, ref)
		requests[i] = contentPut(t, ref, fmt.Sprintf("scan-deadline-publish-%d", i), "YWxwaGEK")
		receipt := putContentRequest(t, ctx, service, requests[i])
		if _, ok := receipt.AsAccepted(); !ok {
			t.Fatal("normal counterpart admission refused")
		}
		var err error
		receipts[i], err = v.Encode(receipt)
		if err != nil {
			t.Fatal(err)
		}
		if worked, err := service.Step(ctx); err != nil || !worked {
			t.Fatal("normal counterpart publication failed", worked, err)
		}
		assertContentBody(t, ctx, service, ref, nil, "alpha\n")
		request := content.SealRequest{Ref: ref, Purpose: "verification", SealID: fmt.Sprintf("清理 \" \\ <>& \u2028-%d", i), Deadline: time.Now().Add(time.Minute).UTC().Truncate(time.Microsecond)}
		if len(request.SealID) > 128 {
			t.Fatal("normal seal identity exceeds the original interface bound")
		}
		observed, err := lifecycle.Seal(ctx, &contentPrincipal, request)
		if err != nil || observed.Seal.Ref != ref || observed.Seal.ID != request.SealID || observed.Seal.PrimaryHolderID != primary || observed.Seal.PrimaryHolderBinding != w.Objects.Binding() || !observed.Seal.Deadline.Equal(request.Deadline) || observed.CleanupComplete || len(observed.Holders) != 2 {
			t.Fatal("normal counterpart seal/holders incomplete", err)
		}
		original[i] = observed
		id, _, err := content.VersionIdentity(ref)
		if err != nil {
			t.Fatal(err)
		}
		refByObject[id] = ref
	}
	scan := func(limit int) ([]runtime.Job, error) {
		var jobs []runtime.Job
		err := w.Store().Within(ctx, contract.OwnerRef{TenantID: contract.ID(contentOwner.TenantID), OwnerID: contract.ID(contentOwner.OwnerID)}, func(ctx context.Context, tx runtime.Tx) error {
			now, err := w.Store().Now(ctx, tx)
			if err != nil {
				return err
			}
			jobs, err = w.Store().ScanBodyCleanup(ctx, tx, now, contentPrincipal, primary, limit)
			return err
		})
		return jobs, err
	}
	normal, err := scan(2)
	if err != nil || len(normal) != 2 {
		t.Fatal("normal counterpart bounded scan failed", len(normal), err)
	}
	for _, job := range normal {
		if _, ok := refByObject[string(job.Object.ID)]; !ok || job.Phase != "body_cleanup" {
			t.Fatal("normal scan returned an unrelated responsibility")
		}
	}
	first, err := scan(1)
	if err != nil || len(first) != 1 || !reflect.DeepEqual(first[0], normal[0]) {
		t.Fatal("normal counterpart first bounded page changed", err)
	}
	restore := w.CorruptBodyCleanupDeadline(ctx, refByObject[string(first[0].Object.ID)])
	jobs, err := scan(1)
	var goParse *time.ParseError
	var pgParse *pgconn.PgError
	if err == nil || len(jobs) != 0 || (!errors.As(err, &goParse) && !(errors.As(err, &pgParse) && (pgParse.Code == "22007" || pgParse.Code == "22008"))) {
		t.Fatal("malformed deadline lost its parse cause or was silently filtered before the page", len(jobs), err)
	}
	if goParse != nil {
		t.Log("actual malformed-deadline cause: Go time.ParseError")
	} else {
		t.Logf("actual malformed-deadline cause: PostgreSQL SQLSTATE %s", pgParse.Code)
	}
	if err = restore(); err != nil {
		t.Fatal("fault restoration / original connection Close failed", err)
	}
	after, err := scan(2)
	if err != nil || !reflect.DeepEqual(after, normal) {
		t.Fatal("restored normal counterpart changed original responsibilities", err)
	}
	for i, ref := range refs {
		assertBacklogPhysicalBody(t, w, ref, "alpha\n")
		observed, err := lifecycle.Observe(ctx, &contentPrincipal, ref, "")
		if err != nil || !sameBacklogPublicObservation(t, observed, original[i]) {
			t.Fatal("scan / fault recovery changed original public seal or holders", err)
		}
	}
	for step := 0; step < 8; step++ {
		complete := true
		for _, ref := range refs {
			observed, err := lifecycle.Observe(ctx, &contentPrincipal, ref, "")
			if err != nil {
				t.Fatal(err)
			}
			complete = complete && observed.CleanupComplete
		}
		if complete {
			break
		}
		if worked, err := lifecycle.Step(ctx, &contentPrincipal); err != nil || !worked {
			t.Fatal("normal original canonical identity did not complete real cleanup", worked, err)
		}
	}
	completed := make([]content.BodyCleanupObservation, len(refs))
	for i, ref := range refs {
		completed[i], err = lifecycle.Observe(ctx, &contentPrincipal, ref, "")
		if err != nil || !completed[i].CleanupComplete || len(completed[i].Holders) != 2 || !sameBacklogPublicObservation(t, content.BodyCleanupObservation{Seal: completed[i].Seal}, content.BodyCleanupObservation{Seal: original[i].Seal}) {
			t.Fatal("normal original seal identity or all-holder ACK changed", err)
		}
		for _, holder := range completed[i].Holders {
			matchedOriginal := false
			for _, old := range original[i].Holders {
				if reflect.DeepEqual(holder.Identity, old.Identity) && holder.Kind == old.Kind && holder.Deadline.Equal(old.Deadline) {
					matchedOriginal = true
				}
			}
			if !matchedOriginal || holder.State != "erased" || holder.Identity.Ref != ref || holder.Identity.SealID != original[i].Seal.ID || !holder.Deadline.Equal(original[i].Seal.Deadline) {
				t.Fatal("normal original holder did not independently ACK its exact duty")
			}
			if holder.Kind == "primary" && (holder.Identity.HolderID != primary || holder.Identity.Binding != original[i].Seal.PrimaryHolderBinding) {
				t.Fatal("normal primary identity or original physical binding changed")
			}
		}
		_, key, err := content.VersionIdentity(ref)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = os.Lstat(filepath.Join(w.Directory, key)); !os.IsNotExist(err) {
			t.Fatal("normal exact body was not physically erased", err)
		}
		metadata := content.MetadataPolicy{Ref: ref, Subject: contentPrincipal, Purpose: "verification", Revision: 1, ValidUntil: time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)}
		if err = lifecycle.InstallMetadataPolicy(ctx, &contentPrincipal, metadata, 0); err != nil {
			t.Fatal(err)
		}
	}
	w.Reopen(ctx)
	service = contentService(t, w)
	lifecycle = newLifecycle()
	for i, ref := range refs {
		observed, err := lifecycle.Observe(ctx, &contentPrincipal, ref, "")
		if err != nil || !sameBacklogPublicObservation(t, observed, completed[i]) {
			t.Fatal("real reopen changed canonical original seal / holder ACK", err)
		}
		command, err := service.GetCommand(ctx, contentCommandGetWire(t, requests[i].CommandID), &contentPrincipal)
		found, ok := command.AsFound()
		progress, hasProgress := found.Progress.AsContent()
		if err != nil || !ok || !hasProgress || progress.ContentRef != ref || progress.Publication != "published" {
			t.Fatal("normal cleanup changed original published history", err)
		}
		receipt, err := v.Encode(found.Receipt)
		if err != nil || string(receipt) != string(receipts[i]) {
			t.Fatal("normal cleanup changed original fixed receipt", err)
		}
		view, err := service.Get(ctx, contentGetWire(t, ref, nil), &contentPrincipal)
		gone, ok := view.AsGone()
		if err != nil || !ok || gone.ContentRef != ref || gone.EvidenceAvailable {
			t.Fatal("normal current authorized metadata does not reflect exact physical absence", err)
		}
	}
}
