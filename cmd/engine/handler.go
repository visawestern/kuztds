package main

import (
	"fmt"
	"html"
	"log/slog"
	"math/rand"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/egerkuzma/kuztds/internal/config"
	"github.com/egerkuzma/kuztds/internal/cost"
	"github.com/egerkuzma/kuztds/internal/detect"
	"github.com/egerkuzma/kuztds/internal/fetch"
	"github.com/egerkuzma/kuztds/internal/geo"
	"github.com/egerkuzma/kuztds/internal/ipindex"
	"github.com/egerkuzma/kuztds/internal/logbuf"
	"github.com/egerkuzma/kuztds/internal/render"
	"github.com/egerkuzma/kuztds/internal/router"
	"github.com/egerkuzma/kuztds/internal/seplist"
	"github.com/egerkuzma/kuztds/internal/server"
	"github.com/egerkuzma/kuztds/internal/store"
)

// engineDeps — hot-path dependencies assembled in main() and passed to the
// handler. Extracted from a closure so the pipeline can be exercised through
// httptest (see handler_test.go).
type engineDeps struct {
	log          *slog.Logger
	lists        *ipindex.Set
	sigs         *detect.Signatures
	seps         *seplist.Set
	geores       geo.Resolver
	counters     *store.Counters
	groups       *config.Groups
	logs         *logbuf.Buffer
	chConn       *store.CH
	rates        *ratesStore
	fetcher      *fetch.Client
	verifs       *verificationStore
	dataDir      string
	keysDir      string
	postbackKey  string
	apiKey       string
	trashMode    string
	trashURL     string
	curlCacheMin int
}

// ipListFiles collects the names of .dat lists referenced by stream ip_list
// filters (without the extension) so the engine loads them into ipindex.Set
// alongside the standard lists. Otherwise the per-stream IP filter would
// silently fail to fire.
func ipListFiles(groups *config.Groups) []string {
	if groups == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, g := range groups.List() {
		for _, s := range g.Streams {
			f := strings.TrimSuffix(s.Rules.IPList.File, ".dat")
			if f == "" || seen[f] {
				continue
			}
			seen[f] = true
			out = append(out, f)
		}
	}
	return out
}

// separationFiles collects the .dat names referenced by stream separation
// rules, so seplist.Set can hold them in memory. Same reason as ipListFiles: a
// file the engine never read matches nothing, and separation failing to fire is
// invisible — the stream just serves its default output.
func separationFiles(groups *config.Groups) []string {
	if groups == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, g := range groups.List() {
		for _, s := range g.Streams {
			if !s.Separation.Enabled {
				continue
			}
			f := strings.TrimSuffix(s.Separation.File, ".dat")
			if f == "" || seen[f] {
				continue
			}
			seen[f] = true
			out = append(out, f)
		}
	}
	return out
}

// urlOutput reports whether the output of this redirect type is a URL the
// stream may edit (G-130). Text, raw JS, offers and group hops are not.
func urlOutput(redirect string) bool {
	switch redirect {
	case "http_redirect", "meta_refresh", "js_redirect", "iframe_redirect",
		"js_selection", "iframe_selection", "curl":
		return true
	}
	return false
}

// hitPercent reports whether a p-percent chance fires: p <= 0 never, p >= 100
// always, and exactly p times in 100 in between.
//
// Written out because "p > rand.Intn(100)+1" reads right and is not: rand+1 is
// uniform over [1,100], so that form fires (p-1) times in 100 — prob 100 gives
// 99% and prob 1 never fires at all.
func hitPercent(p int) bool {
	if p <= 0 {
		return false
	}
	if p >= 100 {
		return true
	}
	return rand.Intn(100)+1 <= p
}

