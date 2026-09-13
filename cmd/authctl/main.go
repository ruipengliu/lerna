// Command authctl is a trusted local installation and management entry point.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"lerna/adapters/authlocal"
	"lerna/adapters/sqliteauth"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/internal/jsonvalue"
	"lerna/protocol"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) < 2 {
		return errors.New("usage: authctl init|call -db FILE -credential-file FILE -config FILE")
	}
	mode := os.Args[1]
	if mode != "init" && mode != "call" {
		return errors.New("unsupported local command")
	}
	flags := flag.NewFlagSet("authctl", flag.ContinueOnError)
	database := flags.String("db", "", "SQLite file")
	credentialPath := flags.String("credential-file", "", "private local credential file")
	configuration := flags.String("config", "", "explicit finite configuration file")
	namespace := flags.String("namespace", "local", "installation namespace")
	administrator := flags.String("administrator", "admin", "installation administrator")
	if err := flags.Parse(os.Args[2:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || *database == "" || *credentialPath == "" || *configuration == "" {
		return errors.New("database, credential file and configuration are required")
	}
	config, err := readConfig(*configuration)
	if err != nil {
		return err
	}
	store, err := sqliteauth.Open(*database)
	if err != nil {
		return err
	}
	defer store.Close()
	service, err := authorization.New(store, authorization.SystemClock{}, config)
	if err != nil {
		return err
	}
	ctx := context.Background()
	if mode == "init" {
		token, err := authorization.NewCredential()
		if err != nil {
			return err
		}
		file, err := os.OpenFile(*credentialPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return errors.New("credential file must be new and private")
		}
		_, writeErr := file.WriteString(token + "\n")
		syncErr := file.Sync()
		closeErr := file.Close()
		if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
			return errors.New("could not persist installation credential")
		}
		directory, err := os.Open(filepath.Dir(*credentialPath))
		if err != nil {
			return err
		}
		syncErr = directory.Sync()
		closeErr = directory.Close()
		if err = errors.Join(syncErr, closeErr); err != nil {
			return err
		}
		if err = service.Initialize(ctx, *namespace, *administrator, token); err != nil {
			return err
		}
		_, err = fmt.Fprintln(os.Stdout, `{"initialized":true}`)
		return err
	}
	info, err := os.Lstat(*credentialPath)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 1024 {
		return errors.New("credential file must be a private regular file")
	}
	token, err := os.ReadFile(*credentialPath)
	if err != nil {
		return err
	}
	input, err := io.ReadAll(io.LimitReader(os.Stdin, protocol.MaxMessageBytes+1))
	if err != nil {
		return err
	}
	if _, err = jsonvalue.Decode(input); err != nil {
		return err
	}
	request := new(wire.AuthorizationRequest)
	if err = protojson.Unmarshal(input, request); err != nil {
		return err
	}
	data, err := proto.Marshal(request)
	if err != nil {
		return err
	}
	data, err = authlocal.Bind(service, strings.TrimSpace(string(token))).Exchange(ctx, data)
	if err != nil {
		return err
	}
	response := new(wire.AuthorizationResponse)
	if err = proto.Unmarshal(data, response); err != nil {
		return err
	}
	output, err := protojson.Marshal(response)
	if err != nil {
		return err
	}
	if _, err = os.Stdout.Write(append(output, '\n')); err != nil {
		return err
	}
	if response.GetFailure() != nil {
		return errors.New(response.GetFailure().Code)
	}
	return nil
}
func readConfig(path string) (authorization.Config, error) {
	var result authorization.Config
	file, err := os.Open(path)
	if err != nil {
		return result, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil {
		return result, err
	}
	if len(data) > 65536 {
		return result, errors.New("configuration too large")
	}
	if _, err = jsonvalue.Decode(data); err != nil {
		return result, err
	}
	var input struct {
		CredentialTTL     string `json:"credential_ttl"`
		GrantTTL          string `json:"grant_ttl"`
		WindowTTL         string `json:"window_ttl"`
		ReceiptRetention  string `json:"receipt_retention"`
		MaxRules          int    `json:"max_rules"`
		MaxResources      int    `json:"max_resources"`
		MaxDepth          int    `json:"max_depth"`
		MaxWork           int    `json:"max_work"`
		EvaluationTimeout string `json:"evaluation_timeout"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&input); err != nil {
		return result, err
	}
	for _, pair := range []struct {
		value  string
		target *time.Duration
	}{{input.CredentialTTL, &result.CredentialTTL}, {input.GrantTTL, &result.GrantTTL}, {input.WindowTTL, &result.WindowTTL}, {input.ReceiptRetention, &result.ReceiptRetention}, {input.EvaluationTimeout, &result.EvaluationTimeout}} {
		value, err := time.ParseDuration(pair.value)
		if err != nil {
			return result, errors.New("finite duration required")
		}
		*pair.target = value
	}
	result.MaxRules = input.MaxRules
	result.MaxResources = input.MaxResources
	result.MaxDepth = input.MaxDepth
	result.MaxWork = input.MaxWork
	return result, nil
}
