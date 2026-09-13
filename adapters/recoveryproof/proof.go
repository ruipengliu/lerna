// Package recoveryproof authenticates fresh collection recovery metadata.
// Signing keys and verifier configuration belong to the live host and must be
// provided independently of the database being restored.
package recoveryproof

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"time"
	"unicode/utf8"

	"lerna/memory"
)

type IssuerConfig struct {
	Authority string
	Epoch     uint64
	Scope     memory.RecoveryScope
	Key       ed25519.PrivateKey
}
type VerifierConfig struct {
	Authority string
	Epoch     uint64
	Scope     memory.RecoveryScope
	Key       ed25519.PublicKey
	// This floor is current trusted host state, never read from a backup proof.
	MinPosition uint64
}
type Issuer struct {
	source memory.RecoveryStateSource
	config IssuerConfig
}
type Verifier struct{ config VerifierConfig }

func valid(authority string, epoch uint64, scope memory.RecoveryScope) bool {
	return len(authority) > 0 && len(authority) <= 256 && utf8.ValidString(authority) && epoch > 0 && memory.ValidRecoveryScope(scope)
}
func NewIssuer(source memory.RecoveryStateSource, config IssuerConfig) (*Issuer, error) {
	if source == nil || !valid(config.Authority, config.Epoch, config.Scope) || len(config.Key) != ed25519.PrivateKeySize {
		return nil, memory.Invalid
	}
	if !bytes.Equal(config.Key, ed25519.NewKeyFromSeed(config.Key.Seed())) {
		return nil, memory.Invalid
	}
	config.Key = append(ed25519.PrivateKey(nil), config.Key...)
	return &Issuer{source: source, config: config}, nil
}
func NewVerifier(config VerifierConfig) (*Verifier, error) {
	if !valid(config.Authority, config.Epoch, config.Scope) || len(config.Key) != ed25519.PublicKeySize || config.MinPosition > 1<<32 {
		return nil, memory.Invalid
	}
	config.Key = append(ed25519.PublicKey(nil), config.Key...)
	return &Verifier{config: config}, nil
}

func message(proof memory.RecoveryProof) ([]byte, error) {
	// Arrays have explicit order and there are no maps. These exact versioned
	// bytes define the signature; no transport serialization is presumed canonical.
	snapshot := proof.Snapshot
	if snapshot.Records == nil {
		snapshot.Records = []memory.RecoveryRecord{}
	}
	if snapshot.Deleted == nil {
		snapshot.Deleted = []memory.VersionRef{}
	}
	if snapshot.Operations == nil {
		snapshot.Operations = []memory.Receipt{}
	}
	if snapshot.Erased == nil {
		snapshot.Erased = []memory.SourceEvent{}
	}
	if snapshot.SourceFences == nil {
		snapshot.SourceFences = []memory.SourceErasure{}
	}
	unsigned := struct {
		Authority string
		Epoch     uint64
		Challenge memory.RecoveryChallenge
		Snapshot  memory.RecoverySnapshot
	}{proof.Authority, proof.Epoch, proof.Challenge, snapshot}
	raw, err := json.Marshal(unsigned)
	if err != nil || len(raw) > 8<<20 {
		return nil, memory.Capacity
	}
	return append([]byte("memory-recovery-proof-v3\n"), raw...), nil
}

func (i *Issuer) ProveRecovery(ctx context.Context, challenge memory.RecoveryChallenge) (memory.RecoveryProof, error) {
	if challenge.Scope != i.config.Scope || challenge.Nonce == ([32]byte{}) || challenge.After > 1<<32 {
		return memory.RecoveryProof{}, memory.Denied
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// Always acquire a new transaction snapshot for this challenge. Never wrap
	// an archived proof or a cached pre-deletion snapshot in a fresh signature.
	snapshot, err := i.source.RecoverySnapshot(ctx, challenge.Scope, challenge.After)
	if err != nil {
		return memory.RecoveryProof{}, err
	}
	if snapshot.Scope != challenge.Scope || snapshot.After != challenge.After || !memory.ValidRecoverySnapshot(snapshot) {
		return memory.RecoveryProof{}, memory.Unavailable
	}
	proof := memory.RecoveryProof{Authority: i.config.Authority, Epoch: i.config.Epoch, Challenge: challenge, Snapshot: snapshot}
	raw, err := message(proof)
	if err != nil {
		return memory.RecoveryProof{}, err
	}
	if err = ctx.Err(); err != nil {
		return memory.RecoveryProof{}, err
	}
	proof.Signature = ed25519.Sign(i.config.Key, raw)
	return proof, nil
}

func (v *Verifier) VerifyRecovery(challenge memory.RecoveryChallenge, proof memory.RecoveryProof) error {
	if challenge.Scope != v.config.Scope || challenge.Nonce == ([32]byte{}) || proof.Challenge != challenge || proof.Authority != v.config.Authority || proof.Epoch != v.config.Epoch || proof.Snapshot.Scope != v.config.Scope || proof.Snapshot.After != challenge.After || proof.Snapshot.Position < v.config.MinPosition || len(proof.Signature) != ed25519.SignatureSize || !memory.ValidRecoverySnapshot(proof.Snapshot) {
		return memory.Denied
	}
	raw, err := message(proof)
	if err != nil || !ed25519.Verify(v.config.Key, raw, proof.Signature) {
		return memory.Denied
	}
	return nil
}

func (v *Verifier) Configuration() [32]byte {
	raw, _ := json.Marshal(v.config)
	return sha256.Sum256(append([]byte("memory-recovery-verifier-v3\n"), raw...))
}

var _ memory.RecoverySource = (*Issuer)(nil)
var _ memory.RecoveryVerifier = (*Verifier)(nil)
