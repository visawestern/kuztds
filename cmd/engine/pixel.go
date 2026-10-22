package main

import (
	"net/http"
	"strconv"

	"github.com/egerkuzma/kuztds/internal/config"
)

// Reserved pixel paths. Group ids/slugs must not use them so a pixel request
// can never be mistaken for a campaign.
const (
	conversionPixelPath = config.ConversionPixelPath
	conversionImagePath = config.ConversionImagePath
)

const conversionPixelJS = "/* kuzTDS conversion pixel */\n"

// A 1x1 transparent GIF, the smallest useful image pixel.
var conversionPixelGIF = []byte{
	0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00, 0x01, 0x00, 0x80, 0x00,
	0x00, 0x00, 0x00, 0x00, 0xff, 0xff, 0xff, 0x21, 0xf9, 0x04, 0x01, 0x00,
	0x00, 0x00, 0x00, 0x2c, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00,
	0x00, 0x02, 0x02, 0x44, 0x01, 0x00, 0x3b,
}

// conversionPixel serves the thank-you-page JS pixel (G-309). The script is
// fetched by the browser; the conversion is recorded server-side through the
// same pipeline as ?pb= postbacks, so dedupe, pricing and the offer cap match.
func (d *engineDeps) conversionPixel(w http.ResponseWriter, r *http.Request) {
	d.serveConversionPixel(w, r, "application/javascript; charset=utf-8", []byte(conversionPixelJS), "pixel")
}

// conversionImage serves the same conversion as a 1x1 GIF (G-068), for a
// thank-you page that uses <img> instead of <script>.
func (d *engineDeps) conversionImage(w http.ResponseWriter, r *http.Request) {
	d.serveConversionPixel(w, r, "image/gif", conversionPixelGIF, "image pixel")
}

func (d *engineDeps) serveConversionPixel(w http.ResponseWriter, r *http.Request, contentType string, body []byte, kind string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	// A HEAD is a probe, not a conversion: headers are enough.
	if r.Method == http.MethodHead {
		return
	}

	q := r.URL.Query()
	if d.postbackKey == "" || q.Get("pb") != d.postbackKey {
		d.log.Debug(kind + " ignored: bad key")
		_, _ = w.Write(body)
		return
	}
	pbd, ok := parsePostback(q)
	if !ok {
		reason := postbackRejectReason(q)
		d.log.Warn(kind+" ignored", "reason", reason,
			"cid", clip(q.Get("cid"), 256), "status", clip(q.Get("status"), 256))
		d.journalPostback(r, reason, "")
	} else if _, err := d.recordConversion(r.Context(), pbd); err != nil {
		d.log.Warn(kind+" failed", "cid", pbd.CID, "status", pbd.Status, "err", err)
		d.journalPostback(r, "not_recorded", err.Error())
	}
	_, _ = w.Write(body)
}
