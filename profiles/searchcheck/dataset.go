// Package searchcheck provides frozen materials for the small research-answer
// evaluation. Candidate inputs and judging criteria have separate loading APIs;
// judging criteria must never be passed to the candidate model.
package searchcheck

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

//go:embed testdata/sources-v1.json
var sourceJSON []byte

//go:embed testdata/expectations-v1.json
var expectationJSON []byte

type Sources struct {
	Version   string `json:"version"`
	Fictional bool   `json:"fictional"`
	Cases     []Case `json:"cases"`
	SHA256    string `json:"sha256"`
}

// Case contains only candidate-visible questions and simulated source content.
type Case struct {
	ID       string `json:"id"`
	Question string `json:"question"`
	Query    string `json:"query"`
	Pages    []Page `json:"pages"`
}

type Page struct {
	Key     string `json:"key"`
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
	Status  int    `json:"status"`
	Body    string `json:"body"`
}

type Expectations struct {
	Version       string        `json:"version"`
	NotModelInput bool          `json:"not_model_input"`
	Cases         []Expectation `json:"cases"`
	SHA256        string        `json:"sha256"`
}

type Expectation struct {
	ID                  string         `json:"id"`
	Status              string         `json:"status"`
	RequiredFacts       []RequiredFact `json:"required_facts"`
	RequiredScope       string         `json:"required_scope"`
	RequiredDisposition string         `json:"required_disposition,omitempty"`
	Forbidden           []string       `json:"forbidden"`
}

type RequiredFact struct {
	ID              string   `json:"id"`
	Meaning         string   `json:"meaning"`
	Sources         []string `json:"sources"`
	SupportingQuote string   `json:"supporting_quote"`
}

// LoadSources returns a fresh copy of the preregistered candidate materials.
// Editing the embedded file without explicitly updating its pinned version is
// an error, rather than silently evaluating against different materials.
func LoadSources() (Sources, error) {
	var out Sources
	const digest = "c4e872461ebd41c2ad5e3d64b8075d87bab7d12bc786f74bc9e0ae2a357dfe5c"
	if err := decodeFrozen(sourceJSON, digest, &out); err != nil {
		return Sources{}, err
	}
	out.SHA256 = digest
	return out, nil
}

// LoadExpectations is for the independent evaluator, after candidate execution.
// Its result includes the preregistered coverage denominator and must not enter
// search responses, task context, or model prompts.
func LoadExpectations() (Expectations, error) {
	var out Expectations
	const digest = "5a171025c1fc7db9b82b82ae6e6cfbbe93a80bdaf6f2f6a6f6f0c3a2b2def3a9"
	if err := decodeFrozen(expectationJSON, digest, &out); err != nil {
		return Expectations{}, err
	}
	out.SHA256 = digest
	return out, nil
}

func decodeFrozen(raw []byte, want string, out any) error {
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != want {
		return fmt.Errorf("searchcheck: frozen material digest mismatch")
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("searchcheck: decode frozen material: %w", err)
	}
	return nil
}
