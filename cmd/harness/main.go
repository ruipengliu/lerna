package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ruipengliu/lerna/runtime"
	sdk "github.com/ruipengliu/lerna/sdk/go"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "version" {
		fmt.Println(runtime.Version)
		return
	}
	if len(os.Args) < 2 || os.Args[1] != "status" {
		fmt.Fprintln(os.Stderr, "usage: harness version | harness status --url http://127.0.0.1:18080; task commands are not implemented")
		os.Exit(2)
	}
	flags := flag.NewFlagSet("status", flag.ExitOnError)
	baseURL := flags.String("url", "http://127.0.0.1:18080", "local host admin URL")
	_ = flags.Parse(os.Args[2:])
	if flags.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "status does not accept positional arguments")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	status, err := (sdk.Client{BaseURL: *baseURL}).Status(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(status); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
