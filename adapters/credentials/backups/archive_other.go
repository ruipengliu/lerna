//go:build !linux

package backups

import (
	"context"
	"lerna/credentials"
)

type Archive struct{}

func Open(string, string) (*Archive, error) { return nil, credentials.Unavailable }
func (*Archive) ID() string                 { return "" }
func (*Archive) Close() error               { return nil }
func (*Archive) Put(context.Context, string, []byte) (string, error) {
	return "", credentials.Unavailable
}
func (*Archive) Check(context.Context, string, string) error  { return credentials.Unavailable }
func (*Archive) Delete(context.Context, string, string) error { return credentials.Unavailable }
