//go:build integration

package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/egerkuzma/kuztds/internal/logbuf"
)

// TestCHPostbackDedupe covers the money path: a repeated (cid,tid) must not
// double revenue or conversion cost, while a second tid of the same click is
// a distinct conversion.
func TestCHPostbackDedupe(t *testing.T) {
	ch := chFromEnv(t)
	defer ch.Close()
	ctx := context.Background()

	group := fmt.Sprintf("itest_dedupe_%d", time.Now().UnixNano())
	cid1 := group + "_c1"
	cid2 := group + "_c2"
	now := time.Now()
	from := now.Add(-time.Minute)
	to := now.Add(time.Minute)
	t.Cleanup(func() {
		_ = ch.conn.Exec(ctx, "ALTER TABLE events DELETE WHERE group_name = ?", group)
		_ = ch.conn.Exec(ctx, "ALTER TABLE postbacks DELETE WHERE group_name = ?", group)
	})

	events := []logbuf.Event{
		{Ts: now, GroupID: group, GroupName: group, Stream: "s1", Out: "o1",
			Device: "phone", Country: "ru", City: "msk", Uniq: 1, IP: "1.1.1.1", CID: cid1},
		{Ts: now, GroupID: group, GroupName: group, Stream: "s1", Out: "o2",
			Device: "phone", Country: "ru", City: "msk", Uniq: 1, IP: "1.1.1.2", CID: cid2},
	}
	if err := ch.InsertEvents(ctx, events); err != nil {
		t.Fatalf("InsertEvents: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	// First sale, its redelivery, and a second transaction of the same click.
	for _, pb := range []struct {
		cid    string
		profit float64
		cost   float64
		tid    string
		status string
	}{
		{cid1, 2.5, 3.0, "t-1", "sale"},
		{cid1, 2.5, 3.0, "t-1", "sale"},
		{cid1, 5.0, 0, "t-2", "sale"},
		{cid2, 10.0, 0, "t-3", "trash"},
	} {
		if _, err := ch.RecordPostback(ctx, pb.cid, pb.profit, pb.status, pb.tid, "", pb.cost, PostbackDedupeUpdate); err != nil {
			t.Fatalf("RecordPostback(%s,%s): %v", pb.cid, pb.tid, err)
		}
	}
	time.Sleep(300 * time.Millisecond)

	rows, revenue, trash, err := ch.Postbacks(ctx, from, to, group, "", 100)
	if err != nil {
		t.Fatalf("Postbacks: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("Postbacks rows = %d, expected 3 (duplicate tid must collapse): %+v", len(rows), rows)
	}
	if revenue != 7.5 || trash != 10.0 {
		t.Errorf("revenue=%v trash=%v, expected 7.5 and 10.0", revenue, trash)
	}
	seen := map[string]int{}
	for _, r := range rows {
		seen[r.TID]++
	}
	if seen["t-1"] != 1 || seen["t-2"] != 1 || seen["t-3"] != 1 {
		t.Errorf("tid counts = %v, expected exactly one of each", seen)
	}

	perf, err := ch.Performance(ctx, from, to)
	if err != nil {
		t.Fatalf("Performance: %v", err)
	}
	var got *PerfRow
	for i := range perf {
		if perf[i].Group == group && perf[i].Stream == "s1" {
			got = &perf[i]
			break
		}
	}
	if got == nil {
		t.Fatal("Performance has no row for the test group/stream")
	}
	if got.Conv != 3 || got.Cost != 3.0 || got.Profit != 4.5 || got.Trash != 1 || got.TrashProfit != 10.0 {
		t.Errorf("Performance = %+v, expected 3 conv / cost 3 / net 4.5 / trash 1 of 10", *got)
	}

	statuses, err := ch.PostbackStatuses(ctx, from, to, group)
	if err != nil {
		t.Fatalf("PostbackStatuses: %v", err)
	}
	counts := map[string]int64{}
	for _, kv := range statuses {
		counts[kv.Key] = kv.Count
	}
	if counts["sale"] != 2 || counts["trash"] != 1 {
		t.Errorf("status counts = %v, expected sale=2 trash=1", counts)
	}
}

// TestCHPostbackDedupeModes covers G-299: update replaces, ignore keeps the
// first message, and new records one row per redelivery.
func TestCHPostbackDedupeModes(t *testing.T) {
	ch := chFromEnv(t)
	defer ch.Close()
	ctx := context.Background()

	group := fmt.Sprintf("itest_modes_%d", time.Now().UnixNano())
	cid := group + "_c1"
	now := time.Now()
	from := now.Add(-time.Minute)
	to := now.Add(time.Minute)
	t.Cleanup(func() {
		_ = ch.conn.Exec(ctx, "ALTER TABLE events DELETE WHERE group_name = ?", group)
		_ = ch.conn.Exec(ctx, "ALTER TABLE postbacks DELETE WHERE group_name = ?", group)
	})
	if err := ch.InsertEvents(ctx, []logbuf.Event{{
		Ts: now, GroupID: group, GroupName: group, Stream: "s1", Out: "o1",
		Device: "phone", Country: "ru", City: "msk", Uniq: 1, IP: "1.1.1.3", CID: cid,
	}}); err != nil {
		t.Fatalf("InsertEvents: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	record := func(mode string, profit float64) bool {
		t.Helper()
		recorded, err := ch.RecordPostback(ctx, cid, profit, "sale", "t-1", "", 0, mode)
		if err != nil {
			t.Fatalf("RecordPostback(%s): %v", mode, err)
		}
		return recorded
	}
	if !record(PostbackDedupeUpdate, 2.5) {
		t.Fatal("first update was not recorded")
	}
	if !record(PostbackDedupeUpdate, 4.0) {
		t.Fatal("second update was not recorded")
	}
	if record(PostbackDedupeIgnore, 9.0) {
		t.Fatal("ignore recorded a duplicate")
	}
	rows, revenue, _, err := ch.Postbacks(ctx, from, to, group, "", 100)
	if err != nil {
		t.Fatalf("Postbacks: %v", err)
	}
	if len(rows) != 1 || revenue != 4.0 {
		t.Fatalf("after update+ignore = %d rows / %v revenue, want 1 / 4.0", len(rows), revenue)
	}
	if !record(PostbackDedupeNew, 6.0) {
		t.Fatal("new did not record the second conversion")
	}
	rows, revenue, _, err = ch.Postbacks(ctx, from, to, group, "", 100)
	if err != nil {
		t.Fatalf("Postbacks: %v", err)
	}
	if len(rows) != 2 || revenue != 10.0 {
		t.Fatalf("after new = %d rows / %v revenue, want 2 / 10.0", len(rows), revenue)
	}
}

// TestCHPostbackHistoricalTimestamp covers G-064: an imported purchase keeps
// its own date in postbacks.ts instead of the import time.
func TestCHPostbackHistoricalTimestamp(t *testing.T) {
	ch := chFromEnv(t)
	defer ch.Close()
	ctx := context.Background()

	group := fmt.Sprintf("itest_hist_%d", time.Now().UnixNano())
	cid := group + "_c1"
	now := time.Now()
	at := now.Add(-48 * time.Hour).Truncate(time.Second)
	t.Cleanup(func() {
		_ = ch.conn.Exec(ctx, "ALTER TABLE events DELETE WHERE group_name = ?", group)
		_ = ch.conn.Exec(ctx, "ALTER TABLE postbacks DELETE WHERE group_name = ?", group)
	})
	if err := ch.InsertEvents(ctx, []logbuf.Event{{
		Ts: now, GroupID: group, GroupName: group, Stream: "s1", Out: "o1",
		Device: "phone", Country: "ru", City: "msk", Uniq: 1, IP: "1.1.1.4", CID: cid,
	}}); err != nil {
		t.Fatalf("InsertEvents: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if _, err := ch.RecordPostbackAt(ctx, cid, 12, "sale", "t-old", "", 0, PostbackDedupeUpdate, at); err != nil {
		t.Fatalf("RecordPostbackAt: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	rows, revenue, _, err := ch.Postbacks(ctx, at.Add(-time.Minute), at.Add(time.Minute), group, "", 10)
	if err != nil {
		t.Fatalf("Postbacks: %v", err)
	}
	if len(rows) != 1 || revenue != 12 {
		t.Fatalf("historical window = %d rows / %v revenue, want 1 / 12", len(rows), revenue)
	}
	if !rows[0].TS.Equal(at) {
		t.Errorf("ts = %s, want %s", rows[0].TS, at)
	}
	recent, _, _, err := ch.Postbacks(ctx, now.Add(-time.Hour), now.Add(time.Minute), group, "", 10)
	if err != nil {
		t.Fatalf("recent Postbacks: %v", err)
	}
	if len(recent) != 0 {
		t.Errorf("historical import leaked into the recent window: %+v", recent)
	}
}
