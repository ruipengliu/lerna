package content

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	d "github.com/ruipengliu/lerna/domain/content"
	"io"
	"strings"
	"testing"
)

// This driver controls only database/sql Rows failure mechanics. No PostgreSQL
// connection or native PostgreSQL Close failure is claimed by these tests.
type pageConnector struct {
	data              [][]driver.Value
	closeErr, nextErr error
}

func (c pageConnector) Connect(context.Context) (driver.Conn, error) { return pageConnection{c}, nil }
func (c pageConnector) Driver() driver.Driver                        { return pageDriver{} }

type pageDriver struct{}

func (pageDriver) Open(string) (driver.Conn, error) { return nil, errors.New("connector only") }

type pageConnection struct{ pageConnector }

func (pageConnection) Prepare(string) (driver.Stmt, error) { return nil, errors.New("no statements") }
func (pageConnection) Begin() (driver.Tx, error)           { return nil, errors.New("no transactions") }
func (pageConnection) Close() error                        { return nil }
func (c pageConnection) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &pageRows{pageConnector: c.pageConnector}, nil
}

type pageRows struct {
	pageConnector
	index int
}

func (*pageRows) Columns() []string { return []string{"body", "cursor"} }
func (r *pageRows) Close() error    { return r.closeErr }
func (r *pageRows) Next(dest []driver.Value) error {
	if r.index == len(r.data) {
		if r.nextErr != nil {
			return r.nextErr
		}
		return io.EOF
	}
	copy(dest, r.data[r.index])
	r.index++
	return nil
}
func TestManagementPageRowsPreserveCloseAndPrimaryCauses(t *testing.T) {
	ref := v.ContentRef{Owner: v.OwnerRef{TenantID: "t", OwnerID: "o"}, ContentID: "a", Version: "1", Hash: "sha256:b6a98d9ce9a2d9149288fa3df42d377c3e42737afdcdaf714e33c0a100b51060", MediaType: "text/plain", ByteLength: "6"}
	type consumer struct {
		name string
		body []byte
		read func(*sql.Rows) (int, string, error)
	}
	marshal := func(x any) []byte {
		b, e := json.Marshal(x)
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	consumers := []consumer{
		{"policies", marshal(d.FixturePolicy{}), func(r *sql.Rows) (int, string, error) {
			p, n, e := readPoliciesForVersionPage(r, 1)
			return len(p), n, e
		}},
		{"descendants", marshal(ref), func(r *sql.Rows) (int, string, error) { p, n, e := readDescendantsPage(r, 1); return len(p), n, e }},
		{"responsibilities", marshal(d.CleanupResponsibility{}), func(r *sql.Rows) (int, string, error) { p, n, e := readResponsibilitiesPage(r, 1); return len(p), n, e }},
		{"admissions", marshal(d.PolicyChange{}), func(r *sql.Rows) (int, string, error) { p, n, e := readAdmissionChangesPage(r, 1); return len(p), n, e }},
	}
	for _, c := range consumers {
		for _, mode := range []string{"normal", "early-close", "decode-close", "scan-close", "iteration"} {
			t.Run(c.name+"/"+mode, func(t *testing.T) {
				closeCause := errors.New("mechanical rows close cause")
				nextCause := errors.New("mechanical iteration cause")
				fault := pageConnector{data: [][]driver.Value{{c.body, "first"}, {c.body, "second"}}}
				switch mode {
				case "early-close":
					fault.closeErr = closeCause
				case "decode-close":
					fault.data[0][0] = []byte("{")
					fault.closeErr = closeCause
				case "scan-close":
					fault.data[0][1] = nil
					fault.closeErr = closeCause
				case "iteration":
					fault.data = nil
					fault.nextErr = nextCause
				}
				db := sql.OpenDB(fault)
				defer func() {
					if e := db.Close(); e != nil {
						t.Error(e)
					}
				}()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				rows, err := db.QueryContext(ctx, "mechanical")
				if err != nil {
					t.Fatal(err)
				}
				count, next, err := c.read(rows)
				switch mode {
				case "normal":
					if err != nil || count != 1 || next != "first" {
						t.Fatal("normal page", count, next, err)
					}
				case "early-close":
					if !errors.Is(err, closeCause) || count != 1 || next != "first" {
						t.Fatal("early page close cause lost", count, next, err)
					}
				case "decode-close":
					var syntax *json.SyntaxError
					if !errors.Is(err, closeCause) || !errors.As(err, &syntax) {
						t.Fatal("decode primary or close cause lost", err)
					}
				case "scan-close":
					if !errors.Is(err, closeCause) || !strings.Contains(err.Error(), "Scan error") {
						t.Fatal("scan primary or close cause lost", err)
					}
				case "iteration":
					if !errors.Is(err, nextCause) {
						t.Fatal("iteration cause lost", err)
					}
				}
			})
		}
	}
}
