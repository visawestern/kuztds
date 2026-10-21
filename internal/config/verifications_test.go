package config

import "testing"

// G-060: ad cabinets verify a domain by a file at its root. The name must be
// safe to serve and must not shadow a flow.
func TestValidateVerifications(t *testing.T) {
	groups := []Group{
		{ID: "promo", Aliases: []string{"sale"}, Share: &Share{Slug: "clean"}},
	}
	ok := []Verification{
		{Name: "google123.html", Content: "google-site-verification: x"},
		{Name: "facebook-verify.txt", Content: "fb"},
	}
	if err := ValidateVerifications(ok, groups); err != nil {
		t.Fatalf("valid files rejected: %v", err)
	}
	for name, files := range map[string][]Verification{
		"no name":                 {{Content: "x"}},
		"path traversal":          {{Name: "../x", Content: "x"}},
		"slash":                   {{Name: "a/b.html", Content: "x"}},
		"healthz":                 {{Name: "healthz", Content: "x"}},
		"empty content":           {{Name: "a.html"}},
		"whitespace content":      {{Name: "a.html", Content: "  \n"}},
		"duplicate ignoring case": {{Name: "A.html", Content: "x"}, {Name: "a.HTML", Content: "y"}},
		"collides with a flow id": {{Name: "promo", Content: "x"}},
		"collides with an alias":  {{Name: "SALE", Content: "x"}},
		"collides with a clean":   {{Name: "clean", Content: "x"}},
		"oversized":               {{Name: "big.html", Content: string(make([]byte, MaxVerificationBytes+1))}},
	} {
		if err := ValidateVerifications(files, groups); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}
