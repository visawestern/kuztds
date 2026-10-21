package main

import (
	"strings"
	"testing"

	"github.com/egerkuzma/kuztds/internal/config"
)

// G-055: the active traffic source template turns query parameters into macros.
func TestSourceLabelsExpand(t *testing.T) {
	g := config.NewGroups(&config.Group{
		ID: "g", Status: true, Redirect: "http_redirect",
		Out: "https://net.example/?g=[GCLID]&s=[SRC]&k=[KEY]",
		Sources: []config.TrafficSource{{
			ID: "google", Name: "Google Ads",
			Labels: []config.SourceLabel{
				{Param: "gclid", Label: "gclid"},
				{Param: "utm_source", Label: "SRC"},
			},
		}},
		Source: "google",
	})
	h, _, stop := curlEnv(t, g, "0")
	rec := do(t, h, "/g?gclid=abc123&utm_source=google&q=shoes", "8.8.8.8", map[string]string{"User-Agent": uaWinChrome})
	if got := rec.Header().Get("Location"); got != "https://net.example/?g=abc123&s=google&k=shoes" {
		t.Errorf("Location = %q, want the source macros filled from the query", got)
	}

	rec = do(t, h, "/g", "8.8.8.8", map[string]string{"User-Agent": uaWinChrome})
	stop()
	if got := rec.Header().Get("Location"); got != "https://net.example/?g=&s=&k=" {
		t.Errorf("without the parameters Location = %q, want empty macros", got)
	}
	if strings.Contains(rec.Header().Get("Location"), "[") {
		t.Error("a macro survived expansion")
	}
}
