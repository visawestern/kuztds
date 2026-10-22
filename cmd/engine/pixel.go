package main

import (
	"io"
	"net/http"
	"strconv"

	"github.com/egerkuzma/kuztds/internal/config"
)

// conversionPixelPath is a reserved engine path. Group ids/slugs must not use
// it so a pixel request can never be mistaken for a campaign.
const conversionPixelPath = config.ConversionPixelPath

const conversionPixelJS = "/* kuzTDS conversion pixel */\n"

// conversionPixel serves the thank-you-page JS pixel (G-309). The script is
// fetched by the browser; the conversion is recorded server-side through the
// same pipeline as ?pb= postbacks, so dedupe, pricing and the offer cap match.
func (d *engineDeps) conversionPixel(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(conversionPixelJS)))
	// A HEAD is a probe, not a conversion: headers are enough.
	if r.Method == http.MethodHead {
		return
	}

	q := r.URL.Query()
	if d.postbackKey == "" || q.Get("pb") != d.postbackKey {
		d.log.Debug("pixel ignored: bad key")
		_, _ = io.WriteString(w, conversionPixelJS)
		return
	}
	pbd, ok := parsePostback(q)
	if !ok {
		reason := postbackRejectReason(q)
		d.log.Warn("pixel ignored", "reason", reason,
			"cid", clip(q.Get("cid"), 256), "status", clip(q.Get("status"), 256))
		d.journalPostback(r, reason, "")
	} else if _, err := d.recordConversion(r.Context(), pbd); err != nil {
		d.log.Warn("pixel failed", "cid", pbd.CID, "status", pbd.Status, "err", err)
		d.journalPostback(r, "not_recorded", err.Error())
	}
	_, _ = io.WriteString(w, conversionPixelJS)
}
