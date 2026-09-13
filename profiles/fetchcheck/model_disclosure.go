package fetchcheck

import (
	"lerna/brain"
	"lerna/fetch"
	"slices"
	"strings"
	"unicode/utf8"
)

// validateResearchDisclosure validates trusted host configuration before any
// model dispatch. It never changes the model's declared processing location.
func validateResearchDisclosure(cap brain.Capabilities, tokens uint64, recipients []string, outputTokens uint64) error {
	if outputTokens < 1 || outputTokens > 32768 || tokens == 0 || tokens > 1048576 || len(recipients) > 16 || cap.Location == "" || cap.Model == "" || cap.Version == "" || !cap.Text || !cap.Structured || !cap.HardBounds || cap.InputUpper < 1 || cap.InputUpper > 1048576-outputTokens || cap.ContextTokens < max(uint64(2048), cap.InputUpper)+outputTokens || tokens < max(uint64(2048), cap.InputUpper)+outputTokens {
		return fetch.Invalid
	}
	for _, location := range recipients {
		if location == "" || len(location) > 256 || !utf8.ValidString(location) || strings.TrimSpace(location) != location {
			return fetch.Invalid
		}
	}
	if cap.Location != "local" && !slices.Contains(recipients, cap.Location) {
		return fetch.Denied
	}
	return nil
}

func researchOutputLimit(configured uint64) uint64 {
	if configured == 0 {
		return 512
	}
	return configured
}
