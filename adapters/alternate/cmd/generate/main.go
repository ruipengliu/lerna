package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/ruipengliu/lerna/adapters/alternate/contracts"
)

func main() {
	check := flag.Bool("check", false, "check generated wire schemas")
	output := flag.String("output", "adapters/alternate/ts/src/contracts.gen.json", "generated file")
	flag.Parse()
	m, err := contracts.Build()
	if err != nil {
		fail(err)
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		fail(err)
	}
	b = append(b, '\n')
	if *check {
		old, err := os.ReadFile(*output)
		if err != nil {
			fail(err)
		}
		if !bytes.Equal(old, b) {
			fail(fmt.Errorf("alternate contracts drift"))
		}
		return
	}
	if err = os.WriteFile(*output, b, 0644); err != nil {
		fail(err)
	}
}
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
