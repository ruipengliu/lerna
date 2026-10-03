// valuerunner drives the public typed Go codec for cross-language conformance.
package main

import (
	"errors"
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
		var refusal *contract.ContractError
		if !errors.As(err, &refusal) {
			refusal = &contract.ContractError{PublicError: contract.PublicError{Code: "schema_invalid"}, Cause: err}
		}
		encoded, encodeErr := contract.Encode(refusal.PublicError)
		if encodeErr != nil {
			fmt.Fprintln(os.Stderr, encodeErr)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, string(encoded))
		os.Exit(1)
	}
	if _, err := os.Stdout.Write(data); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
