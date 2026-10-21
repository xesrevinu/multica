// Command orchtestdb creates the orchestrator integration-test database if it
// does not exist. Used by scripts/orch-testdb.sh (no psql needed).
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
)

func main() {
	base, name := os.Args[1], os.Args[2]
	ctx := context.Background()
	c, err := pgx.Connect(ctx, base+"/postgres?sslmode=disable")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer c.Close(ctx)
	var n int
	_ = c.QueryRow(ctx, `SELECT count(*) FROM pg_database WHERE datname=$1`, name).Scan(&n)
	if n == 0 {
		if _, err := c.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("created", name)
	}
}
