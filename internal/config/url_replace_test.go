package config

import "testing"

// G-130: URL replacements need something to find; the rest is free text.
func TestValidateStreamsURLReplace(t *testing.T) {
	ok := Group{ID: "g", Streams: []Stream{{Name: "s", URLReplace: []URLEdit{
		{Find: "OLD", Replace: "NEW"},
		{Find: "/a", Replace: ""}, // deletion is allowed
	}}}}
	if err := ok.ValidateStreams(); err != nil {
		t.Fatalf("valid replacements rejected: %v", err)
	}
	for name, r := range map[string]URLEdit{
		"empty find":      {Replace: "NEW"},
		"whitespace find": {Find: "   ", Replace: "NEW"},
	} {
		g := Group{ID: "g", Streams: []Stream{{Name: "s", URLReplace: []URLEdit{r}}}}
		if err := g.ValidateStreams(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
