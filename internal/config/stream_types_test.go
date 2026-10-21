package config

import "testing"

// G-127: a stream is forced, regular or the single default fallback.
func TestValidateStreams(t *testing.T) {
	valid := Group{ID: "g", Streams: []Stream{
		{Name: "vip", Status: true, Type: "forced"},
		{Name: "usual", Status: true, Type: "regular"},
		{Name: "legacy", Status: true}, // empty type = regular, old configs
		{Name: "rest", Status: true, Type: "default"},
	}}
	if err := valid.ValidateStreams(); err != nil {
		t.Fatalf("valid streams rejected: %v", err)
	}
	for name, g := range map[string]Group{
		"unknown type": {ID: "g", Streams: []Stream{{Name: "s", Type: "fallback"}}},
		"wrong case":   {ID: "g", Streams: []Stream{{Name: "s", Type: "Forced"}}},
		"two defaults": {ID: "g", Streams: []Stream{
			{Name: "a", Type: "default"}, {Name: "b", Type: "default"},
		}},
		"disabled default still counts": {ID: "g", Streams: []Stream{
			{Name: "a", Type: "default", Status: false}, {Name: "b", Type: "default"},
		}},
	} {
		if err := g.ValidateStreams(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
