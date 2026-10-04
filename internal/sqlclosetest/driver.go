// Package sqlclosetest is a mechanical database/sql test driver. It performs no
// queries and owns no physical database, file, network or business scope.
package sqlclosetest

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"time"
)

type Fault struct {
	PingError, CloseError error
	CloseStarted          chan struct{}
	CloseDone             chan struct{}
	CloseRelease          <-chan struct{}
}

func Open(f Fault) *sql.DB { return sql.OpenDB(connector{f}) }

type connector struct{ fault Fault }

func (c connector) Connect(context.Context) (driver.Conn, error) { return connection{c.fault}, nil }
func (c connector) Driver() driver.Driver                        { return testDriver{} }

type testDriver struct{}

func (testDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("mechanical connector only")
}

type connection struct{ fault Fault }

func (connection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("mechanical driver has no statements")
}
func (connection) Begin() (driver.Tx, error) {
	return nil, errors.New("mechanical driver has no transactions")
}
func (c connection) Ping(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.fault.PingError
}
func (c connection) Close() error {
	if c.fault.CloseDone != nil {
		defer close(c.fault.CloseDone)
	}
	if c.fault.CloseStarted != nil {
		close(c.fault.CloseStarted)
	}
	if c.fault.CloseRelease != nil {
		select {
		case <-c.fault.CloseRelease:
		case <-time.After(2 * time.Second):
			return errors.New("mechanical close gate deadline")
		}
	}
	return c.fault.CloseError
}
