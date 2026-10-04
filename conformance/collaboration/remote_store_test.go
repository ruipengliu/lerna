package collaboration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/runtime"
)

// 显式真实数据库轴；没有PG配置时的skip不能计作该数据库通过。
// 每个测试使用原受信Scope隔离，初始化不重置既有数据库身份。
func openAgentContractStore(t *testing.T, ctx context.Context, driver string) (runtime.Store, string) {
	t.Helper()
	switch driver {
	case "sqlite":
		st, err := sqlite.Open(filepath.Join(t.TempDir(), "agent.sqlite"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := st.Close(); err != nil {
				t.Error(err)
			}
		})
		if err = st.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		return st, st.ID()
	case "postgres":
		dsn := os.Getenv("HARNESS_TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("actual PostgreSQL DSN required for the original parent authority")
		}
		st, err := postgres.Open(ctx, dsn, postgres.WithMaxConnections(4))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := st.Close(); err != nil {
				t.Error(err)
			}
		})
		if err = st.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		return st, st.ID()
	default:
		t.Fatal("remote contract requires an explicit SQLite or PostgreSQL parent")
		return nil, ""
	}
}
