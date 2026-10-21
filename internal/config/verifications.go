package config

import (
	"fmt"
	"regexp"
	"strings"
)

// Verification is a file an ad cabinet asks to find at the tracker's root to
// prove the domain belongs to the operator (G-060). The engine serves it at
// /<name>; the content is what the cabinet gave you to upload.
type Verification struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// Verifications is the document stored in KUZTDS_VERIFY_FILE.
type Verifications struct {
	Files []Verification `json:"files"`
}

// MaxVerificationBytes bounds one file. Verification files are tiny; a large
// one is a mistake or an attempt to turn the tracker into a file host.
const MaxVerificationBytes = 64 << 10

var verificationNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

// ValidVerificationName reports whether a name can be served at the root.
// The engine applies the same check when it loads the file, so a hand-edited
// config cannot publish a name with a path separator.
func ValidVerificationName(name string) bool {
	if !verificationNameRe.MatchString(name) || strings.Contains(name, "..") {
		return false
	}
	return !strings.EqualFold(name, "healthz")
}

// ValidateVerifications checks the files and the namespace they live in.
// Verification names share the root with group IDs, aliases and clean links;
// the engine resolves a group first, so a collision would silently hide the
// verification file and leave the domain unverified.
func ValidateVerifications(files []Verification, groups []Group) error {
	taken := map[string]string{}
	for _, g := range groups {
		if g.ID != "" {
			taken[strings.ToLower(g.ID)] = "flow ID " + g.ID
		}
		for _, a := range g.Aliases {
			if a != "" {
				taken[strings.ToLower(a)] = "alias " + a
			}
		}
		if g.Share != nil && g.Share.Slug != "" {
			taken[strings.ToLower(g.Share.Slug)] = "clean link " + g.Share.Slug
		}
	}
	seen := map[string]bool{}
	for _, f := range files {
		name := strings.TrimSpace(f.Name)
		if name == "" {
			return fmt.Errorf("a verification file needs a name")
		}
		if !ValidVerificationName(name) {
			return fmt.Errorf("verification file %q: use 1-100 letters, digits, dots, - or _ (not healthz)", name)
		}
		key := strings.ToLower(name)
		if seen[key] {
			return fmt.Errorf("duplicate verification file %q", name)
		}
		seen[key] = true
		if what, ok := taken[key]; ok {
			return fmt.Errorf("verification file %q is already the %s", name, what)
		}
		if strings.TrimSpace(f.Content) == "" {
			return fmt.Errorf("verification file %q is empty", name)
		}
		if len(f.Content) > MaxVerificationBytes {
			return fmt.Errorf("verification file %q is larger than 64 KiB", name)
		}
	}
	return nil
}
