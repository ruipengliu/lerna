package grpc_test

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	harness "github.com/ruipengliu/lerna/sdk/go"
)

type delayedEndpointProofs struct {
	base  originalDeliveryProofs
	until time.Time
}

func (p delayedEndpointProofs) ReadDeliveryProof(ctx context.Context, ref api.ContentRef) ([]byte, error) {
	if delay := time.Until(p.until); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return p.base.ReadDeliveryProof(ctx, ref)
}

func TestGoWSEndpointClosesMissingAuthorityAndWrongScopeBeforeConnection(t *testing.T) {
	s := newSDKEndpointSetup(t, newUnaryFixture(t))
	journal, err := harness.OpenReplyJournal(t.TempDir(), s.discovery.IdentityScope, s.reg.EndpointID, s.reg.InstanceID, s.reg.Generation)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	receiver := &sdkEndpointReceiver{f: s.f, journal: journal}
	for _, field := range []string{"scope", "profile", "key", "current", "receiver"} {
		t.Run(field, func(t *testing.T) {
			cfg := s.config(journal, receiver)
			switch field {
			case "scope":
				cfg.IdentityScope = api.NewID("scope")
			case "profile":
				cfg.Profile = "unregistered-profile"
			case "key":
				cfg.Keys = nil
			case "current":
				cfg.Current = nil
			case "receiver":
				cfg.Receiver = nil
			}
			if _, err = harness.DialWebSocketEndpointWithHTTP(context.Background(), s.address, s.f.token, s.discovery, false, s.httpClient, cfg); err == nil {
				t.Fatal("missing/wrong endpoint authority accepted")
			}
			if len(s.router.States()) != 0 || receiver.invoked.Load() != 0 {
				t.Fatal("closed endpoint configuration reached a connection or handler")
			}
		})
	}
}

func TestGoWSEndpointRejectsWrongKeyExpiryAndCurrentRevocationBeforeHandler(t *testing.T) {
	for _, reason := range []string{"key", "expired", "current_barrier"} {
		t.Run(reason, func(t *testing.T) {
			s := newSDKEndpointSetup(t, newUnaryFixture(t))
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			journal, err := harness.OpenReplyJournal(t.TempDir(), s.discovery.IdentityScope, s.reg.EndpointID, s.reg.InstanceID, s.reg.Generation)
			if err != nil {
				t.Fatal(err)
			}
			defer journal.Close()
			s.owner.journal = journal
			receiver := &sdkEndpointReceiver{f: s.f, journal: journal}
			cfg := s.config(journal, receiver)
			until := time.Now().Add(time.Minute)
			wantCode := "forbidden"
			if reason == "key" {
				other, err := platform.NewDevelopmentKey(s.f.auth.TenantID, s.f.owner, []string{"delivery"})
				if err != nil {
					t.Fatal(err)
				}
				cfg.Keys = map[string]*ecdsa.PublicKey{"development-es256": other.Keys["development-es256"].Public}
			} else if reason == "expired" {
				until = time.Now().Add(time.Second)
				cfg.Proofs = delayedEndpointProofs{originalDeliveryProofs(s.proofRoot), until.Add(10 * time.Millisecond)}
				wantCode = "expired"
			} else {
				cfg.Current = func(ctx context.Context) error {
					entries, _, err := journal.Incomplete(ctx, 32)
					if err != nil {
						return err
					}
					for _, entry := range entries {
						if entry.Invocation != nil && entry.Invocation.Phase == "started" {
							return api.E("forbidden", "current_recipient_revoked_at_handler_entry")
						}
					}
					return s.f.identity.CheckCurrent(ctx, s.f.auth)
				}
			}
			ws, err := harness.DialWebSocketEndpointWithHTTP(ctx, s.address, s.f.token, s.discovery, false, s.httpClient, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			d := signedStaticDelivery(t, s.f, s.reg, s.keys, s.proofRoot, until)
			if err = s.servers[0].Deliver(ctx, s.reg.TenantID, d); err != nil {
				t.Fatal(err)
			}
			for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
				if ws.EndpointError() != nil {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			if err = ws.EndpointError(); !api.IsCode(err, wantCode) {
				t.Fatalf("wrong denial: %v", err)
			}
			if receiver.invoked.Load() != 0 {
				t.Fatal("denied endpoint physically invoked handler")
			}
			entry, err := journal.Invocation(ctx, d.DeliveryID)
			if reason == "current_barrier" {
				if err != nil || entry.Invocation == nil || entry.Invocation.Phase != "started" || entry.ReplyDigest != "" {
					t.Fatal("late current denial lost durable original responsibility")
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Fatal("unsigned/expired new input created invocation responsibility")
			}
		})
	}
}
