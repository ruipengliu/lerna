package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"lerna/brain"
	"lerna/profiles/fetchcheck"
	"os"
	"path/filepath"
	"time"
)

// The host supplies target authority before discovery. Candidates cannot extend it.
func runPublic(output, configuration string, inner brain.Model) error {
	f, err := os.Open(configuration)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil {
		return err
	}
	if len(data) > 65536 {
		return fmt.Errorf("public configuration exceeds 65536 bytes")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var cfg fetchcheck.RuntimeConfig
	if err := decoder.Decode(&cfg); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("expected one configuration object")
	}
	cfg.AnswerFromSearch = true
	if cfg.AnswerOutputTokens == 0 {
		cfg.AnswerOutputTokens = 4096
	}
	if cfg.SearchFormat != "doubao" || os.Getenv("DOUBAO_SEARCH_API_KEY") == "" {
		return fmt.Errorf("public run requires Doubao search configuration and host credential")
	}
	if err := save(filepath.Join(output, "batch.json"), map[string]any{"Plan": "search-public-v1", "Started": time.Now().UTC(), "Config": cfg, "Model": inner.Capabilities(), "MaxExternalModelRequests": 1, "ReservedCNY": 1}); err != nil {
		return err
	}
	m := &model{inner: inner, path: filepath.Join(output, "public"), outputLimit: cfg.AnswerOutputTokens}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	started := time.Now().UTC()
	taskRoot := filepath.Join(output, "task")
	if err := os.Mkdir(taskRoot, 0700); err != nil {
		return err
	}
	if err := syncDirectory(output); err != nil {
		return err
	}
	record, failure := fetchcheck.RunResearch(ctx, taskRoot, cfg, m, func(context.Context) (string, error) { return os.Getenv("DOUBAO_SEARCH_API_KEY"), nil })
	errorText := ""
	if failure != nil {
		errorText = failure.Error()
	}
	if err := save(filepath.Join(output, "public.json"), map[string]any{"Started": started, "ElapsedSeconds": time.Since(started).Seconds(), "Record": record, "Error": errorText, "ExternalRequests": len(m.attempts)}); err != nil {
		return err
	}
	fmt.Printf("public requests=%d success=%t\n", len(m.attempts), failure == nil)
	return failure
}
