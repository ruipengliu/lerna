package credentials

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"reflect"
	"strings"
	"time"
)

type Broker struct {
	store  Store
	keys   KeySource
	auth   Authority
	clock  Clock
	config Config
	slots  chan struct{}
}
type Driver struct {
	broker  *Broker
	binding Binding
	exit    Exit
}

func New(s Store, k KeySource, a Authority, c Clock, cfg Config) (*Broker, error) {
	if s == nil || k == nil || a == nil || c == nil || cfg.Timeout < time.Millisecond || cfg.Timeout > 5*time.Second || cfg.MaxConcurrent < 1 || cfg.MaxConcurrent > 4 {
		return nil, Invalid
	}
	return &Broker{s, k, a, c, cfg, make(chan struct{}, cfg.MaxConcurrent)}, nil
}
func name(s string) bool {
	if len(s) == 0 || len(s) > 256 {
		return false
	}
	for _, r := range s {
		if r < 33 || r > 126 {
			return false
		}
	}
	return true
}
func (b Binding) Valid() bool {
	return name(b.Namespace) && name(b.Subject) && name(b.Driver) && name(b.Service) && name(b.Account) && name(b.Purpose) && name(b.Location)
}
func validRef(s string) bool { return name(s) && len(s) <= 128 && !strings.ContainsAny(s, "/\\") }
func (b *Broker) authorize(ctx context.Context, token string, target Binding, verb string) error {
	if !target.Valid() || len(token) > 4096 {
		return Invalid
	}
	if e := b.auth.Check(ctx, token, target, verb); e != nil {
		return Denied
	}
	return nil
}
func storeError(e error) error {
	switch e {
	case Missing, Conflict, Invalid, Denied, Exhausted:
		return e
	default:
		return Unavailable
	}
}
func (b *Broker) now() (int64, error) {
	v, e := b.clock.Now()
	if e != nil {
		return 0, Unavailable
	}
	return v.Unix(), nil
}
func aad(r Record) []byte { r.Nonce = nil; r.Ciphertext = nil; v, _ := json.Marshal(r); return v }
func crypt(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, KeyUnavailable
	}
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, KeyUnavailable
	}
	a, e := cipher.NewGCM(block)
	if e != nil {
		return nil, KeyUnavailable
	}
	return a, nil
}
func ValidRecord(r Record) bool {
	return validRef(r.Ref) && r.Binding.Valid() && r.Format == 1 && r.Revision > 0 && r.ExpiresUnix > 0 && name(r.KeyVersion) && len(r.Nonce) == 12 && len(r.Ciphertext) > 16 && len(r.Ciphertext) <= 4112
}
func (b *Broker) seal(ctx context.Context, r Record, secret []byte) (Record, error) {
	version, key, nonce, e := b.keys.Reserve(ctx)
	defer clear(key)
	if e != nil {
		if e == Exhausted {
			return Record{}, Exhausted
		}
		return Record{}, KeyUnavailable
	}
	if !name(version) || len(nonce) != 12 {
		return Record{}, KeyUnavailable
	}
	a, e := crypt(key)
	if e != nil {
		return Record{}, e
	}
	r.KeyVersion = version
	r.Nonce = append([]byte(nil), nonce...)
	r.Ciphertext = a.Seal(nil, r.Nonce, secret, aad(r))
	return r, nil
}
func (b *Broker) open(ctx context.Context, r Record) ([]byte, error) {
	if !ValidRecord(r) {
		return nil, Denied
	}
	key, e := b.keys.Read(ctx, r.KeyVersion)
	defer clear(key)
	if e != nil {
		return nil, KeyUnavailable
	}
	a, e := crypt(key)
	if e != nil {
		return nil, e
	}
	plain, e := a.Open(nil, r.Nonce, r.Ciphertext, aad(r))
	if e != nil {
		return nil, Denied
	}
	return plain, nil
}

