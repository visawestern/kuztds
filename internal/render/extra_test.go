package render

import "testing"

// G-055: traffic source templates add their own macros, filled from the query.
func TestExpandExtraMacros(t *testing.T) {
	d := MacroDeps{Extra: map[string]string{"GCLID": "abc", "SRC": "", "EVIL": "[CID]"}}
	got := Expand("https://x/?g=[GCLID]&s=[SRC]&p=[PAR-1]&e=[EVIL]&u=[NOPE]", d)
	want := "https://x/?g=abc&s=&p=&e=[CID]&u=[NOPE]"
	if got != want {
		t.Errorf("Expand = %q, want %q", got, want)
	}
}
