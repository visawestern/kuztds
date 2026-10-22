//go:build integration

package store

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestCHPostbacksMigrateDedupe builds an old ORDER BY (cid) table in a
// throwaway database, runs the explicit migration, and verifies the new key
// plus dedupe semantics.
func TestCHPostbacksMigrateDedupe(t *testing.T) {
	ch := chFromEnv(t)
	defer ch.Close()
	ctx := context.Background()

	dbName := fmt.Sprintf("itest_migrate_%d", time.Now().UnixNano())
	if err := ch.conn.Exec(ctx, "CREATE DATABASE "+dbName+" ENGINE = Atomic"); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() { _ = ch.conn.Exec(context.Background(), "DROP DATABASE IF EXISTS "+dbName) })

	addr := envDef("KUZTDS_CLICKHOUSE_ADDR", "localhost:9000")
	user := envDef("KUZTDS_CLICKHOUSE_USER", "kuztds")
	pass := envDef("KUZTDS_CLICKHOUSE_PASSWORD", "devpassword")
	mig, err := OpenCH(addr, dbName, user, pass)
	if err != nil {
		t.Fatalf("OpenCH(%s): %v", dbName, err)
	}
	defer mig.Close()

	if err := mig.conn.Exec(ctx, `CREATE TABLE postbacks
	(
		ts          DateTime DEFAULT now(),
		version     DateTime64(3) DEFAULT now64(3),
		domain      String,
		page        String,
		device      LowCardinality(String),
		operator    LowCardinality(String),
		country     LowCardinality(String),
		city        String,
		profit      Float64,
		group_name  LowCardinality(String),
		stream_name LowCardinality(String),
		cid         String,
		status      LowCardinality(String),
		tid         String,
		cost        Float64,
		offer       String
	)
	ENGINE = ReplacingMergeTree(ts)
	PARTITION BY toYYYYMM(ts)
	ORDER BY (cid)
	TTL ts + INTERVAL 365 DAY`); err != nil {
		t.Fatalf("create legacy postbacks: %v", err)
	}
	if err := mig.conn.Exec(ctx, `INSERT INTO postbacks
		(ts, version, domain, page, device, operator, country, city, profit,
		 group_name, stream_name, cid, status, tid, cost, offer)
		VALUES (?, ?, '', '', '', '', '', '', 2.5, 'g', 's', 'c1', 'sale', 't1', 3, '')`,
		time.Now(), time.Now()); err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}

	before, err := mig.PostbacksSortingKey(ctx)
	if err != nil {
		t.Fatalf("sorting key before: %v", err)
	}
	if before != "cid" {
		t.Fatalf("legacy sorting key = %q, expected cid", before)
	}
	applied, err := mig.MigratePostbacksDedupe(ctx)
	if err != nil {
		t.Fatalf("MigratePostbacksDedupe: %v", err)
	}
	if !applied {
		t.Fatal("MigratePostbacksDedupe reported no change")
	}
	after, err := mig.PostbacksSortingKey(ctx)
	if err != nil {
		t.Fatalf("sorting key after: %v", err)
	}
	if strings.ReplaceAll(after, " ", "") != "cid,tid" {
		t.Fatalf("sorting key after = %q, expected cid, tid", after)
	}
	applied, err = mig.MigratePostbacksDedupe(ctx)
	if err != nil || applied {
		t.Fatalf("second migration = applied %v, err %v; expected no-op", applied, err)
	}

	var oldCount uint64
	if err := mig.conn.QueryRow(ctx, "SELECT count() FROM postbacks").Scan(&oldCount); err != nil {
		t.Fatalf("count after migration: %v", err)
	}
	if oldCount != 1 {
		t.Fatalf("count after migration = %d, expected 1", oldCount)
	}

	if err := mig.conn.Exec(ctx, `INSERT INTO postbacks
		(ts, version, domain, page, device, operator, country, city, profit,
		 group_name, stream_name, cid, status, tid, cost, offer)
		VALUES
		(now(), now64(3), '', '', '', '', '', '', 2.5, 'g', 's', 'c1', 'sale', 't1', 3, ''),
		(now(), now64(3), '', '', '', '', '', '', 5.0, 'g', 's', 'c1', 'sale', 't2', 0, '')`); err != nil {
		t.Fatalf("insert post-migration rows: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	var distinct uint64
	var revenue float64
	if err := mig.conn.QueryRow(ctx,
		"SELECT count(), sum(profit) FROM postbacks FINAL").Scan(&distinct, &revenue); err != nil {
		t.Fatalf("read final: %v", err)
	}
	if distinct != 2 || revenue != 7.5 {
		t.Errorf("FINAL = %d rows / %v revenue, expected 2 / 7.5", distinct, revenue)
	}
}
