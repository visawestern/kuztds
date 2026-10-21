package config

import "testing"

// G-055: a flow may keep ready-made traffic source templates: each maps a
// query parameter to a short macro label used in the output links.
func TestSourceValidate(t *testing.T) {
	g := Group{
		ID:     "g",
		Source: "google",
		Sources: []TrafficSource{{
			ID: "google", Name: "Google Ads",
			Labels: []SourceLabel{
				{Param: "gclid", Label: "gclid"},
				{Param: "utm_source", Label: "UTM_SOURCE"},
				{Param: "utm_campaign", Label: "campaign_1"},
			},
		}},
	}
	if err := g.ValidateSources(); err != nil {
		t.Errorf("valid source: %v", err)
	}

	cases := map[string]Group{
		"unknown active source": {Source: "nope"},
		"source without id": {Sources: []TrafficSource{{
			Labels: []SourceLabel{{Param: "gclid", Label: "GCLID"}},
		}}},
		"bad source id": {Sources: []TrafficSource{{ID: "goo gle"}}},
		"duplicate source id": {Sources: []TrafficSource{
			{ID: "a"}, {ID: "a"},
		}},
		"label without param":    {Sources: []TrafficSource{{ID: "a", Labels: []SourceLabel{{Label: "GCLID"}}}}},
		"param with a separator": {Sources: []TrafficSource{{ID: "a", Labels: []SourceLabel{{Param: "a&b", Label: "GCLID"}}}}},
		"bad label":              {Sources: []TrafficSource{{ID: "a", Labels: []SourceLabel{{Param: "gclid", Label: "G-CLID"}}}}},
		"reserved label":         {Sources: []TrafficSource{{ID: "a", Labels: []SourceLabel{{Param: "cid", Label: "CID"}}}}},
		"duplicate label": {Sources: []TrafficSource{{ID: "a", Labels: []SourceLabel{
			{Param: "gclid", Label: "GCLID"},
			{Param: "gbraid", Label: "gclid"},
		}}}},
	}
	for name, g := range cases {
		if err := g.ValidateSources(); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestSourceTemplateLookup(t *testing.T) {
	g := Group{
		Source: "google",
		Sources: []TrafficSource{
			{ID: "meta", Name: "Meta"},
			{ID: "google", Name: "Google Ads"},
		},
	}
	s, ok := g.SourceTemplate("google")
	if !ok || s.Name != "Google Ads" {
		t.Errorf("SourceTemplate(google) = (%+v,%v)", s, ok)
	}
	if _, ok := g.SourceTemplate("nope"); ok {
		t.Error("unknown source reported found")
	}
	if _, ok := g.SourceTemplate(""); ok {
		t.Error("empty source reported found")
	}
}
