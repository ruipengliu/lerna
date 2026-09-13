// Command credentialctl is a trusted local management entry. It never provides
// a secret download command or accepts secret bytes on the command line.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"lerna/adapters/credentialauth"
	"lerna/adapters/filekeys"
	"lerna/adapters/sqliteauth"
	"lerna/adapters/sqlitecredentials"
	"lerna/authorization"
	"lerna/credentials"
	"lerna/internal/jsonvalue"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type configuration struct {
	AuthDB, TokenFile, CredentialDB, KeyDirectory, Resource string
	Binding                                                 credentials.Binding
	BackupDirectory, ArchiveID                              string
	Renewal                                                 *renewalConfiguration
}

func read(path string, max int, private bool) ([]byte, error) {
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() || info.Size() > int64(max) || (private && info.Mode().Perm()&0077 != 0) {
		return nil, credentials.Unavailable
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, credentials.Unavailable
	}
	defer f.Close()
	raw, e := io.ReadAll(io.LimitReader(f, int64(max+1)))
	if e != nil || len(raw) > max {
		return nil, credentials.Unavailable
	}
	return raw, nil
}
func run(ctx context.Context, args []string, input io.Reader, output io.Writer) error {
	if len(args) < 1 {
		return credentials.Invalid
	}
	fs := flag.NewFlagSet("credentialctl", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", "", "nonsecret host configuration")
	keyDir := fs.String("key-dir", "", "independent private master-key directory")
	ref := fs.String("ref", "", "credential reference")
	operation := fs.String("operation", "", "stable management operation")
	keyVersion := fs.String("key-version", "", "old key version")
	expected := fs.Uint64("expected", 0, "expected revision")
	expires := fs.Int64("expires", 0, "expiry Unix seconds")
	if fs.Parse(args[1:]) != nil || fs.NArg() != 0 {
		return credentials.Invalid
	}
	encode := func(v any) error {
		if json.NewEncoder(output).Encode(v) != nil {
			return credentials.Unavailable
		}
		return nil
	}
	if args[0] == "init-key" {
		if *keyDir == "" {
			return credentials.Invalid
		}
		if args[0] == "init-key" {
			if e := os.Mkdir(*keyDir, 0700); e != nil && !os.IsExist(e) {
				return credentials.KeyUnavailable
			}
		}
		_, present := os.Lstat(filepath.Join(*keyDir, "keys.json"))
		if args[0] == "init-key" && present == nil {
			return credentials.Conflict
		}
		source, e := filekeys.Open(*keyDir, filekeys.MaxUses)
		if e != nil {
			return e
		}
		defer source.Close()
		id, e := source.Generate(ctx)
		if e != nil {
			return e
		}
		return encode(struct{ KeyVersion string }{id})
	}
	if args[0] != "put" && args[0] != "rewrap" && args[0] != "list" && !isLifecycleCommand(args[0]) {
		return credentials.Invalid
	}
	raw, e := read(*configPath, 16384, false)
	if e != nil {
		return e
	}
	if _, e = jsonvalue.Decode(raw); e != nil {
		return credentials.Invalid
	}
	var cfg configuration
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&cfg) != nil || !cfg.Binding.Valid() {
		return credentials.Invalid
	}
	dbPath, e := filepath.Abs(cfg.CredentialDB)
	if e != nil || cfg.CredentialDB == "" {
		return credentials.Invalid
	}
	dirPath, e := filepath.Abs(cfg.KeyDirectory)
	if e != nil || cfg.KeyDirectory == "" {
		return credentials.Invalid
	}
	rel, e := filepath.Rel(dirPath, dbPath)
	if e != nil || rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..") {
		return credentials.Denied
	}
	token, e := read(cfg.TokenFile, 4096, true)
	if e != nil {
		return e
	}
	defer clear(token)
	db, e := sqliteauth.Open(cfg.AuthDB)
	if e != nil {
		return credentials.Unavailable
	}
	defer db.Close()
	snap, e := db.Load(ctx)
	if e != nil || snap.State.Format != 1 {
		return credentials.Unavailable
	}
	authority, e := authorization.New(db, authorization.SystemClock{}, snap.State.Config)
	if e != nil {
		return credentials.Unavailable
	}
	a, e := credentialauth.New(authority, cfg.Resource)
	if e != nil {
		return e
	}
	if e = a.Check(ctx, strings.TrimSpace(string(token)), cfg.Binding, "manage"); e != nil {
		return e
	}
	keys, e := filekeys.Open(cfg.KeyDirectory, filekeys.MaxUses)
	if e != nil {
		return e
	}
	defer keys.Close()
	store, e := sqlitecredentials.Open(cfg.CredentialDB)
	if e != nil {
		return e
	}
	defer store.Close()
	broker, e := credentials.New(store, keys, a, authorization.SystemClock{}, credentials.Config{Timeout: time.Second, MaxConcurrent: 4})
	if e != nil {
		return e
	}
	if args[0] == "list" {
		rows, e := store.List(ctx)
		if e != nil {
			return e
		}
		out := []credentials.Record{}
		for _, r := range rows {
			if r.Binding == cfg.Binding {
				out = append(out, r.Metadata())
			}
		}
		return encode(out)
	}
	if isLifecycleCommand(args[0]) {
		manager, e := credentials.NewLifecycle(store, keys, a, authorization.SystemClock{}, credentials.Config{Timeout: time.Second, MaxConcurrent: 4})
		if e != nil {
			return e
		}
		result, e := lifecycleCommand(ctx, manager, strings.TrimSpace(string(token)), cfg, managementAction{Command: args[0], Operation: *operation, Ref: *ref, KeyVersion: *keyVersion})
		if e != nil {
			return e
		}
		return encode(result)
	}
	var r credentials.Record
	if args[0] == "put" {
		secret, e := readSecret(ctx, input)
		defer clear(secret)
		if e != nil {
			return e
		}
		r, e = broker.Put(ctx, strings.TrimSpace(string(token)), *ref, cfg.Binding, *expected, *expires, secret)
		if e != nil {
			return e
		}
	} else {
		r, e = broker.Rewrap(ctx, strings.TrimSpace(string(token)), *ref, cfg.Binding, *expected)
		if e != nil {
			return e
		}
	}
	return encode(r)
}
func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if e := run(ctx, os.Args[1:], os.Stdin, os.Stdout); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}

// readSecret bounds both input size and waiting time. On cancellation the CLI
// closes its input; the reader owns and clears any bytes received after expiry.
func readSecret(ctx context.Context, input io.Reader) ([]byte, error) {
	type result struct {
		data []byte
		err  error
	}
	done := make(chan result)
	go func() {
		data, err := io.ReadAll(io.LimitReader(input, 4097))
		select {
		case done <- result{data, err}:
		case <-ctx.Done():
			clear(data)
		}
	}()
	select {
	case r := <-done:
		if ctx.Err() != nil {
			clear(r.data)
			return nil, credentials.Unavailable
		}
		if r.err != nil || len(r.data) > 4096 {
			clear(r.data)
			return nil, credentials.Invalid
		}
		return r.data, nil
	case <-ctx.Done():
		if closer, ok := input.(io.Closer); ok {
			go closer.Close()
		}
		return nil, credentials.Unavailable
	}
}
