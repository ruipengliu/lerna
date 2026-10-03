// valuerunner drives the public typed Go codec for cross-language conformance.
package main

import (
	"fmt"
	"github.com/ruipengliu/lerna/contract"
	"io"
	"os"
)

func roundtrip[T contract.Value](data []byte) ([]byte, error) {
	value, err := contract.Decode[T](data)
	if err != nil {
		return nil, err
	}
	return contract.Encode(value)
}
func run(name string, data []byte) ([]byte, error) {
	switch name {
	case "ID":
		return roundtrip[contract.ID](data)
	case "Revision":
		return roundtrip[contract.Revision](data)
	case "Time":
		return roundtrip[contract.Time](data)
	case "Kind":
		return roundtrip[contract.Kind](data)
	case "OwnerRef":
		return roundtrip[contract.OwnerRef](data)
	case "ObjectRef":
		return roundtrip[contract.ObjectRef](data)
	case "ContentRef":
		return roundtrip[contract.ContentRef](data)
	case "Amount":
		return roundtrip[contract.Amount](data)
	case "Gap":
		return roundtrip[contract.Gap](data)
	case "ReadScope":
		return roundtrip[contract.ReadScope](data)
	case "CollectionView":
		return roundtrip[contract.CollectionView](data)
	default:
		return nil, fmt.Errorf("unknown value schema %q", name)
	}
}
func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: valuerunner SCHEMA < JSON")
		os.Exit(2)
	}
	data, err := io.ReadAll(io.LimitReader(os.Stdin, contract.MaxBodyBytes+1))
	if err == nil {
		data, err = run(os.Args[1], data)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err := os.Stdout.Write(data); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
