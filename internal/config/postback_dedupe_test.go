package config

import "testing"

func TestPostbackDedupeMode(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", PostbackDedupeUpdate},
		{"update", PostbackDedupeUpdate},
		{" UPDATE ", PostbackDedupeUpdate},
		{"ignore", PostbackDedupeIgnore},
		{"new", PostbackDedupeNew},
		{"unknown", PostbackDedupeUpdate},
	}
	for _, c := range cases {
		g := Group{PostbackDedupe: c.in}
		if got := g.PostbackDedupeMode(); got != c.want {
			t.Errorf("PostbackDedupeMode(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestValidatePostbackDedupe(t *testing.T) {
	for _, ok := range []string{"", "update", "IGNORE", " new "} {
		if err := (Group{ID: "g", PostbackDedupe: ok}).ValidatePostbackDedupe(); err != nil {
			t.Errorf("mode %q rejected: %v", ok, err)
		}
	}
	if err := (Group{ID: "g", PostbackDedupe: "upsert"}).ValidatePostbackDedupe(); err == nil {
		t.Fatal("unknown mode accepted")
	}
}
