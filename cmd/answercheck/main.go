// Command answercheck runs the separately authorized real-provider acceptance.
// It is deliberately excluded from make verify and never auto-retries.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"lerna/profiles/answer"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	envFile := flag.String("env-file", "", "optional local file containing ARK_API_KEY; never executed")
	parent := flag.String("state-parent", "build", "parent for a new private SQLite evidence directory")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected arguments")
		os.Exit(2)
	}
	key := os.Getenv("ARK_API_KEY")
	if *envFile != "" {
		data, e := os.ReadFile(*envFile)
		if e != nil || len(data) > 65536 {
			fmt.Fprintln(os.Stderr, "cannot read bounded credential file")
			os.Exit(2)
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			line = strings.TrimPrefix(line, "export ")
			name, value, ok := strings.Cut(line, "=")
			if ok && strings.TrimSpace(name) == "ARK_API_KEY" {
				key = strings.Trim(strings.TrimSpace(value), "\"'")
			}
		}
	}
	if e := os.MkdirAll(*parent, 0700); e != nil {
		fmt.Fprintln(os.Stderr, "state directory unavailable")
		os.Exit(2)
	}
	root, e := os.MkdirTemp(*parent, "real-answer-")
	if e != nil {
		fmt.Fprintln(os.Stderr, "state directory unavailable")
		os.Exit(2)
	}
	root, e = filepath.Abs(root)
	if e != nil {
		fmt.Fprintln(os.Stderr, "state directory unavailable")
		os.Exit(2)
	}
	report := answer.Real(context.Background(), root, key)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if enc.Encode(report) != nil {
		os.Exit(2)
	}
	if report.Status != "passed" {
		os.Exit(1)
	}
}
