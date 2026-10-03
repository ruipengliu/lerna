// sqlite-durable-work-fixture assembles the real v1 Host writer. It creates a
// complete file fixture using actual admission, never synthetic old SQL rows.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/host/durablework"
	"github.com/ruipengliu/lerna/runtime"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	commandsPath := flag.String("commands", "conformance/fixtures/durable-work/sqlite-v1/commands.json", "fixed fake-data commands")
	output := flag.String("output", "", "new output directory for actual SQLite file and Host observation")
	flag.Parse()
	if *output == "" {
		return errors.New("explicit new output directory required")
	}
	if err := os.Mkdir(*output, 0755); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(*output, "database.sqlite"), TransactionTimeout: 3 * time.Second, BusyTimeout: 100 * time.Millisecond})
	if err != nil {
		return err
	}
	defer store.Close()
	if err = store.Migrate(ctx); err != nil {
		return err
	}
	owner := contract.OwnerRef{TenantID: "fixture-tenant", OwnerID: "fixture-owner"}
	subject := contract.SubjectBinding{TenantID: owner.TenantID, SubjectID: "fixture-writer", DelegationChain: []contract.DelegatedSubject{}}
	host := durablework.New(owner, store, store, store, store, durablework.NewPermissions([]durablework.Permission{{Subject: subject, Owner: owner, Record: true, Read: true}}))
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
		out, err := host.Record(ctx, command, &subject)
		if err != nil {
			return err
		}
		outcomes = append(outcomes, out)
	}
	first, ok := outcomes[0].AsReceived()
	if !ok {
		return errors.New("v1 initial admission unconfirmed")
	}
	applied, ok := first.Receipt.AsApplied()
	if !ok || applied.Revision != "1" {
		return errors.New("v1 initial admission is not applied revision1")
	}
	expired, ok := outcomes[1].AsReceived()
	if !ok {
		return errors.New("v1 expiry unconfirmed")
	}
	rejected, ok := expired.Receipt.AsRejected()
	if !ok || rejected.Reason != "expired" {
		return errors.New("v1 expiry not fixed rejected")
	}
	replay, ok := outcomes[2].AsReceived()
	if !ok {
		return errors.New("v1 retransmission unconfirmed")
	}
	a, _ := contract.Encode(first.Receipt)
	b, _ := contract.Encode(replay.Receipt)
	if string(a) != string(b) {
		return errors.New("v1 retransmission changed fixed receipt")
	}
	observation, err := host.Observe(ctx, "v1-input", &subject)
	if err != nil {
		return err
	}
	if observation.Input.Revision != 1 || observation.Job.WorkRevision != 1 || observation.Job.CompletedRevision != 0 || observation.Job.State != "ready" {
		return errors.New("v1 pending responsibility missing")
	}
	migration, err := store.MigrationStatus(ctx)
	if err != nil {
		return err
	}
	var settings sqlite.Settings
	if err = store.Within(ctx, owner, func(ctx context.Context, tx runtime.Tx) error {
		var err error
		settings, err = store.Settings(ctx, tx)
		return err
	}); err != nil {
		return err
	}
	// Closing the only SQLite connection checkpoints its WAL into this complete
	// database file. The artifact is the actual writer database, not a logical
	// export assembled from private-table queries or hand-filled rows.
	if err = store.Close(); err != nil {
		return err
	}
	report := struct {
		Migration   sqlite.Migration
		Settings    sqlite.Settings
		Outcomes    []contract.TransportOutcome
		Observation any
	}{migration, settings, outcomes, observation}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(*output, "writer-observation.json"), append(data, '\n'), 0644); err != nil {
		return err
	}
	fmt.Println("Real SQLite Host v1 writer closed applied/pending, expired and retransmission fixture.")
	return nil
}
