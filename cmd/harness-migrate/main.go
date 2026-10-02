// harness-migrate is an explicit, local schema control action.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ruipengliu/lerna/internal/storage/postgres"
	"github.com/ruipengliu/lerna/internal/storage/sqlite"
)

func main() {
	driver := flag.String("driver", "", "postgres or sqlite")
	urlEnv := flag.String("url-env", "LERNA_MIGRATION_DATABASE_URL", "environment name for privileged migration URL")
	path := flag.String("path", "", "absolute SQLite database path")
	flag.Parse()
	if flag.NArg() != 0 || (*driver != "postgres" && *driver != "sqlite") || (*driver == "postgres" && (*path != "" || os.Getenv(*urlEnv) == "")) || (*driver == "sqlite" && !filepath.IsAbs(*path)) {
		fmt.Fprintln(os.Stderr, "usage: harness-migrate --driver postgres --url-env ENV | --driver sqlite --path /absolute/database.db")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var err error
	if *driver == "postgres" {
		err = postgres.Migrate(ctx, os.Getenv(*urlEnv))
	} else {
		err = sqlite.Migrate(ctx, *path)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "migration failed; check explicit credentials, database ownership, original file lock and schema version")
		os.Exit(1)
	}
	fmt.Println("durable schema version 1 is ready")
}
