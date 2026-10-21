package store

import (
	"strings"
	"testing"
)

// Revenue-сплит trash (G-302): чистые хелперы агрегатов постбеков.
// Пустой status (строки до миграции 004) считается обычной выручкой.

func TestProfitSumsWithColumns(t *testing.T) {
	rev, trash := profitSums(true)
	if !strings.Contains(rev, "status != 'trash'") {
		t.Errorf("revenue expr = %q, must exclude trash", rev)
	}
	if !strings.Contains(trash, "status = 'trash'") {
		t.Errorf("trash expr = %q, must select trash only", trash)
	}
}

func TestProfitSumsLegacy(t *testing.T) {
	rev, trash := profitSums(false)
	if rev != "sum(profit)" {
		t.Errorf("legacy revenue expr = %q, want sum(profit)", rev)
	}
	if trash != "0" {
		t.Errorf("legacy trash expr = %q, want 0", trash)
	}
}

func TestConvCountsWithColumns(t *testing.T) {
	all, trash := convCounts(true)
	if all != "count()" {
		t.Errorf("conv all = %q, want count()", all)
	}
	if !strings.Contains(trash, "status = 'trash'") {
		t.Errorf("conv trash = %q, must count trash only", trash)
	}
}

func TestPostbackRowCols(t *testing.T) {
	with := postbackRowCols(true, false)
	for _, c := range []string{"status", "tid", "cost", "profit", "cid"} {
		if !strings.Contains(with, c) {
			t.Errorf("row cols = %q, missing %s", with, c)
		}
	}
	legacy := postbackRowCols(false, false)
	if strings.Contains(legacy, "status") || strings.Contains(legacy, "tid") {
		t.Errorf("legacy row cols = %q, must not mention status/tid", legacy)
	}
}

// G-053: the offer rides with the conversion so reports can split profit and
// cost per offer. Old tables without the column keep working.
func TestPostbackOfferColumns(t *testing.T) {
	if cols := postbackRowCols(true, true); !strings.Contains(cols, "offer") {
		t.Errorf("row cols = %q, missing offer", cols)
	}
	if cols := postbackExtraInsert(true, true); !strings.Contains(cols, "offer") {
		t.Errorf("insert suffix = %q, missing offer", cols)
	}
	if cols := postbackExtraInsert(true, false); strings.Contains(cols, "offer") {
		t.Errorf("insert suffix = %q, offer without the column", cols)
	}
	if cols := postbackExtraInsert(false, false); cols != "" {
		t.Errorf("legacy insert suffix = %q, want empty", cols)
	}
}
