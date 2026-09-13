package grants

import (
	"context"
	"lerna/adapters/sqliteauth"
	"lerna/conformance"
	"os"
	"path/filepath"
)

const ID = "restricted-grants-v1"

func Profile(executable string) (conformance.Profile, error) {
	dir, err := os.MkdirTemp("", "lerna-grants-version-")
	if err != nil {
		return conformance.Profile{}, err
	}
	defer os.RemoveAll(dir)
	db, err := sqliteauth.Open(filepath.Join(dir, "version.db"))
	if err != nil {
		return conformance.Profile{}, err
	}
	version, err := db.Version(context.Background())
	db.Close()
	if err != nil {
		return conformance.Profile{}, err
	}
	p := conformance.Profile{ID: ID, Version: "1", Method: "contractcheck -profile " + ID, Configuration: map[string]string{"jose": "github.com/go-jose/go-jose/v4 v4.1.5; ES256; compact JWS; harness-grant+jwt;v=1", "storage": "real SQLite; WAL/FULL; shared authorization/runtime CAS", "sqlite_runtime": version, "limits": "TTL 1h; IO 1s; depth 4; 64 grants; 64 use reservations; 128 operations including reserved revocation space; 16 delegate targets; 32768-byte material; units <=1000000", "identity": "locally authenticated administrator; verified fixture certificate; pinned signing key", "clock": "controlled 2026-09-10; trusted authority time; no remote offline mode", "process_faults": "five child exit(73) boundaries; <=10s each"}, Limitations: []string{"No real mTLS transport, node enrollment, remote synchronization or offline freshness guarantee.", "Signature encoding is cross-checked using independent Go crypto/ecdsa; certificates are locally generated and chain-verified fixtures, not real device evidence.", "UsePermit allocates quota before delivery; a separate SQLite fixture commits a simulated effect with consumption. Production business consumers are not implemented.", "Recipient confirmation is a trusted adapter seam, not an untrusted RPC or proof from a transport acknowledgement.", "No automatic quota refunds, record cleanup, key rotation or second durable backend. Lost usage remains allocated.", "Host key sources, clock and storage are trusted. This does not prove safety against compromised hosts, power loss or restored stale backups."}}
	for _, name := range checks {
		p.Cases = append(p.Cases, conformance.Case{ID: name, Required: true, Evidence: "restricted_grant_contract", Input: name, Expected: "verified", Check: func(ctx context.Context) (string, error) {
			if err := check(ctx, name); err != nil {
				return "not verified", err
			}
			return "verified", nil
		}})
	}
	for _, point := range crashPoints {
		p.Cases = append(p.Cases, conformance.Case{ID: point, Required: true, Evidence: "sqlite_grant_process_recovery", Input: point, Expected: "verified", Check: func(ctx context.Context) (string, error) {
			if err := crashCheck(ctx, executable, point); err != nil {
				return "not verified", err
			}
			return "verified", nil
		}})
	}
	return p, nil
}
