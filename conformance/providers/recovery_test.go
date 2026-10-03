package providers_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ruipengliu/lerna/adapters/providers"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

func TestLostPhysicalResponseNeverRetriesOrQueriesByNewIdentity(t *testing.T) {
	var sends atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		connection, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = connection.Close()
	}))
	defer server.Close()
	cfg := configuration(t, server.URL+"/v1/chat/completions")
	engine, err := providers.NewOpenAI(cfg)
	if err != nil {
		t.Fatal(err)
	}
	enc := encoded(t, engine, cfg)
	call := api.NewID("call")
	if _, err = engine.Request(context.Background(), call, enc); !api.IsCode(err, "effect_unknown") {
		t.Fatalf("lost response: %v", err)
	}
	restored, err := providers.NewOpenAI(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err = restored.Lookup(context.Background(), call, enc); !api.IsCode(err, "effect_unknown") {
			t.Fatalf("original lookup: %v", err)
		}
		if _, err = restored.Request(context.Background(), call, enc); !api.IsCode(err, "effect_unknown") {
			t.Fatalf("repeat original: %v", err)
		}
	}
	view, err := restored.Call(context.Background(), call)
	if err != nil || view.Status != "send_started" || view.ResponseDigest != "" {
		t.Fatalf("durable uncertain original: %+v %v", view, err)
	}
	if sends.Load() != 1 {
		t.Fatalf("implicit physical retry: %d", sends.Load())
	}
}

func TestCommitUnknownSendMarkerStopsOutboundAndReplyCanRecover(t *testing.T) {
	for _, phase := range []string{"send_marker", "original_reply"} {
		t.Run(phase, func(t *testing.T) {
			var sends atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				_, _ = w.Write(providerReply(t))
			}))
			defer server.Close()
			var armed atomic.Bool
			var commits atomic.Int64
			failAt := int64(1)
			if phase == "original_reply" {
				failAt = 2
			}
			cfg := configuration(t, server.URL+"/v1/chat/completions", sqlite.WithCommitFault(func(when sqlite.CommitPhase) error {
				if armed.Load() && when == sqlite.AfterCommit && commits.Add(1) == failAt {
					return errors.New("contract: lost COMMIT reply")
				}
				return nil
			}))
			engine, err := providers.NewOpenAI(cfg)
			if err != nil {
				t.Fatal(err)
			}
			enc := encoded(t, engine, cfg)
			call := api.NewID("call")
			armed.Store(true)
			if _, err = engine.Request(context.Background(), call, enc); !errors.Is(err, runtime.ErrCommitUnknown) {
				t.Fatalf("unknown original commit: %v", err)
			}
			armed.Store(false)
			restored, err := providers.NewOpenAI(cfg)
			if err != nil {
				t.Fatal(err)
			}
			out, err := restored.Lookup(context.Background(), call, enc)
			if phase == "send_marker" {
				if !api.IsCode(err, "effect_unknown") || sends.Load() != 0 {
					t.Fatalf("unknown marker must stop physical send: %v sends=%d", err, sends.Load())
				}
			} else if err != nil || out.Draft.Kind != "complete" || sends.Load() != 1 {
				t.Fatalf("original reply recovery: %+v %v sends=%d", out, err, sends.Load())
			}
		})
	}
}

func TestReceiverRedirectAndChangedOriginalAreClosed(t *testing.T) {
	var receiverSends atomic.Int64
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { receiverSends.Add(1); _, _ = w.Write(providerReply(t)) }))
	defer other.Close()
	var sends atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	cfg := configuration(t, server.URL)
	engine, err := providers.NewOpenAI(cfg)
	if err != nil {
		t.Fatal(err)
	}
	enc := encoded(t, engine, cfg)
	changed := enc
	changed.Location = "device"
	if _, err = engine.Request(context.Background(), api.NewID("call"), changed); !api.IsCode(err, "forbidden") || sends.Load() != 0 {
		t.Fatalf("receiver/location before physical send: %v", err)
	}
	call := api.NewID("call")
	if _, err = engine.Request(context.Background(), call, enc); !api.IsCode(err, "effect_unknown") {
		t.Fatalf("redirect original unknown: %v", err)
	}
	if sends.Load() != 1 || receiverSends.Load() != 0 {
		t.Fatalf("redirect forwarded credential/call: sends=%d receiver=%d", sends.Load(), receiverSends.Load())
	}
	changed = encoded(t, engine, cfg)
	if _, err = engine.Request(context.Background(), call, changed); !api.IsCode(err, "idempotency_conflict") || sends.Load() != 1 {
		t.Fatalf("original identity changed payload: %v", err)
	}
}
