//go:build !linux

// Package filekeys provides the Linux private-file reference key source. Other
// platforms can bind another credentials.KeySource; this adapter fails closed.
package filekeys

import (
	"context"
	"lerna/credentials"
)

const MaxUses = 1 << 20

type Source struct{}

func Open(string, uint64) (*Source, error)               { return nil, credentials.KeyUnavailable }
func (*Source) Close() error                             { return nil }
func (*Source) Generate(context.Context) (string, error) { return "", credentials.KeyUnavailable }
func (*Source) Reserve(context.Context) (string, []byte, []byte, error) {
	return "", nil, nil, credentials.KeyUnavailable
}
func (*Source) Read(context.Context, string) ([]byte, error) { return nil, credentials.KeyUnavailable }

func (*Source) Prepare(context.Context, string) (string, error) {
	return "", credentials.KeyUnavailable
}
func (*Source) Activate(context.Context, string, string) error { return credentials.KeyUnavailable }
func (*Source) Retire(context.Context, string) error           { return credentials.KeyUnavailable }

func (*Source) Active(context.Context) (string, error) { return "", credentials.KeyUnavailable }