// root — hot-path handler. Full request pipeline:
// postback → api mode → blacklist → group/trash → antiflood → geo/detect →
// uniqueness → router → bots → separation/remote/chance → distribution →
// render → async log → key collection.
func (d *engineDeps) root(w http.ResponseWriter, r *http.Request) {
	// Postback pixel: ?pb=KEY&cid=<id>&profit=X&status=<status>&tid=<txn>.
	// Без валидного status= конверсия игнорируется (паритет Keitaro 11),
	// ответ всё равно 200.
	if pb := r.URL.Query().Get("pb"); pb != "" {
		if d.postbackKey != "" && pb == d.postbackKey && d.chConn != nil {
			if pbd, ok := parsePostback(r.URL.Query()); ok {
				// Conversion cost by the entry campaign's model (G-120) and the
				// offer payout when the postback carries no amount (G-049).
				// Best effort: pricing must never block the conversion itself.
				profit := pbd.Profit
				convCost := 0.0
				offer := ""
				if gid, gerr := d.chConn.EventGroupID(r.Context(), pbd.CID); gerr == nil && d.groups != nil {
					if grp, found := d.groups.Get(gid); found {
						if offID, oerr := d.chConn.EventOfferID(r.Context(), pbd.CID); oerr == nil && offID != "" {
							offer = offID
							if p, priced := effectiveProfit(pbd.Profit, pbd.HasProfit, *grp, offID, d.rates.Rates()); priced {
								profit = p
							} else {
								d.log.Debug("engine: offer payout skipped (no rates for currency)", "offer", offID)
							}
							// G-050: a purchase spends one slot of the offer's day.
							// trash is a reversal, not a purchase.
							if pbd.Status != "trash" && d.counters != nil {
								if cerr := d.counters.CountConversion(r.Context(), gid, offID, time.Now()); cerr != nil {
									d.log.Debug("engine: offer cap not counted", "offer", offID, "err", cerr)
								}
							}
						}
						m := cost.Model{Kind: grp.Cost.Model, Value: grp.Cost.Value, Currency: grp.Cost.Currency, PayoutPct: grp.Cost.PayoutPct}
						if cc, ok := cost.ForConversionStatusUSD(m, pbd.Status, profit, d.rates.Rates()); ok {
							convCost = cc
						} else {
							d.log.Debug("engine: conversion cost skipped (no rates for currency)", "currency", grp.Cost.Currency)
						}
					}
				}
				if err := d.chConn.RecordPostback(r.Context(), pbd.CID, profit, pbd.Status, pbd.TID, offer, convCost); err != nil {
					d.log.Warn("postback failed", "cid", pbd.CID, "status", pbd.Status, "err", err)
				}
			} else {
				d.log.Warn("postback ignored", "query", r.URL.RawQuery)
			}
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	// API-client mode: ?api=base64(JSON). Input is taken from the client request.
	var apiReq *apiRequest
	apiMode := false
	if a := r.URL.Query().Get("api"); a != "" {
		req, ok := parseAPIRequest(a, d.apiKey)
		if !ok {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		apiReq, apiMode = req, true
	}

	var ip netip.Addr
	if apiMode {
		ip, _ = netip.ParseAddr(strings.TrimSpace(apiReq.IP))
	} else {
		ip = server.ClientIP(r.Context())
	}
	if !ip.IsValid() {
		w.WriteHeader(http.StatusOK)
		return
	}

	// 1) Blacklist — instant rejection.
	if _, blocked := d.lists.Lookup("ip_blacklist", ip); blocked {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	rctx := r.Context()

	// Group: by ID/alias — from api.id, or from the FIRST segment of the request
	// path. Everything after it is ignored, so "/promo", "/promo/" and
	// "/promo/iphone-15-sale.html" all resolve to the group "promo".
	//
	// Matching the whole path instead would send every deep link to trash mode —
	// an empty 200 by default, indistinguishable from nothing happening. TDS
	// links are routinely dressed up as real pages, so the extra segments are
	// decoration and must not change routing.
	gid, _, _ := strings.Cut(strings.Trim(r.URL.Path, "/"), "/")
	if apiMode {
		gid = apiReq.ID
	}
	var grp *config.Group
	if d.groups != nil {
		grp, _ = d.groups.Get(gid)
	} else {
		grp = demoGroup
	}
	path := strings.Trim(r.URL.Path, "/")
	oneSegment := !strings.Contains(path, "/")
	if grp == nil && !apiMode && oneSegment && d.verifs != nil &&
		(r.Method == http.MethodGet || r.Method == http.MethodHead) {
		if vf, ok := d.verifs.Get(path); ok {
			w.Header().Set("Content-Type", vf.ct)
			w.Header().Set("Cache-Control", "public, max-age=300")
			if r.Method == http.MethodHead {
				return
			}
			_, _ = w.Write(vf.body)
			return
		}
	}
	if grp == nil && !apiMode && d.groups != nil {
		if oneSegment && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			if sg, ok := d.groups.GetShare(gid); ok {
				if !sg.Status {
					trashResult(d.trashMode, d.trashURL).Write(w)
					return
				}
				serveShare(w, r, sg)
				return
			}
		}
	}
	if grp == nil || !grp.Status {
		trashResult(d.trashMode, d.trashURL).Write(w)
		return
	}

	// 1.5) Antiflood (phase 4): no more than N requests from an IP per window.
	if d.counters != nil && grp.Firewall.Enabled {
		fw := grp.Firewall
		if ok, _ := d.counters.Firewall(rctx, grp.ID, ip, fw.Queries, secs(fw.Seconds)); !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	ua := r.Header.Get("User-Agent")
	ref := r.Header.Get("Referer")
	if apiMode {
		ua, ref = apiReq.UserAgent, apiReq.Referer
	}

	// 2) Device/OS/browser/brand and geo (phases 2, 7).
	info := detect.Parse(ua)
	device := info.Device
	g := d.geores.Resolve(ip)
	// Country has two sources: the geo database and the CDN's header. The
	// group says which one wins when both answer; the other is the fallback.
	//
	// The header is believed only when the request came through a trusted
	// proxy — exactly the rule X-Forwarded-For follows. From anyone else it is
	// text the visitor typed, and a visitor who can name their own country
	// picks their own stream. api.cf_country is the api client's statement
	// and is covered by the api key. Either way a country is two characters;
	// anything else is dropped rather than passed into the log, the response
	// header and — through [RANDLINE-([COUNTRY].dat)-1] — a file name.
	country := g.Country
	hdr := geo.Empty
	if apiMode {
		hdr = isoCode(apiReq.CFCountry)
	} else if server.ViaTrustedProxy(r.Context()) {
		hdr = isoCode(r.Header.Get("CF-IPCountry"))
	}
	if hdr != geo.Empty && (apiMode || country == geo.Empty || grp.Geo == "cf") {
		country = hdr
	}
	tzOffset := g.UTCOffset(time.Now())

	// 3.5) Uniqueness: the api client sends its own; otherwise cookie/Redis.
	unique := true
	if apiMode {
		unique = apiReq.Uniq == "yes"
	} else if grp.UniqMethod == "cookie" {
		unique = cookieUnique(w, r, grp)
	} else if d.counters != nil {
		if u, _ := d.counters.Unique(rctx, grp.ID, ip, secs(grp.UniqSeconds)); !u {
			unique = false
		}
	}

	// Input (key/lang/domain/extra params): from the api request or from HTTP.
	key := r.URL.Query().Get("q")
	lang := langOf(r)
	domain := domainOf(ref)
	pars := extraParams(r)
	if apiMode {
		key, lang, domain, pars = apiReq.Key, apiReq.Lang, apiReq.Domain, apiReq.Pars
	}

	// 4) Stream selection (phase 3). Operator — by the label from the wap list.
	operator := router.Empty
	if label, ok := d.lists.Lookup("wap", ip); ok && label != "" {
		operator = label
	}
	v := router.Visitor{
		Lang:       lang,
		Country:    country,
		City:       g.City,
		Region:     g.Region,
		UA:         ua,
		Referer:    refOrEmpty(ref),
		Domain:     domain,
		Key:        key,
		Device:     device,
		Operator:   operator,
		OS:         info.OS,
		OSVersion:  info.OSVersion,
		Browser:    info.Browser,
		BrowserVer: info.BrowserVer,
		Brand:      info.Brand,
		Unique:     unique,
		IP:         ip,
		ASN:        g.ASN,
		Org:        g.Org,
		Timezone:   g.Timezone,
		TZOffset:   tzOffset,
		Query:      r.URL.Query(),
	}
	// Output defaults — from the group; the stream overrides them.
	//
	// A stream (or a group's default) of type "group" hands the visitor to
	// another group: selection runs again over that group's streams with the
	// same visitor. Everything decided before this point — antiflood,
	// uniqueness, country source — belongs to the group the visitor entered
	// through; what follows, including the log line, belongs to the group
	// that finally served them.
	var (
		streamName, redirect, outRaw, ctype string
		selStream                           *config.Stream
		via                                 string     // the first link followed: "<group id>/<stream>"
		entryCost                           = grp.Cost // money belongs to the entry campaign (G-119)
	)
	for hop := 0; ; hop++ {
		deps := router.Deps{IP: d.lists}
		if d.counters != nil {
			deps.Limiter = limiter{c: d.counters, ctx: rctx, id: grp.ID}
		}
		streamName, redirect, outRaw, ctype, selStream = "-", grp.Redirect, grp.Out, grp.Header, nil
		if s, ok := router.Select(grp, v, deps); ok {
			selStream = s
			streamName = s.Name
			if s.Out.Redirect != "" {
				redirect = s.Out.Redirect
				outRaw = s.Out.Out
			}
			if s.Out.Header != "" {
				ctype = s.Out.Header
			}
			// The limit counter is consumed inside the router (limiter.Allowed):
			// selecting the stream and taking its limit are one event, so there is
			// nothing to record here.
		}
		if redirect != "group" {
			break
		}
		var next *config.Group
		if d.groups != nil {
			next, _ = d.groups.Get(strings.TrimSpace(outRaw))
		}
		// A link to nowhere, to a disabled group, or a chain that does not end
		// is a configuration error; the visitor gets what an unknown group gets.
		if next == nil || !next.Status || next == grp || hop >= config.MaxGroupHops {
			d.log.Warn("engine: group link not followed", "group", grp.ID, "stream", streamName, "target", outRaw, "hop", hop)
			trashResult(d.trashMode, d.trashURL).Write(w)
			return
		}
		if via == "" {
			via = grp.ID + "/" + streamName
		}
		grp = next
	}

	// 4.5) Bot detection by the selected stream's toggles (after selection).
	botsCfg := config.Bots{CheckUA: true} // no stream — signatures only (for the log)
	if selStream != nil {
		botsCfg = selStream.Bots
	}
	bot := detectBot(d.lists, d.sigs, d.dataDir, ip, ua, refOrEmpty(ref), v.Lang, botsCfg)
	// Separate serving for bots (bot_redirect). "skip"/"" → normal stream (the bot is still logged).
	botServed := false
	if bot != detect.BotNone && selStream != nil && botsCfg.Redirect != "" && botsCfg.Redirect != "skip" {
		redirect = botsCfg.Redirect
		outRaw = botsCfg.Out
		if botsCfg.Header != "" {
			ctype = botsCfg.Header
		}
		botServed = true
	}

	// separation: substitute the output by keyword (unless this is bot serving).
	if !botServed && selStream != nil && selStream.Separation.Enabled &&
		v.Key != "" && selStream.Separation.File != "" {
		if o := d.seps.Lookup(selStream.Separation.File, v.Key); o != "" {
			outRaw = o
		}
	}

	// remote: load [REMOTE] from an external URL (cache, regex, fallback).
	// The value itself is spliced in after render.Expand, below: it is the
	// partner's response, not our template, and Expand's "substituted values
	// are never rescanned" rule only covers what Expand itself substitutes.
	remoteVal, wantRemote := "", false
	if !botServed && selStream != nil && selStream.Remote.Enabled && strings.Contains(outRaw, "[REMOTE]") {
		remoteVal, wantRemote = remoteValue(d.fetcher, rctx, selStream.Remote, ip.String(), country, g.City, v.Lang, v.Key), true
		// "|||" is the distribution separator, applied to the template by
		// pickOut. A remote value carrying it would otherwise let the partner
		// choose which variant we serve; treat it as a bad response instead.
		if strings.Contains(remoteVal, "|||") {
			remoteVal = selStream.Remote.Reserved
		}
	}

	// distribution of out variants (|||): random/rotator/evenly.
	dist := ""
	if selStream != nil {
		dist = selStream.Out.Distribution
	}
	// G-130: remember whether the flow serves a URL before offers rewrite the
	// redirect type; replacements must not touch an offer's own URL.
	urlEditable := urlOutput(redirect)

	// offer: a stream (or a group default) of type "offer" sends the visitor to
	// a campaign offer (G-048). The URL then goes through the normal macro and
	// distribution path as an ordinary redirect. A missing or disabled offer is
	// a configuration error and gets the trash answer, like a broken group link.
	//
	// G-051: the output may name several offers separated by |||. The stream's
	// distribution then picks one per visit, and ids the flow cannot serve are
	// skipped, so one stale entry does not take the whole stream down.
	offerID := ""
	if redirect == "offer" {
		ids := strings.Split(outRaw, "|||")
		servable := make([]string, 0, len(ids))
		for _, id := range ids {
			if o, ok := grp.Offer(id); ok && o.Status {
				servable = append(servable, o.ID)
			}
		}
		if len(servable) == 0 {
			d.log.Warn("engine: offer not served", "group", grp.ID, "stream", streamName, "offer", strings.TrimSpace(outRaw))
			trashResult(d.trashMode, d.trashURL).Write(w)
			return
		}
		// A separate key keeps the offer rotation state from mixing with the
		// distribution of the offer's own URL variants.
		picked := pickOut(w, r, d.counters, rctx, grp.ID, streamName+"__o", strings.Join(servable, "|||"), dist)
		off, _ := grp.Offer(picked)
		// G-054: the network may buy only some countries. Try its fallback; when
		// that does not accept the country either, send the visit nowhere.
		if !off.AcceptsCountry(country) {
			if fb, fok := grp.Offer(off.Fallback); fok && fb.Status && fb.ID != off.ID && fb.AcceptsCountry(country) {
				d.log.Debug("engine: offer does not buy the country, fallback", "offer", off.ID, "country", country, "fallback", fb.ID)
				off = fb
			} else {
				d.log.Warn("engine: offer does not buy the country", "group", grp.ID, "offer", off.ID, "country", country)
				trashResult(d.trashMode, d.trashURL).Write(w)
				return
			}
		}
		// G-050: a daily purchase cap moves the visit to the fallback offer.
		// Without one the visit gets the trash answer; Redis being down must
		// not stop the traffic.
		if off.CapDaily > 0 && d.counters != nil {
			reached, cerr := d.counters.OfferCapReached(r.Context(), grp.ID, off.ID, off.CapDaily, time.Now())
			switch {
			case cerr != nil:
				d.log.Warn("engine: offer cap unavailable, serving anyway", "offer", off.ID, "err", cerr)
			case reached:
				if fb, fok := grp.Offer(off.Fallback); fok && fb.Status && fb.ID != off.ID && fb.AcceptsCountry(country) {
					d.log.Debug("engine: offer cap reached, fallback", "offer", off.ID, "fallback", fb.ID)
					off = fb
				} else {
					d.log.Warn("engine: offer cap reached, no fallback", "group", grp.ID, "offer", off.ID)
					trashResult(d.trashMode, d.trashURL).Write(w)
					return
				}
			}
		}
		offerID = off.ID
		outRaw = off.URL
		redirect = "http_redirect"
	}

	// chance: for javascript/js_selection — show with probability chance %.
	// Here chance 0 means "not configured", so only a real 1..99 gates the
	// output; the draw itself goes through hitPercent, which carries the
	// off-by-one this comparison would otherwise have to get right by hand.
	if !botServed && selStream != nil && (redirect == "javascript" || redirect == "js_selection") {
		if ch := selStream.Out.Chance; ch > 0 && ch < 100 && !hitPercent(ch) {
			redirect = "stop"
			outRaw = ""
		}
	}

	outRaw = pickOut(w, r, d.counters, rctx, grp.ID, streamName, outRaw, dist)

	// G-055: the active traffic source template turns query parameters into
	// macros. Only the chosen template is read, and only when it has labels.
	var extra map[string]string
	if src, ok := grp.SourceTemplate(grp.Source); ok && len(src.Labels) > 0 {
		q := r.URL.Query()
		extra = make(map[string]string, len(src.Labels))
		for _, l := range src.Labels {
			extra[strings.ToUpper(strings.TrimSpace(l.Label))] = q.Get(strings.TrimSpace(l.Param))
		}
	}

	// 5) Render the output: macros, api_mac, CURL fetch, or a normal redirect.
	cid := newCID()
	md := render.MacroDeps{
		Key: v.Key, Path: r.Host, IP: ip.String(), Country: country, City: g.City,
		Region: g.Region, Lang: v.Lang, Device: device, Operator: operator,
		Domain: v.Domain, UserAgent: ua, CID: cid, Pars: pars, Extra: extra,
		ASN: asnText(g.ASN), Org: dashEmpty(g.Org), Timezone: g.Timezone, TZOffset: tzOffset,
		DataDir: d.dataDir}
	out := render.Expand(outRaw, md)
	if wantRemote {
		out = strings.ReplaceAll(out, "[REMOTE]", remoteVal)
	}
	if urlEditable && selStream != nil {
		for _, e := range selStream.URLReplace {
			out = replaceCI(out, e.Find, e.Replace)
		}
	}

	// api_mac: mac code in the api response with probability Prob %.
	mac := ""
	if redirect == "api" && selStream != nil && selStream.APIMac.Enabled &&
		selStream.APIMac.Code != "" && hitPercent(selStream.APIMac.Prob) {
		mac = render.Expand(selStream.APIMac.Code, md)
	}

	// logRedirect is what the event carries. It tracks redirect except when a
	// curl fetch fails: the visitor is then served something other than the
	// partner's page, and the report must not call that an ordinary serve.
	logRedirect := redirect

	var res render.Result
	if redirect == "curl" {
		rules := ""
		if selStream != nil {
			if rules = selStream.Curl; botServed {
				rules = selStream.BotCurl
			}
		}
		ct := ctype
		if ct == "" {
			ct = "text/html; charset=utf-8"
		}
		body, ok := curlBody(d.fetcher, rctx, out, render.Expand(rules, md), d.curlCacheMin)
		if ok {
			res = render.Result{Status: http.StatusOK, ContentType: ct, Body: []byte(body)}
		} else {
			// No stream-level fallback exists for curl (unlike Remote.Reserved),
			// so reuse the configured trash behaviour — the same answer the
			// engine already gives when it cannot serve a group. Anything is
			// better than a blank 200, which burns the click and hides it.
			logRedirect = "curl_error"
			res = trashResult(d.trashMode, d.trashURL)
			d.log.Error("engine: curl fetch failed", "group", grp.ID, "stream", streamName, "url", out)
		}
	} else {
		res = render.Do(redirect, out, render.Options{ContentType: ctype, Path: r.Host,
			Country: country, City: g.City, Region: g.Region, Device: device,
			Operator: operator, Bot: bot, Uniq: b2yn(unique), Lang: v.Lang, Mac: mac})
	}

	// 6) Event log — asynchronous, the response does not wait for the write (phase 4).
	// G-126: a service flow is served but deliberately leaves no click.
	if d.logs != nil && (selStream == nil || !selStream.SkipClicks) {
		// Click cost by the entry campaign's model (G-119…G-121). The value is
		// normalized to USD, so cost_cur says USD; a currency without a rate is
		// honestly 0 + debug rather than a guess.
		clickCost, costOK := cost.ForClickPaidUSD(cost.Model{
			Kind: entryCost.Model, Value: entryCost.Value, Currency: entryCost.Currency,
			LossPct: entryCost.LossPct,
		}, d.rates.Rates())
		costCur := ""
		if costOK && clickCost != 0 {
			costCur = "USD"
		} else if !costOK {
			d.log.Debug("engine: cost skipped (no rates for currency)", "currency", entryCost.Currency)
		}
		d.logs.Push(logbuf.Event{
			Ts: time.Now(), GroupID: grp.ID, GroupName: grp.Name,
			Stream: streamName, Out: out, Redirect: logRedirect, Device: device,
			Operator: operator, Country: country, City: g.City, Region: g.Region,
			Lang: v.Lang, Uniq: b2u8(unique), Bot: bot, IP: ip.String(),
			Referer: v.Referer, UserAgent: ua, Domain: v.Domain, Keyword: v.Key,
			OS: info.OS, OSVersion: info.OSVersion, Browser: info.Browser,
			BrowserV: info.BrowserVer, Brand: info.Brand, CID: cid,
			ASN: g.ASN, Org: dashEmpty(g.Org), Timezone: dashEmpty(g.Timezone), Via: via,
			Cost: clickCost, CostCur: costCur, Offer: offerID,
		})
	}

	// Keyword collection.
	if bot == detect.BotNone {
		if grp.SaveKeys && v.Key != "" {
			appendKey(d.keysDir, grp.ID, v.Key, false)
		}
		if grp.SaveKeysSE {
			if kse := parseSEKeyword(ref); kse != "" {
				appendKey(d.keysDir, grp.ID, kse, true)
			}
		}
	}

	// Diagnostic headers (before writing the response) + render.
	h := w.Header()
	h.Set("X-Kuztds-Bot", emptyDash(bot))
	h.Set("X-Kuztds-Device", device)
	h.Set("X-Kuztds-Country", country)
	h.Set("X-Kuztds-Stream", streamName)
	h.Set("X-Kuztds-Uniq", b2yn(unique))
	res.Write(w)
}

// asnText renders an AS number for macros: "" when unknown, so a template
// like "as=[ASN]" degrades to an empty value instead of "as=0".
func asnText(n uint32) string {
	if n == 0 {
		return ""
	}
	return strconv.FormatUint(uint64(n), 10)
}

// dashEmpty turns the "-" placeholder into "": free-text values go into URLs
// and columns where a literal dash would read as data.
func dashEmpty(s string) string {
	if s == geo.Empty {
		return ""
	}
	return s
}

// serveShare renders the clean share link (G-059): an Open Graph card for the
// crawler that builds the preview and an immediate hand-off to the flow for the
// browser. This request is not a visit, so it writes no click — the follow-up
// to /<group id> goes through the usual pipeline and carries the tracking.
func serveShare(w http.ResponseWriter, r *http.Request, g *config.Group) {
	title := g.Share.Title
	if title == "" {
		title = g.Name
	}
	if title == "" {
		title = g.ID
	}
	target := "/" + url.PathEscape(g.ID)
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	esc := html.EscapeString
	head := `<!doctype html>
<html><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + esc(title) + `</title>
<meta property="og:type" content="website">
<meta property="og:title" content="` + esc(title) + `">
`
	if g.Share.Description != "" {
		head += `<meta property="og:description" content="` + esc(g.Share.Description) + `">
`
	}
	if g.Share.Image != "" {
		head += `<meta property="og:image" content="` + esc(g.Share.Image) + `">
<meta name="twitter:card" content="summary_large_image">
`
	}
	head += `<meta http-equiv="refresh" content="0;url=` + esc(target) + `">
</head><body><p><a href="` + esc(target) + `">` + esc(target) + `</a></p></body></html>
`
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex")
	if r.Method == http.MethodHead {
		return
	}
	fmt.Fprint(w, head)
}
