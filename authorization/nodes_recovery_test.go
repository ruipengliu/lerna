package authorization_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"lerna/adapters/authorization/josegrant"
	sqliteauth "lerna/adapters/authorization/sqlite"
	"lerna/adapters/transport/nodetls"
	"lerna/authorization"
)

type nodeCrashStore struct{ authorization.Store }

func (s nodeCrashStore) Commit(ctx context.Context, v uint64, st authorization.State) error {
	err := s.Store.Commit(ctx, v, st)
	if err == nil && st.Nodes != nil && len(st.Nodes.Operations) > 0 {
		os.Exit(73)
	}
	return err
}
func TestNodeProcessProbe(t *testing.T) {
	dir := os.Getenv("HARNESS_NODE_PROBE_DIR")
	if dir == "" {
		t.Skip("subprocess only")
	}
	ctx := context.Background()
	db, err := sqliteauth.Open(filepath.Join(dir, "authority.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	key, err := josegrant.LoadPrivateKey(filepath.Join(dir, "issuer.pem"))
	if err != nil {
		t.Fatal(err)
	}
	der, err := os.ReadFile(filepath.Join(dir, "issuer.der"))
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := nodetls.NewIssuer(tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key})
	if err != nil {
		t.Fatal(err)
	}
	token, err := os.ReadFile(filepath.Join(dir, "administrator"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	var in authorization.NodeMutation
	if err = json.Unmarshal(raw, &in); err != nil {
		t.Fatal(err)
	}
	clock := &grantClock{time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)}
	var store authorization.Store = db
	var signer authorization.NodeIssuer = issuer
	if os.Getenv("HARNESS_NODE_PROBE_PHASE") == "prepared" {
		signer = interruptedNodeIssuer{issuer, func() { os.Exit(73) }}
	} else {
		store = nodeCrashStore{db}
	}
	svc, err := authorization.New(store, clock, config())
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := svc.Nodes(authorization.NodeConfig{MaxTTL: time.Hour, RequestTTL: time.Minute, IOTimeout: time.Second, MaxRecords: 16, MaxOperations: 64}, signer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = nodes.Mutate(ctx, string(token), in); err != nil {
		t.Fatal(err)
	}
	t.Fatal("did not reach crash checkpoint")
}
func TestNodeProcessRecovery(t *testing.T) {
	for _, phase := range []string{"prepared", "committed"} {
		t.Run(phase, func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			path := filepath.Join(dir, "authority.db")
			db, err := sqliteauth.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			clock := &grantClock{time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)}
			svc, err := authorization.New(db, clock, config())
			if err != nil {
				t.Fatal(err)
			}
			token, err := svc.Bootstrap(ctx, "local", "admin")
			if err != nil {
				t.Fatal(err)
			}
			op, err := svc.NewOperation(ctx, token)
			if err != nil {
				t.Fatal(err)
			}
			in := authorization.NodeMutation{OperationID: op, Namespace: "local", Node: "node", Kind: "ENROLL", Subjects: []string{"admin"}, RequestExpires: clock.now.Add(time.Minute).Unix(), Expires: clock.now.Add(time.Hour).Unix()}
			key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			in.CSR, err = nodetls.Request(key, in)
			if err != nil {
				t.Fatal(err)
			}
			ca, err := nodetls.GenerateCA(clock.now, 2*time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			caKey, err := x509.MarshalPKCS8PrivateKey(ca.PrivateKey)
			if err != nil {
				t.Fatal(err)
			}
			request, err := json.Marshal(in)
			if err != nil {
				t.Fatal(err)
			}
			for name, data := range map[string][]byte{"administrator": []byte(token), "request.json": request, "issuer.pem": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: caKey}), "issuer.der": ca.Certificate[0]} {
				if err = os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err = db.Close(); err != nil {
				t.Fatal(err)
			}
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			child := exec.CommandContext(bounded, executable, "-test.run=^TestNodeProcessProbe$")
			child.Env = append(os.Environ(), "HARNESS_NODE_PROBE_DIR="+dir, "HARNESS_NODE_PROBE_PHASE="+phase)
			output, err := child.CombinedOutput()
			exit, ok := err.(*exec.ExitError)
			if !ok || exit.ExitCode() != 73 {
				t.Fatalf("checkpoint: %v %s", err, output)
			}
			db, err = sqliteauth.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			svc, err = authorization.New(db, clock, config())
			if err != nil {
				t.Fatal(err)
			}
			issuer, _ := nodetls.NewIssuer(ca)
			nodes, err := svc.Nodes(authorization.NodeConfig{MaxTTL: time.Hour, RequestTTL: time.Minute, IOTimeout: time.Second, MaxRecords: 16, MaxOperations: 64}, issuer)
			if err != nil {
				t.Fatal(err)
			}
			saved, err := nodes.LookupOperation(ctx, token, op)
			if phase == "prepared" {
				if !authorization.Is(err, authorization.NotFound) {
					t.Fatalf("prepared certificate published: %v", err)
				}
			} else if err != nil || len(saved.Certificate) == 0 {
				t.Fatalf("committed registration lost: %v", err)
			}
			restored, err := nodes.Mutate(ctx, token, in)
			if err != nil {
				t.Fatal(err)
			}
			if phase == "committed" && restored.CertificateSHA256 != saved.CertificateSHA256 {
				t.Fatal("recovery issued a second certificate")
			}
			if err = nodes.Check(ctx, "local", "node", restored.CertificateSHA256, "admin"); err != nil {
				t.Fatal(err)
			}
		})
	}
}
