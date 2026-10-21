package config

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// shareSlugRe is deliberately lowercase: links are pasted into ad cabinets and
// chat apps, and two slugs that differ only by case would be indistinguishable
// to a person reading them.
var shareSlugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,31}$`)

// ValidateShares checks every clean share link and the shared namespace it
// lives in. A slug that equals any group ID or alias would make one of the two
// unreachable: the engine resolves a group first.
func ValidateShares(list []Group) error {
	seen := map[string]string{} // slug -> group id
	known := map[string]string{}
	for _, g := range list {
		known[g.ID] = "group id " + g.ID
		for _, a := range g.Aliases {
			if a != "" {
				known[a] = "alias " + a
			}
		}
	}
	for _, g := range list {
		sh := g.Share
		if sh == nil {
			continue
		}
		slug := strings.TrimSpace(sh.Slug)
		if slug == "" {
			if sh.Title != "" || sh.Description != "" || sh.Image != "" {
				return fmt.Errorf("group %s: a share preview needs a link slug", g.ID)
			}
			continue
		}
		if !shareSlugRe.MatchString(slug) {
			return fmt.Errorf("group %s: share link %q must be 2-32 lowercase letters, digits, - or _", g.ID, slug)
		}
		if what, ok := known[slug]; ok {
			return fmt.Errorf("group %s: share link %q is already the %s", g.ID, slug, what)
		}
		if other, ok := seen[slug]; ok {
			return fmt.Errorf("groups %s and %s share the link %q", other, g.ID, slug)
		}
		seen[slug] = g.ID
		if len([]rune(sh.Title)) > 120 {
			return fmt.Errorf("group %s: share title is longer than 120 characters", g.ID)
		}
		if len([]rune(sh.Description)) > 300 {
			return fmt.Errorf("group %s: share description is longer than 300 characters", g.ID)
		}
		if sh.Image != "" {
			u, err := url.Parse(sh.Image)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return fmt.Errorf("group %s: share image must be an absolute http(s) URL", g.ID)
			}
		}
	}
	return nil
}
