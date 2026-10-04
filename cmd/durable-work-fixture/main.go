// durable-work-fixture assembles the real Host writer for auditable v1 input.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/host/durablework"
	demo "github.com/ruipengliu/lerna/internal/durableworkdemo"
	"github.com/ruipengliu/lerna/runtime"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() (resultErr error) {
	commandsPath := flag.String("commands", "conformance/fixtures/durable-work/pg-v1/commands.json", "fixed fake-data commands")
	output := flag.String("output", "", "output directory for full SQL dump and Host observation")
	dumpTool := flag.String("pg-dump", "pg_dump", "PostgreSQL 18.6 pg_dump executable")
	flag.Parse()
	if *output == "" {
		return errors.New("explicit output directory required")
	}
	dsn := os.Getenv("LERNA_TEST_POSTGRES_DSN")
	if dsn == "" {
		return errors.New("LERNA_TEST_POSTGRES_DSN is required (dedicated test database)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const schema = "lerna_test_000000000000000000000001"
	store, err := postgres.Open(ctx, postgres.Config{DSN: dsn, Schema: schema, TransactionTimeout: 3 * time.Second, StatementTimeout: 2 * time.Second, LockTimeout: time.Second})
	if store != nil {
		defer func() { resultErr = errors.Join(resultErr, store.Close()) }()
	}
	if err != nil {
		return err
	}
	if err = store.CreateSchema(ctx); err != nil {
		return err
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := store.DropTestSchema(cleanup); err != nil {
			resultErr = errors.Join(resultErr, errors.New("fixture schema cleanup failed"))
		}
	}()
	if err = store.Migrate(ctx); err != nil {
		return err
	}
	owner := contract.OwnerRef{TenantID: "fixture-tenant", OwnerID: "fixture-owner"}
	subject := contract.SubjectBinding{TenantID: owner.TenantID, SubjectID: "fixture-writer", DelegationChain: []contract.DelegatedSubject{}}
	host := durablework.New(owner, store, store, store, store, durablework.NewPermissions([]durablework.Permission{{Subject: subject, Owner: owner, Record: true, Read: true}}))
	// Current Host assembly requires a durable finite pool. The historical
	// export scripts build their immutable source commits; frozen v1/v2 corpus
	// bytes and original writers are unaffected by this current configuration.
	host.PoolControl = true
	if err = host.InstallPool(ctx, demo.DefaultPool("fixture-pool", []contract.OwnerRef{owner}), 0); err != nil {
		return err
	}
	host.PoolControl = false
	input, err := os.ReadFile(*commandsPath)
	if err != nil {
		return err
	}
	var commands []json.RawMessage
	if err = json.Unmarshal(input, &commands); err != nil {
		return err
	}
	if len(commands) != 3 {
		return errors.New("v1 corpus must contain applied, expired, original retransmission")
	}
	outcomes := make([]contract.TransportOutcome, 0, len(commands))
	for _, command := range commands {
		outcome, err := host.Record(ctx, command, &subject)
		if err != nil {
			return err
		}
		outcomes = append(outcomes, outcome)
	}
	first, ok := outcomes[0].AsReceived()
	if !ok {
		return errors.New("v1 writer did not confirm initial admission")
	}
	applied, ok := first.Receipt.AsApplied()
	if !ok || applied.Revision != "1" {
		return errors.New("v1 initial admission is not applied revision1")
	}
	expired, ok := outcomes[1].AsReceived()
	if !ok {
		return errors.New("v1 writer did not confirm expired rejection")
	}
	refusal, ok := expired.Receipt.AsRejected()
	if !ok || refusal.Reason != "expired" {
		return errors.New("v1 expired command was not fixed rejected")
	}
	replay, ok := outcomes[2].AsReceived()
	if !ok {
		return errors.New("v1 writer did not confirm original retransmission")
	}
	a, _ := contract.Encode(first.Receipt)
	b, _ := contract.Encode(replay.Receipt)
	if string(a) != string(b) {
		return errors.New("v1 retransmission changed the original receipt")
	}
	observed, err := host.Observe(ctx, "v1-input", &subject)
	if err != nil {
		return err
	}
	if observed.Input.Revision != 1 || observed.Job.WorkRevision != 1 || observed.Job.CompletedRevision != 0 || observed.Job.State != "ready" {
		return errors.New("v1 writer did not retain pending responsibility")
	}
	var settings postgres.Settings
	if err = store.Within(ctx, owner, func(ctx context.Context, tx runtime.Tx) error {
		var err error
		settings, err = store.Settings(ctx, tx)
		return err
	}); err != nil {
		return err
	}
	if err = os.MkdirAll(*output, 0755); err != nil {
		return err
	}
	dump, err := os.Create(filepath.Join(*output, "database.sql"))
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, *dumpTool, "--format=plain", "--no-owner", "--no-privileges", "--restrict-key=lernav1fixture", "--strict-names", "--schema="+schema, dsn)
	command.Stdout = dump
	// Do not expose a connection string, credentials, command arguments or
	// unfiltered pg_dump stderr. The script verifies the export tool separately.
	err = command.Run()
	closeError := dump.Close()
	if err != nil {
		return errors.New("v1 database export failed")
	}
	if closeError != nil {
		return closeError
	}
	report := struct {
		Schema, MigrationChecksum string
		Settings                  postgres.Settings
		Outcomes                  []contract.TransportOutcome
		Observation               any
	}{schema, postgres.MigrationV1Checksum(), settings, outcomes, observed}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err = os.WriteFile(filepath.Join(*output, "writer-observation.json"), data, 0644); err != nil {
		return err
	}
	fmt.Println("Real Host writer exported applied/pending, expired and retransmission evidence.")
	return nil
}