// Put is a trusted management entry; ordinary task/skill handlers receive only
// Driver. Updating a reference cannot move it to another target binding.
func (b *Broker) Put(ctx context.Context, token, ref string, target Binding, expected uint64, expires int64, secret []byte) (Record, error) {
	ctx, cancel := context.WithTimeout(ctx, b.config.Timeout)
	defer cancel()
	if !validRef(ref) || len(secret) < 1 || len(secret) > 4096 || expected >= 1<<32 {
		return Record{}, Invalid
	}
	if e := b.authorize(ctx, token, target, "manage"); e != nil {
		return Record{}, e
	}
	now, e := b.now()
	if e != nil {
		return Record{}, e
	}
	if expires <= now || expires-now > 365*86400 {
		return Record{}, Expired
	}
	old, e := b.store.Get(ctx, ref)
	if e != nil && e != Missing {
		return Record{}, storeError(e)
	}
	if e == nil && old.Binding != target {
		return Record{}, Denied
	}
	if old.Revision != expected {
		return Record{}, Conflict
	}
	r, e := b.seal(ctx, Record{Ref: ref, Binding: target, Format: 1, Revision: expected + 1, ExpiresUnix: expires}, secret)
	if e != nil {
		return Record{}, e
	}
	return b.commit(ctx, token, expected, r)
}
func (b *Broker) Bind(target Binding, exit Exit) (*Driver, error) {
	if !target.Valid() || exit == nil {
		return nil, Invalid
	}
	if exit.Target() != target {
		return nil, Denied
	}
	return &Driver{b, target, exit}, nil
}
func (d *Driver) Use(ctx context.Context, token, ref string, call Call) (out Result, err error) {
	b := d.broker
	ctx, cancel := context.WithTimeout(ctx, b.config.Timeout)
	defer cancel()
	// The reference exit honors context; concurrency stays occupied until it
	// returns. Arbitrary trusted in-process code is not a sandbox boundary.
	select {
	case b.slots <- struct{}{}:
		defer func() { <-b.slots }()
	default:
		return out, Unavailable
	}
	defer func() {
		if recover() != nil {
			out = Result{}
			err = TargetUnavailable
		}
	}()
	if !validRef(ref) || !name(call.OperationID) || len(call.Payload) > 4096 {
		return out, Invalid
	}
	if e := b.authorize(ctx, token, d.binding, "use"); e != nil {
		return out, e
	}
	r, e := b.store.Get(ctx, ref)
	if e != nil {
		return out, storeError(e)
	}
	if r.Ref != ref || r.Binding != d.binding {
		return out, Denied
	}
	now, e := b.now()
	if e != nil {
		return out, e
	}
	if now >= r.ExpiresUnix {
		return out, Expired
	}
	secret, e := b.open(ctx, r)
	if e != nil {
		return out, e
	}
	defer clear(secret)
	latest, e := b.store.Get(ctx, ref)
	if e != nil {
		return out, storeError(e)
	}
	if !reflect.DeepEqual(r, latest) {
		return out, Conflict
	}
	if e = b.authorize(ctx, token, d.binding, "use"); e != nil {
		return out, e
	}
	now, e = b.now()
	if e != nil {
		return out, e
	}
	if now >= r.ExpiresUnix {
		return out, Expired
	}
	if ctx.Err() != nil {
		return out, Unavailable
	}
	out, e = d.exit.Send(ctx, Call{OperationID: call.OperationID, Payload: append([]byte(nil), call.Payload...)}, secret)
	if e != nil {
		return Result{}, TargetUnavailable
	}
	return out, nil
}

// Rewrap changes only the key envelope. It is individually atomic and can be
// resumed by comparing the stored revision; old key material stays available.
func (b *Broker) Rewrap(ctx context.Context, token, ref string, target Binding, expected uint64) (Record, error) {
	ctx, cancel := context.WithTimeout(ctx, b.config.Timeout)
	defer cancel()
	if !validRef(ref) || expected == 0 || expected >= 1<<32 {
		return Record{}, Invalid
	}
	if e := b.authorize(ctx, token, target, "manage"); e != nil {
		return Record{}, e
	}
	r, e := b.store.Get(ctx, ref)
	if e != nil {
		return Record{}, storeError(e)
	}
	if r.Ref != ref || r.Binding != target {
		return Record{}, Denied
	}
	if r.Revision != expected {
		return Record{}, Conflict
	}
	secret, e := b.open(ctx, r)
	if e != nil {
		return Record{}, e
	}
	defer clear(secret)
	r.Revision++
	r, e = b.seal(ctx, r, secret)
	if e != nil {
		return Record{}, e
	}
	return b.commit(ctx, token, expected, r)
}

// Target exposes immutable nonsecret binding metadata to trusted composition.
func (d *Driver) Target() Binding { return d.binding }

func (b *Broker) commit(ctx context.Context, token string, expected uint64, r Record) (Record, error) {
	if e := b.authorize(ctx, token, r.Binding, "manage"); e != nil {
		return Record{}, e
	}
	if e := b.store.Swap(ctx, expected, r); e != nil {
		return Record{}, storeError(e)
	}
	return r.Metadata(), nil
}
