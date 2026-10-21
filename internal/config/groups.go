package config

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// Groups — a thread-safe set of groups with lookup by ID and hot-reload.
// The groups and streams configuration is stored in JSON format.
type Groups struct {
	mu      sync.RWMutex
	byID    map[string]*Group
	byAlias map[string]*Group
	byShare map[string]*Group
}

func index(list []Group) (map[string]*Group, map[string]*Group, map[string]*Group) {
	byID := make(map[string]*Group, len(list))
	byAlias := make(map[string]*Group)
	byShare := make(map[string]*Group)
	for i := range list {
		grp := &list[i]
		byID[grp.ID] = grp
		for _, a := range grp.Aliases {
			if a != "" {
				byAlias[a] = grp
			}
		}
		if grp.Share != nil && grp.Share.Slug != "" {
			byShare[grp.Share.Slug] = grp
		}
	}
	return byID, byAlias, byShare
}

// LoadGroups reads groups from a JSON file (an array of Group).
func LoadGroups(path string) (*Groups, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	var list []Group
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	byID, byAlias, byShare := index(list)
	return &Groups{byID: byID, byAlias: byAlias, byShare: byShare}, nil
}

// NewGroups creates a set from a ready-made list (for tests/embedded config).
func NewGroups(list ...*Group) *Groups {
	g := &Groups{byID: make(map[string]*Group, len(list)), byAlias: map[string]*Group{}, byShare: map[string]*Group{}}
	for _, grp := range list {
		g.byID[grp.ID] = grp
		for _, a := range grp.Aliases {
			if a != "" {
				g.byAlias[a] = grp
			}
		}
		if grp.Share != nil && grp.Share.Slug != "" {
			g.byShare[grp.Share.Slug] = grp
		}
	}
	return g
}

// Get returns a group by ID or alias.
func (g *Groups) Get(id string) (*Group, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if grp, ok := g.byID[id]; ok {
		return grp, true
	}
	grp, ok := g.byAlias[id]
	return grp, ok
}

// GetShare returns a group by its clean share link (G-059).
func (g *Groups) GetShare(slug string) (*Group, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	grp, ok := g.byShare[slug]
	return grp, ok
}

// List returns all groups (for the admin API).
func (g *Groups) List() []Group {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]Group, 0, len(g.byID))
	for _, grp := range g.byID {
		out = append(out, *grp)
	}
	return out
}

// Replace atomically swaps the contents (for hot-reload).
func (g *Groups) Replace(other *Groups) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.byID = other.byID
	g.byAlias = other.byAlias
}
