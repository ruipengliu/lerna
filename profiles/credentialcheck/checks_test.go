package credentialcheck

import (
	"context"
	"encoding/base64"
	"net/http/httptrace"
	"strings"
	"sync"
	"testing"
)

func TestCredentialBoundaries(t *testing.T) {
	for _, name := range caseNames {
		t.Run(name, func(t *testing.T) {
			if e := Check(context.Background(), name); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestSDKExecutionUsesCredentialAndObservesTarget(t *testing.T) {
	if e := executionCase(context.Background()); e != nil {
		t.Fatal(e)
	}
}

func TestHTTPTraceCannotObserveCredentialHeader(t *testing.T) {
	f, e := newFixture(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer f.close()
	var mu sync.Mutex
	var seen string
	ctx := httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{WroteHeaderField: func(_ string, values []string) { mu.Lock(); defer mu.Unlock(); seen += strings.Join(values, ",") }})
	if e = f.use(ctx, "credential", "trace"); e != nil {
		t.Fatal(e)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Contains(seen, fixtureSecret) || strings.Contains(seen, base64.RawStdEncoding.EncodeToString([]byte(fixtureSecret))) {
		t.Fatal("credential escaped through HTTP tracing")
	}
}
