// valuerunner drives the public typed Go codec for cross-language conformance.
package main

import (
	"errors"
	"fmt"
	contract "github.com/ruipengliu/lerna/contract/v1_2"
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
	if len(os.Args) == 2 && os.Args[1] == "--batch" {
		if err := batch(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: valuerunner SCHEMA < JSON")
		os.Exit(2)
	}
	data, err := io.ReadAll(io.LimitReader(os.Stdin, contract.MaxBodyBytes+1))
	if err == nil {
		data, err = run(os.Args[1], data)
	}
	if err != nil {
		encoded, encodeErr := contract.Encode(publicError(err))
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

func publicError(err error) contract.PublicError {
	var refusal *contract.ContractError
	if errors.As(err, &refusal) {
		return refusal.PublicError
	}
	return contract.PublicError{Code: "schema_invalid"}
}
