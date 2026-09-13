// Package credentialcheck validates real local credential storage, authority and
// HTTP target effects. Test credentials are public fixtures, never user keys.
package credentialcheck

import (
	"context"
	"lerna/conformance"
)

const ProfileID = "bound-credentials-v1"

func Profile() conformance.Profile {
	p := conformance.Profile{ID: ProfileID, Version: "1", Configuration: map[string]string{"cipher": "Go AES-256-GCM, 96-bit durable per-key nonce counter; full binding/version/expiry AAD", "storage": "SQLite ciphertext; independent Linux 0700 directory and 0600 versioned key file", "limits": "64 credentials, 4096 bytes each, 8 retained key versions, 2^20 seals/key, 4 concurrent uses, 1s broker/HTTP timeout", "target": "loopback HTTP stateful counter with independent SQLite effects; formal SDK Execution case included"}, Limitations: []string{"Public fixture credentials only; no external service, model or personal credential is used.", "The private-file key adapter supports Linux; other platforms must bind a different KeySource.", "File permissions do not defend a compromised host or joint key/ciphertext disclosure. Key-directory rollback is unsupported; restore operations must preserve its nonce high-water mark.", "Credential database backup restoration requires trusted revision reconciliation before activation; AEAD alone does not detect replay of a whole valid old record.", "Separate package tests verify process-exit nonce recovery, concurrency, key capacity and CLI management; they are not counted again as profile cases."}}
	for _, mode := range append(append([]string{}, caseNames...), "sdk-execution", "independent") {
		mode := mode
		p.Cases = append(p.Cases, conformance.Case{ID: mode, Required: true, Evidence: "real_ciphertext_authorization_bound_http_effect", Input: mode, Expected: "verified", Check: func(ctx context.Context) (string, error) {
			var e error
			if mode == "sdk-execution" {
				e = executionCase(ctx)
			} else if mode == "independent" {
				e = independentCase(ctx)
			} else {
				e = Check(ctx, mode)
			}
			if e != nil {
				return "", e
			}
			return "verified", nil
		}})
	}
	return p
}
