package catalogcheck

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"lerna/adapters/simworkflow"
	"lerna/conformance"
	"sync"
)

const ID = "capability-catalog-v1"

func Profile(executable string) (conformance.Profile, error) {
	defs, e := simworkflow.Definitions()
	if e != nil {
		return conformance.Profile{}, e
	}
	raw, _ := json.Marshal(defs)
	digest := fmt.Sprintf("%x", sha256.Sum256(raw))
	p := conformance.Profile{ID: ID, Version: "1", Configuration: map[string]string{"catalog": "1008 APIs; 168 business types, each six state/effect transitions; single full SQLite authority directory", "manifest_sha256": digest, "execution": "168 fixed business namespace authorities; six exact partitions each; at most one business runtime resident; durable state retained on eviction", "discovery": "List/Search/Describe local binary SDK; real authorization; keyword/category/alias; no model or expected ref input", "limits": "2048 active declarations; 4096 retained; 64KiB/declaration; <=4096 metadata rows; <=2048 authorized scoring documents; 32 candidates; 64/page; zero query schemas; one exact schema pair per Describe; 1s query IO; 4 query slots; index batch64; runtime six services, each operations32; task100, content64/2MiB, signed grants64; target64 operations/records", "storage_format": "existing execution Format2 unchanged; independent business authority namespaces; no conversion of existing local namespace", "model": "none"}, Limitations: []string{"Sequential local simulated business workflow fixture; not 1008 vendors, concurrent tasks, or third-party integrations.", "A business type is counted with six explicit state/effect operations; aliases, versions and deployment instances are excluded. Shared engine uses authored typed quantity, constraint, verification and accounting rules.", "Queries read bounded full metadata for pre-candidate authorization; work budget counts only authorized candidate documents, never hidden rows. No bulk schema loads in queries.", "90% first-call and 95% bounded-retry model quality are not measured here; independent full quality benchmark remains future work.", "Reference runtime manager is serial and host-authenticated; no network tenant provisioning or automatic authority migration."}}
	var mu sync.Mutex
	var f *fixture
	var initErr error
	for i, def := range defs {
		for j, action := range simworkflow.Actions {
			index := i*6 + j
			entry := def.Entries()[j]
			p.Cases = append(p.Cases, conformance.Case{ID: entry.Ref.Name, Required: true, Evidence: "full_directory_sdk_business_effect", Input: action + " " + def.Title + "; exact declaration " + entry.Ref.Digest, Expected: "verified", Check: func(ctx context.Context) (string, error) {
				mu.Lock()
				defer mu.Unlock()
				if f == nil && initErr == nil {
					f, initErr = newFixture(ctx)
				}
				if initErr != nil {
					return "", initErr
				}
				err := f.call(ctx, index)
				if index == 1007 {
					f.close()
					f = nil
				}
				if err != nil {
					return "", err
				}
				return "verified", nil
			}})
		}
	}
	for _, name := range boundaryNames {
		p.Cases = append(p.Cases, conformance.Case{ID: name, Required: true, Evidence: "catalog_boundary", Input: name, Expected: "verified", Check: func(ctx context.Context) (string, error) {
			if e := boundary(ctx, name); e != nil {
				return "", e
			}
			return "verified", nil
		}})
	}
	for _, name := range dataPolicyNames {
		p.Cases = append(p.Cases, conformance.Case{ID: name, Required: true, Evidence: "independent_source_policy", Input: name + "; catalog authorization remains allowed", Expected: "verified", Check: func(ctx context.Context) (string, error) {
			if e := dataPolicyCheck(ctx, name); e != nil {
				return "", e
			}
			return "verified", nil
		}})
	}
	for _, point := range crashNames {
		p.Cases = append(p.Cases, conformance.Case{ID: point, Required: true, Evidence: "real_sqlite_child_crash", Input: point + "; child exit73; same codecs and durable targets", Expected: "verified", Check: func(ctx context.Context) (string, error) {
			if e := recovery(ctx, executable, point); e != nil {
				return "", e
			}
			return "verified", nil
		}})
	}
	return p, nil
}
