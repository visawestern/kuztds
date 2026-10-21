package conversion

import "testing"

// G-301/302/303: системные статусы + trash + кастомные (Keitaro 11 паритет).
// G-304: ключ дедупа по tid.

func TestParseStatusKnown(t *testing.T) {
	for _, s := range []string{"lead", "sale", "rejected", "registration", "deposit", "trash"} {
		got, ok := ParseStatus(s)
		if !ok || got != s {
			t.Errorf("ParseStatus(%q) = (%q,%v), want (%q,true)", s, got, ok, s)
		}
	}
}

func TestParseStatusCaseInsensitiveAndTrimmed(t *testing.T) {
	got, ok := ParseStatus(" SALE ")
	if !ok || got != "sale" {
		t.Errorf("ParseStatus(\" SALE \") = (%q,%v), want (sale,true)", got, ok)
	}
}

func TestParseStatusEmptyIgnored(t *testing.T) {
	// Keitaro 11 breaking change: без status= постбек игнорируется.
	if _, ok := ParseStatus(""); ok {
		t.Errorf("ParseStatus(\"\") = ok, want ignored")
	}
	if _, ok := ParseStatus("   "); ok {
		t.Errorf("ParseStatus(spaces) = ok, want ignored")
	}
}

func TestParseStatusCustom(t *testing.T) {
	got, ok := ParseStatus("vip_level2")
	if !ok || got != "vip_level2" {
		t.Errorf("ParseStatus(custom) = (%q,%v), want (vip_level2,true)", got, ok)
	}
}

func TestParseStatusRejectsGarbage(t *testing.T) {
	for _, s := range []string{
		"sale'; DROP TABLE postbacks;--",
		"a/b",
		"with space inside",
		"UPPER-WITH-DASH",
		"0123456789012345678901234567890123", // 33 chars > 32
		"статус",
	} {
		if _, ok := ParseStatus(s); ok {
			t.Errorf("ParseStatus(%q) = ok, want rejected", s)
		}
	}
}

func TestRevenueByStatus(t *testing.T) {
	// trash обнуляет Revenue, но сохраняет сумму в Revenue(trash).
	if got := RevenueByStatus("sale", 10.5); got != 10.5 {
		t.Errorf("RevenueByStatus(sale,10.5) = %v, want 10.5", got)
	}
	if got := RevenueByStatus("trash", 10.5); got != 0 {
		t.Errorf("RevenueByStatus(trash,10.5) = %v, want 0", got)
	}
	if got := TrashRevenueByStatus("trash", 10.5); got != 10.5 {
		t.Errorf("TrashRevenueByStatus(trash,10.5) = %v, want 10.5", got)
	}
	if got := TrashRevenueByStatus("sale", 10.5); got != 0 {
		t.Errorf("TrashRevenueByStatus(sale,10.5) = %v, want 0", got)
	}
}

func TestDedupKey(t *testing.T) {
	if got := DedupKey("cid1", ""); got != "cid1" {
		t.Errorf("DedupKey(cid1,\"\") = %q, want cid1", got)
	}
	if got := DedupKey("cid1", "2"); got != "4:cid1\x001:2" {
		t.Errorf("DedupKey(cid1,2) = %q, want length-prefixed", got)
	}
	// tid с разделителем внутри не должен склеиваться с чужим ключом.
	if DedupKey("a\x00b", "c") == DedupKey("a", "b\x00c") {
		t.Errorf("DedupKey collision on embedded NUL")
	}
}
