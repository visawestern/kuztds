package config

import "testing"

// G-059: a clean share link is a short pretty URL with an Open Graph preview.
// The slug lives in the same namespace as group IDs and aliases, otherwise the
// engine could not tell which one a request means.
func TestValidateShares(t *testing.T) {
	ok := []Group{
		{ID: "g1", Aliases: []string{"one"}, Share: &Share{
			Slug: "summer-preview", Title: "Summer picks",
			Description: "What is inside", Image: "https://cdn.example/p.jpg",
		}},
		{ID: "g2"},
	}
	if err := ValidateShares(ok); err != nil {
		t.Fatalf("valid shares rejected: %v", err)
	}
	if err := ValidateShares([]Group{{ID: "empty", Share: &Share{}}}); err != nil {
		t.Errorf("an empty share block must be allowed: %v", err)
	}
	if err := ValidateShares([]Group{{ID: "long", Share: &Share{Slug: "pro", Description: string(make([]byte, 301))}}}); err == nil {
		t.Error("a 301-character description must be rejected")
	}
	if err := ValidateShares([]Group{{ID: "bad", Share: &Share{Slug: "pro", Image: "http://[::1"}}}); err == nil {
		t.Error("an unparsable image URL must be rejected")
	}
	for name, list := range map[string][]Group{
		"slug too short":        {{ID: "g", Share: &Share{Slug: "a"}}},
		"slug with a space":     {{ID: "g", Share: &Share{Slug: "summer picks"}}},
		"uppercase slug":        {{ID: "g", Share: &Share{Slug: "Summer"}}},
		"slug same as id":       {{ID: "promo", Share: &Share{Slug: "promo"}}},
		"slug same as alias":    {{ID: "g", Aliases: []string{"promo"}, Share: &Share{Slug: "promo"}}},
		"slug same as other id": {{ID: "promo"}, {ID: "g", Share: &Share{Slug: "promo"}}},
		"duplicate slugs":       {{ID: "a", Share: &Share{Slug: "promo"}}, {ID: "b", Share: &Share{Slug: "promo"}}},
		"relative image":        {{ID: "g", Share: &Share{Slug: "promo", Image: "/p.jpg"}}},
		"ftp image":             {{ID: "g", Share: &Share{Slug: "promo", Image: "ftp://h/p.jpg"}}},
		"fields without slug":   {{ID: "g", Share: &Share{Title: "No link"}}},
		"long title":            {{ID: "g", Share: &Share{Slug: "promo", Title: string(make([]byte, 121))}}},
	} {
		if err := ValidateShares(list); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestGroupsGetShare(t *testing.T) {
	gg := NewGroups(
		&Group{ID: "g", Share: &Share{Slug: "summer"}},
		&Group{ID: "plain"},
	)
	if grp, ok := gg.GetShare("summer"); !ok || grp.ID != "g" {
		t.Fatalf("GetShare(summer) = (%+v,%v)", grp, ok)
	}
	if _, ok := gg.GetShare("missing"); ok {
		t.Error("GetShare(missing) must miss")
	}
	if _, ok := gg.GetShare(""); ok {
		t.Error("an empty slug is not a share link")
	}
	if _, ok := gg.Get("summer"); ok {
		t.Error("a share slug must not become a group id/alias")
	}
}
