package credentialcheck

import (
	"bytes"
	"context"
	"fmt"
	"lerna/adapters/credentials/filekeys"
	credentialhttp "lerna/adapters/credentials/http"
	sqlitecredentials "lerna/adapters/credentials/sqlite"
	"lerna/credentials"
	"os"
	"path/filepath"
	"time"
)

var caseNames = []string{"stateful-use", "ciphertext-only", "reopen", "namespace", "subject", "driver", "service", "account", "purpose", "location", "exit-binding", "missing", "expired", "key-missing", "ciphertext-tamper", "reference-move", "source-policy", "reflection", "rotation"}

func Check(ctx context.Context, mode string) error {
	f, e := newFixture(ctx)
	if e != nil {
		return e
	}
	defer f.close()
	if mode == "stateful-use" {
		if e = f.use(ctx, "credential", "op"); e != nil {
			return e
		}
		if e = f.use(ctx, "credential", "op"); e != nil {
			return e
		}
		v, e := f.value(ctx)
		if e != nil || v != 3 || f.sent() != 2 {
			return fmt.Errorf("target did not retain one operation effect")
		}
		return nil
	}
	if mode == "ciphertext-only" {
		rows, e := f.store.List(ctx)
		if e != nil || len(rows) != 1 {
			return fmt.Errorf("ciphertext record missing")
		}
		if bytes.Contains(rows[0].Ciphertext, []byte(fixtureSecret)) {
			return fmt.Errorf("plaintext persisted")
		}
		files, _ := filepath.Glob(filepath.Join(f.root, "credentials.db*"))
		for _, p := range files {
			raw, e := os.ReadFile(p)
			if e != nil {
				return e
			}
			if bytes.Contains(raw, []byte(fixtureSecret)) {
				return fmt.Errorf("secret in database")
			}
		}
		return f.use(ctx, "credential", "op")
	}
	if mode == "reopen" {
		f.store.Close()
		f.store, e = sqlitecredentials.Open(filepath.Join(f.root, "credentials.db"))
		if e != nil {
			return e
		}
		f.keys.Close()
		f.keys, e = filekeys.Open(filepath.Join(f.root, "keys"), filekeys.MaxUses)
		if e != nil {
			return e
		}
		if e = f.rebind(); e != nil {
			return e
		}
		return f.use(ctx, "credential", "op")
	}
	if mode == "rotation" {
		old, e := f.store.Get(ctx, "credential")
		if e != nil {
			return e
		}
		if _, e = f.keys.Generate(ctx); e != nil {
			return e
		}
		if e = f.use(ctx, "credential", "before-rewrap"); e != nil {
			return e
		}
		if _, e = f.broker.Rewrap(ctx, f.token, "credential", f.binding, old.Revision); e != nil {
			return e
		}
		fresh, e := f.store.Get(ctx, "credential")
		if e != nil || fresh.KeyVersion == old.KeyVersion || fresh.Revision != old.Revision+1 {
			return fmt.Errorf("rotation did not change envelope")
		}
		return f.use(ctx, "credential", "after-rewrap")
	}
	if mode == "exit-binding" {
		other := f.binding
		other.Account = "other"
		if _, e = f.broker.Bind(other, f.exit); e != credentials.Denied {
			return fmt.Errorf("exit mismatch accepted")
		}
		return nil
	}
	ref := "credential"
	expected := credentials.Denied
	switch mode {
	case "namespace", "subject", "driver", "service", "account", "purpose", "location":
		b := f.binding
		switch mode {
		case "namespace":
			b.Namespace = "other"
		case "subject":
			b.Subject = "other"
		case "driver":
			b.Driver = "other"
		case "service":
			b.Service = "http://127.0.0.1:1"
		case "account":
			b.Account = "other"
		case "purpose":
			b.Purpose = "other"
		case "location":
			b.Location = "other"
		}
		exit, e := credentialhttp.New(credentialhttp.Config{Binding: b, Path: "/apply", Timeout: time.Second, AllowLoopbackHTTP: true})
		if e != nil {
			return e
		}
		defer exit.Close()
		d, e := f.broker.Bind(b, exit)
		if e != nil {
			return e
		}
		_, e = d.Use(ctx, f.token, ref, credentials.Call{OperationID: "op", Payload: []byte(`{"Delta":3}`)})
		if e != expected || f.sent() != 0 {
			return fmt.Errorf("binding %s did not reject before send", mode)
		}
		return nil
	case "missing":
		ref = "absent"
		expected = credentials.Missing
	case "expired":
		f.clock.advance(time.Hour)
		if e = f.setPolicy(ctx, []string{"credential.manage", "credential.use"}); e != nil {
			return e
		}
		expected = credentials.Expired
	case "key-missing":
		if e = os.Rename(filepath.Join(f.root, "keys", "keys.json"), filepath.Join(f.root, "keys", "unavailable")); e != nil {
			return e
		}
		expected = credentials.KeyUnavailable
	case "ciphertext-tamper":
		r, e := f.store.Get(ctx, ref)
		if e != nil {
			return e
		}
		r.Revision++
		r.Ciphertext[0] ^= 1
		if e = f.store.Swap(ctx, r.Revision-1, r); e != nil {
			return e
		}
	case "reference-move":
		r, e := f.store.Get(ctx, ref)
		if e != nil {
			return e
		}
		r.Ref = "moved"
		if e = f.store.Swap(ctx, 0, r); e != nil {
			return e
		}
		ref = "moved"
	case "source-policy":
		if e = f.setPolicy(ctx, []string{"credential.manage"}); e != nil {
			return e
		}
	case "reflection":
		f.mu.Lock()
		f.reflection = true
		f.mu.Unlock()
		expected = credentials.TargetUnavailable
	default:
		return fmt.Errorf("unknown check")
	}
	e = f.use(ctx, ref, "op")
	if e != expected {
		return fmt.Errorf("unexpected rejection for %s: %v", mode, e)
	}
	count := 0
	if mode == "reflection" {
		count = 1
	}
	if f.sent() != count {
		return fmt.Errorf("credential refusal reached target")
	}
	value, e := f.value(ctx)
	if e != nil || value != 0 {
		return fmt.Errorf("refused operation changed target")
	}
	return nil
}

func independentCase(ctx context.Context) error {
	f, e := newFixture(ctx)
	if e != nil {
		return e
	}
	defer f.close()
	if e = f.use(ctx, "missing", "blocked"); e != credentials.Missing || f.sent() != 0 {
		return fmt.Errorf("missing credential did not block locally")
	}
	if e = f.use(ctx, "credential", "independent"); e != nil {
		return e
	}
	v, e := f.value(ctx)
	if e != nil || v != 3 {
		return fmt.Errorf("independent credential work blocked")
	}
	return nil
}
