package providers_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/ruipengliu/lerna/adapters/providers"
	"github.com/ruipengliu/lerna/api"
)

type constructorTransportProbe struct{ outbound atomic.Uint64 }

func (p *constructorTransportProbe) RoundTrip(*http.Request) (*http.Response, error) {
	p.outbound.Add(1)
	return nil, api.E("dependency_unavailable", "unexpected_constructor_outbound")
}

func TestNewOpenAIRejectsUnsupportedDefaultTransportWithoutPanicOrOutbound(t *testing.T) {
	probe := &constructorTransportProbe{}
	original := http.DefaultTransport
	http.DefaultTransport = probe
	t.Cleanup(func() { http.DefaultTransport = original })
	cfg := configuration(t, "https://provider.example/v1/chat/completions")
	engine, err := providers.NewOpenAI(cfg)
	if engine != nil {
		if closeErr := engine.Close(); closeErr != nil {
			t.Error(closeErr)
		}
		t.Fatal("unqualified transport unexpectedly enabled a physical model profile")
	}
	var refusal *api.Error
	if !errors.As(err, &refusal) || refusal.Code != "unsupported" || refusal.Reason != "model_transport_contract_unconfigured" {
		t.Fatalf("unsupported transport must be a classified configuration refusal: %v", err)
	}
	if probe.outbound.Load() != 0 {
		t.Fatalf("configuration refusal sent %d requests", probe.outbound.Load())
	}
}

func TestNewOpenAIRejectsNilDefaultTransportWithoutPanic(t *testing.T) {
	var typedNil *http.Transport
	for _, transport := range []http.RoundTripper{nil, typedNil} {
		original := http.DefaultTransport
		http.DefaultTransport = transport
		func() {
			defer func() { http.DefaultTransport = original }()
			cfg := configuration(t, "https://provider.example/v1/chat/completions")
			engine, err := providers.NewOpenAI(cfg)
			if engine != nil {
				if closeErr := engine.Close(); closeErr != nil {
					t.Error(closeErr)
				}
				t.Fatal("nil transport enabled an unqualified model profile")
			}
			if !api.IsCode(err, "unsupported") {
				t.Fatalf("nil transport did not return a configuration refusal: %v", err)
			}
		}()
	}
}

func TestNewOpenAIQualifiedTransportStillConstructsWithoutNetworkIO(t *testing.T) {
	original := http.DefaultTransport
	base, ok := original.(*http.Transport)
	if !ok || base == nil {
		t.Fatal("test requires its original qualified default transport")
	}
	var dials atomic.Uint64
	qualified := base.Clone()
	qualified.DialContext = func(context.Context, string, string) (net.Conn, error) {
		dials.Add(1)
		return nil, api.E("dependency_unavailable", "unexpected_constructor_dial")
	}
	http.DefaultTransport = qualified
	t.Cleanup(func() { http.DefaultTransport = original; qualified.CloseIdleConnections() })
	cfg := configuration(t, "https://provider.example/v1/chat/completions")
	engine, err := providers.NewOpenAI(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = engine.Close(); err != nil {
		t.Fatal(err)
	}
	if dials.Load() != 0 {
		t.Fatalf("qualified constructor dialed %d times", dials.Load())
	}
}
