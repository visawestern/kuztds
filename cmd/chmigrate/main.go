// Command chmigrate applies explicit ClickHouse migrations that ALTER cannot
// express, currently the postbacks (cid, tid, dedupe_key) rebuild. It is intentionally not
// run by the engine: the operator starts it, reviews the dry-run, then passes
// -apply.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/egerkuzma/kuztds/internal/store"
)

func main() {
	apply := flag.Bool("apply", false, "perform the migration; without it only report the current sorting key")
	flag.Parse()
	addr := env("KUZTDS_CLICKHOUSE_ADDR", "localhost:9000")
	db := env("KUZTDS_CLICKHOUSE_DB", "kuztds")
	user := env("KUZTDS_CLICKHOUSE_USER", "kuztds")
	pass := os.Getenv("KUZTDS_CLICKHOUSE_PASSWORD")
	ch, err := store.OpenCH(addr, db, user, pass)
	if err != nil {
		log.Fatalf("clickhouse: %v", err)
	}
	defer ch.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	key, err := ch.PostbacksSortingKey(ctx)
	if err != nil {
		log.Fatalf("postbacks: %v", err)
	}
	fmt.Printf("postbacks sorting key: %s\n", key)
	if !*apply {
		fmt.Println("dry run: pass -apply to upgrade to ORDER BY (cid, tid, dedupe_key)")
		return
	}
	applied, err := ch.MigratePostbacksDedupe(ctx)
	if err != nil {
		log.Fatalf("migration: %v", err)
	}
	if !applied {
		fmt.Println("already migrated")
		return
	}
	fmt.Println("migrated: postbacks now supports update/ignore/new duplicate modes")
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
