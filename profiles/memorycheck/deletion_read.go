package memorycheck

import (
	"context"
	"fmt"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"os"
	"reflect"
	"time"
)

// This boundary wrapper returns an actually loaded record, then commits a real
// authorized deletion before Reader can return the cached body to its caller.
type deletedAfterRead struct {
	memory.QueryStore
	remove func(context.Context) error
	fired  bool
}

func (s *deletedAfterRead) Read(ctx context.Context, ref memory.Ref, revision uint64) (memory.Revision, error) {
	row, err := s.QueryStore.Read(ctx, ref, revision)
	if err == nil && !s.fired {
		s.fired = true
		if err = s.remove(ctx); err != nil {
			return memory.Revision{}, err
		}
	}
	return row, err
}
func CheckDeletionReadRace(ctx context.Context) error     { return checkDeletionReadRace(ctx, false) }
func CheckDeletionCoverageRace(ctx context.Context) error { return checkDeletionReadRace(ctx, true) }
func checkDeletionReadRace(ctx context.Context, coverage bool) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	f, err := newFixture(ctx)
	if err != nil {
		return err
	}
	defer os.RemoveAll(f.config.Root)
	defer f.close()
	if err = f.put(ctx); err != nil {
		return err
	}
	query := f.query()
	if coverage {
		query.MaxBytes = 1
		intent, e := memory.DescribeQuery(f.binding, query)
		if e != nil {
			return e
		}
		f.config.Grant, e = f.sign(ctx, intent)
		if e != nil {
			return e
		}
	}
	request := &wire.MemoryRequest{Method: "QUERY", Query: query, GrantMaterial: f.config.Grant}
	initial, e := f.client.Exchange(ctx, request)
	if e != nil {
		return e
	}
	if coverage && (len(initial.Result.Records) != 0 || initial.Result.Coverage != "budget_exhausted") {
		return fmt.Errorf("coverage witness setup failed")
	}

	original, err := f.store.LookupRead(ctx, "local", f.config.ReadID)
	if err != nil {
		return err
	}
	id, err := f.auth.NewOperation(ctx, f.config.Token)
	if err != nil {
		return err
	}
	wrapped := &deletedAfterRead{QueryStore: f.store, remove: func(ctx context.Context) error {
		_, e := f.client.Exchange(ctx, &wire.MemoryRequest{Method: "DELETE", Delete: &wire.MemoryDelete{OperationId: id, Ref: f.write(f.config.WriteID, 0, "concise").Write.Ref, ExpectedRevision: 1, Purpose: "assist"}})
		return e
	}}
	client, err := f.bind(wrapped, f.permits)
	if err != nil {
		return err
	}
	for range 2 {
		result, e := client.Exchange(ctx, request)
		if e != memory.Irrecoverable || result != nil {
			return fmt.Errorf("deleted body escaped original query: %v", e)
		}
	}
	if !wrapped.fired {
		return fmt.Errorf("deletion boundary was not reached")
	}
	after, err := f.store.LookupRead(ctx, "local", f.config.ReadID)
	if err != nil || !reflect.DeepEqual(after, original) {
		return fmt.Errorf("query replaced original binding: %v", err)
	}
	return nil
}
