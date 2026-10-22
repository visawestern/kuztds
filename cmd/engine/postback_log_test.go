package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestRejectedPostbackLogHasNoSecret(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	h := testEnv(t, group(), func(d *engineDeps) {
		d.log = log
		d.postbackKey = "secret"
	})
	do(t, h, "/?pb=secret&cid=c1&status=not/a/status&password=hunter2", "8.8.8.8", nil)
	got := buf.String()
	if strings.Contains(got, "secret") || strings.Contains(got, "hunter2") {
		t.Fatalf("secret leaked to logs: %s", got)
	}
	if !strings.Contains(got, "not/a/status") && !strings.Contains(got, `"cid":"c1"`) {
		t.Fatalf("diagnostic fields missing: %s", got)
	}
}
