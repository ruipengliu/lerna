//go:build integration

package component_test

import (
	"context"
	"errors"
	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	"github.com/ruipengliu/lerna/domain/content"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestContentActualStagingAndPrimaryErasureAllowsOnlyAuthorizedGone(t *testing.T) {
	for _, state := range []string{"preparing", "published"} {
		t.Run(state, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			w := fixture.New(t, ctx)
			installContentPolicy(t, ctx, w, alphaRef)
			service := contentService(t, w)
			req := contentPut(t, alphaRef, "cleanup-original", "YWxwaGEK")
			if _, ok := putContentRequest(t, ctx, service, req).AsAccepted(); !ok {
				t.Fatal("normal content refused")
			}
			if state == "published" {
				if _, err := service.Step(ctx); err != nil {
					t.Fatal(err)
				}
				assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
			}
			l := bodyLifecycle(t, w)
			staging, err := l.ObserveStaging(ctx, &contentPrincipal, alphaRef)
			if err != nil || staging.Ref != alphaRef || staging.Present != (state == "preparing") {
				t.Fatalf("independent original staging observation: %+v %v", staging, err)
			}
			request := content.SealRequest{Ref: alphaRef, Purpose: "verification", SealID: "cleanup-original", Deadline: time.Now().Add(time.Minute).UTC().Truncate(time.Microsecond)}
			if _, err = l.Seal(ctx, &contentPrincipal, request); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 4; i++ {
				worked, err := l.Step(ctx, &contentPrincipal)
				if err != nil {
					t.Fatal(err)
				}
				if !worked {
					break
				}
			}
			w.Reopen(ctx)
			l = bodyLifecycle(t, w)
			observation, err := l.Observe(ctx, &contentPrincipal, alphaRef, "")
			if err != nil || !observation.CleanupComplete || len(observation.Holders) != 2 {
				t.Fatalf("actual holder ACK: %+v %v", observation, err)
			}
			for _, holder := range observation.Holders {
				if holder.State != "erased" {
					t.Fatal("holder was not independently erased", holder)
				}
			}
			staging, err = l.ObserveStaging(ctx, &contentPrincipal, alphaRef)
			if err != nil || staging.Present {
				t.Fatalf("staging bytes retained after erase: %+v %v", staging, err)
			}
			_, key, err := content.VersionIdentity(alphaRef)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = os.ReadFile(filepath.Join(w.Directory, key)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("independent final body remains", err)
			}
			service = contentService(t, w)
			view, err := service.Get(ctx, contentGetWire(t, alphaRef, nil), &contentPrincipal)
			denied, ok := view.AsRejected()
			if err != nil || !ok || denied.Reason != "forbidden" {
				t.Fatalf("gone leaked without metadata qualification: %+v %v", view, err)
			}
			metadata := content.MetadataPolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 1, ValidUntil: time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)}
			if err = l.InstallMetadataPolicy(ctx, &contentPrincipal, metadata, 0); err != nil {
				t.Fatal(err)
			}
			view, err = service.Get(ctx, contentGetWire(t, alphaRef, nil), &contentPrincipal)
			gone, ok := view.AsGone()
			if err != nil || !ok || gone.ContentRef != alphaRef || gone.EvidenceAvailable {
				t.Fatalf("authorized exact minimal gone: %+v %v", view, err)
			}
		})
	}
}
