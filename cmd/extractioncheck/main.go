// extractioncheck emits reproducible local extraction quality evidence.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	localextraction "lerna/adapters/extraction/rules"
	"lerna/profiles/extractioncheck"
	"os"
	"runtime"
	"time"
)

func main() {
	profile := flag.String("profile", "local-rules-quality-v1", "quality profile (local-rules-quality-v1)")
	flag.Parse()
	if *profile != "local-rules-quality-v1" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unsupported extraction quality profile or arguments")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	quality, err := extractioncheck.Quality(ctx, localextraction.Rules{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	report := struct {
		Profile, Implementation, Location, GoVersion string
		ExternalModelRequests                        int
		Quality                                      extractioncheck.QualityReport
	}{*profile, "local-rules-v1", "local", runtime.Version(), 0, quality}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(report); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if !quality.Passed {
		os.Exit(1)
	}
}
