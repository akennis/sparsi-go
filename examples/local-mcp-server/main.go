// Package main demonstrates two MCPScriptOp variants composed via dagor's
// MapOver fan-out: one DAG step searches Google for a query and extracts the
// first 3 result URLs from a single browser session, and a downstream
// MapOver vertex spawns one playwright-mcp subprocess per URL in parallel,
// each opening the page and saving a screenshot.
//
// Per-URL screenshots run best-effort: a navigation or screenshot failure
// for one URL does not abort the DAG. The failure is captured on the
// per-URL ShotResult and surfaced to the caller alongside the successes.
//
// DAG:
//
//	search_input ─► find_results ─► shoot_each (MapOver) ─► screenshot_results
//	  (context-     (MCPScriptOp,    (per-URL MCPScriptOp,
//	   val const)    one session,     own subprocess each,
//	                 emits []string)  N parallel; emits ShotResult)
//
// Prerequisites:
//   - npx on PATH (Node.js).
//   - First run downloads @playwright/mcp@latest plus a browser binary;
//     subsequent runs reuse the cache.
//   - No CLAUDE_API_KEY required.
package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/akennis/sparsi-go/library"

	"github.com/panjf2000/ants/v2"
	"github.com/wwz16/dagor"
	"github.com/wwz16/dagor/config"
	"github.com/wwz16/dagor/graph"
	"github.com/wwz16/dagor/operator"
	builtin "github.com/wwz16/dagor/operator/builtin"
	"github.com/wwz16/dagor/reporter"
)

// ─── Search step: typed input/output ───────────────────────────────────────

type SearchInput struct {
	Query string `json:"query"`
}

const googleSearchBoxTarget = `textarea[name="q"]`

// dismissConsentJS best-effort clicks Google's "Accept all" / "I agree"
// consent button when the EU/UK consent.google.com interstitial fires (or an
// in-page modal is present). Returns the matched label, or "" if none.
const dismissConsentJS = `() => {
  const labels = ['accept all','i agree','agree','aceptar todo','akzeptieren','tout accepter','accetta tutto','aceitar tudo','accept'];
  const norm = (s) => (s || '').trim().toLowerCase();
  const match = (s) => { const t = norm(s); return labels.some(l => t === l || t.startsWith(l + ' ')); };
  const candidates = Array.from(document.querySelectorAll('button, [role="button"], input[type="submit"]'));
  for (const b of candidates) {
    const txt = b.innerText || b.textContent || b.value || '';
    const aria = (b.getAttribute && b.getAttribute('aria-label')) || '';
    if (match(txt))  { b.click(); return norm(txt).slice(0, 40); }
    if (match(aria)) { b.click(); return norm(aria).slice(0, 40); }
  }
  return '';
}`

// waitForSearchBoxJS polls up to ~10s for Google's search box. The consent
// click above can trigger a redirect back to google.com, so we don't assume
// the box is already mounted.
const waitForSearchBoxJS = `async () => {
  for (let i = 0; i < 50; i++) {
    if (document.querySelector('textarea[name="q"], input[name="q"]')) return true;
    await new Promise(r => setTimeout(r, 200));
  }
  return false;
}`

// waitForResultsJS polls up to ~12s for at least one anchor inside Google's
// results container. A much stronger signal than matching the query string
// against page text — the query is already in the search box well before
// results actually render.
const waitForResultsJS = `async () => {
  for (let i = 0; i < 60; i++) {
    if (document.querySelector('#search a[href], #rso a[href]')) return true;
    await new Promise(r => setTimeout(r, 200));
  }
  return false;
}`

// extractURLsJS picks the first 3 distinct off-Google http(s) hrefs from the
// search results container, deduped by hostname so we don't return three
// links to the same site.
//
// Scoped to #search/#rso so header/footer/nav links can't slip in. Unwraps
// Google's /url?q= redirect wrapper. Excludes any *.google.tld,
// googleusercontent, and gstatic host.
const extractURLsJS = `() => {
  const seen = new Set();
  const out = [];
  const root = document.querySelector('#search') || document.querySelector('#rso') || document.body;
  const isGoogleHost = (h) => h === 'google.com' ||
    h.endsWith('.google.com') ||
    h.endsWith('.googleusercontent.com') ||
    h.endsWith('.gstatic.com') ||
    /\.google\.[a-z.]+$/.test(h);
  for (const a of root.querySelectorAll('a[href]')) {
    let h = a.href;
    if (!h) continue;
    if (h.startsWith('https://www.google.com/url?') || h.startsWith('http://www.google.com/url?')) {
      try {
        const u = new URL(h);
        const q = u.searchParams.get('q') || u.searchParams.get('url');
        if (q) h = q;
      } catch (e) {}
    }
    if (!h.startsWith('http://') && !h.startsWith('https://')) continue;
    let host;
    try { host = new URL(h).hostname.toLowerCase(); } catch (e) { continue; }
    if (!host || isGoogleHost(host)) continue;
    if (seen.has(host)) continue;
    if (a.closest('header, footer, nav')) continue;
    seen.add(host);
    out.push(h);
    if (out.length >= 3) break;
  }
  return out;
}`

// MCPGoogleSearchURLsOp performs Google → search → extract first 3 URLs in
// one playwright-mcp session.
type MCPGoogleSearchURLsOp struct {
	library.MCPScriptOp[SearchInput, []string]
}

func newMCPGoogleSearchURLsOp() operator.IOperator {
	op := &MCPGoogleSearchURLsOp{}
	op.Script = func(ctx context.Context, sess library.MCPSession, in *SearchInput, out *[]string) error {
		if in == nil || in.Query == "" {
			return fmt.Errorf("googleSearchURLs: empty query")
		}
		if _, _, err := sess.CallTool(ctx, "browser_navigate", map[string]any{
			"url": "https://www.google.com/?hl=en",
		}); err != nil {
			return fmt.Errorf("browser_navigate: %w", err)
		}

		// Best-effort consent dismissal. Failures are non-fatal — the
		// follow-up wait-for-search-box will surface a real problem.
		if _, structured, err := sess.CallTool(ctx, "browser_evaluate", map[string]any{
			"function": dismissConsentJS,
		}); err == nil {
			if label := normalizeJSStringResult(structured); label != "" {
				slog.InfoContext(ctx, "dismissed Google consent dialog", "label", label)
			}
		}

		// Wait for the search box before typing — the consent click can
		// trigger a redirect that takes a moment to settle.
		if _, _, err := sess.CallTool(ctx, "browser_evaluate", map[string]any{
			"function": waitForSearchBoxJS,
		}); err != nil {
			return fmt.Errorf("wait for search box: %w", err)
		}

		if _, _, err := sess.CallTool(ctx, "browser_type", map[string]any{
			"target": googleSearchBoxTarget,
			"text":   in.Query,
			"submit": true,
		}); err != nil {
			return fmt.Errorf("browser_type: %w", err)
		}

		// Wait for the results container, not for query echo on the page.
		if _, _, err := sess.CallTool(ctx, "browser_evaluate", map[string]any{
			"function": waitForResultsJS,
		}); err != nil {
			slog.WarnContext(ctx, "wait for results soft-failed", "err", err)
		}

		text, structured, err := sess.CallTool(ctx, "browser_evaluate", map[string]any{
			"function": extractURLsJS,
		})
		if err != nil {
			return fmt.Errorf("browser_evaluate: %w", err)
		}

		urls, perr := parseURLList(structured, text)
		if perr != nil {
			return fmt.Errorf("parse URL list (text=%q): %w", truncate(text, 200), perr)
		}
		if len(urls) == 0 {
			return fmt.Errorf("browser_evaluate returned no URLs (text=%q)", truncate(text, 200))
		}
		*out = urls
		return nil
	}
	return op
}

// normalizeJSStringResult pulls a string out of a browser_evaluate structured
// payload that may be a bare JSON string ("foo"), a {"result":"foo"} wrapper,
// or empty/false/null. Returns "" for falsy/missing values.
func normalizeJSStringResult(structured json.RawMessage) string {
	if len(structured) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(structured, &s); err == nil {
		return strings.TrimSpace(s)
	}
	var wrap struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(structured, &wrap); err == nil {
		return strings.TrimSpace(wrap.Result)
	}
	return ""
}

// parseURLList accepts the structured-content JSON payload and/or the text
// fallback from browser_evaluate and returns the contained []string.
//
// playwright-mcp wraps the text payload with a "### Result" header before
// the JSON array, so we strip that framing if present and then scan for the
// first '[' or '{'. Both a bare array and the {result: [...]} shape are
// accepted.
func parseURLList(structured json.RawMessage, text string) ([]string, error) {
	if len(structured) > 0 {
		if arr, ok := decodeURLs(structured); ok {
			return arr, nil
		}
	}
	if i := strings.Index(text, "### Result"); i >= 0 {
		text = text[i+len("### Result"):]
	}
	if i := strings.IndexAny(text, "[{"); i >= 0 {
		if arr, ok := decodeURLs([]byte(text[i:])); ok {
			return arr, nil
		}
	}
	return nil, fmt.Errorf("could not decode URL list from structured or text payload")
}

func decodeURLs(b []byte) ([]string, bool) {
	var arr []string
	if err := json.NewDecoder(bytes.NewReader(b)).Decode(&arr); err == nil && len(arr) > 0 {
		return arr, true
	}
	var wrap struct {
		Result []string `json:"result"`
	}
	if err := json.NewDecoder(bytes.NewReader(b)).Decode(&wrap); err == nil && len(wrap.Result) > 0 {
		return wrap.Result, true
	}
	return nil, false
}

// ─── Per-URL screenshot step ───────────────────────────────────────────────

// ShotResult travels with each MapOver sub-vertex output so URL, screenshot
// path, and any per-URL error stay paired by structure rather than by
// positional zip in main(). Exactly one of Path or Error is meaningful.
type ShotResult struct {
	URL   string `json:"url"`
	Path  string `json:"path,omitempty"`
	Error string `json:"error,omitempty"`
}

// MCPScreenshotURLOp navigates a fresh playwright-mcp session to a single URL
// and saves a screenshot to <out_dir>/shot-<sha1(url)[:16]>.png. Used as the
// sub-vertex of the MapOver fan-out.
//
// Best-effort: navigation or screenshot failures are captured on
// ShotResult.Error and reported to the caller, but never abort the DAG.
//
// Vertex params (in addition to those consumed by MCPScriptOp.Setup):
//
//	out_dir — absolute directory inside which the screenshot is saved.
type MCPScreenshotURLOp struct {
	library.MCPScriptOp[string, ShotResult]
	outDir string
}

func (op *MCPScreenshotURLOp) Setup(p *config.Params) error {
	if err := op.MCPScriptOp.Setup(p); err != nil {
		return err
	}
	op.outDir = p.GetString("out_dir", "")
	if op.outDir == "" {
		return fmt.Errorf("MCPScreenshotURLOp: 'out_dir' param is required")
	}
	if !filepath.IsAbs(op.outDir) {
		return fmt.Errorf("MCPScreenshotURLOp: 'out_dir' must be absolute, got %q", op.outDir)
	}
	return nil
}

func newMCPScreenshotURLOp() operator.IOperator {
	op := &MCPScreenshotURLOp{}
	op.Script = func(ctx context.Context, sess library.MCPSession, in *string, out *ShotResult) error {
		if in == nil || *in == "" {
			out.Error = "empty url"
			return nil
		}
		out.URL = *in
		path := filepath.Join(op.outDir, screenshotName(*in))
		if _, _, err := sess.CallTool(ctx, "browser_navigate", map[string]any{
			"url": *in,
		}); err != nil {
			out.Error = fmt.Sprintf("browser_navigate: %v", err)
			return nil
		}
		if _, _, err := sess.CallTool(ctx, "browser_take_screenshot", map[string]any{
			"filename": path,
		}); err != nil {
			out.Error = fmt.Sprintf("browser_take_screenshot: %v", err)
			return nil
		}
		out.Path = path
		return nil
	}
	return op
}

func screenshotName(url string) string {
	h := sha1.Sum([]byte(url))
	return "shot-" + hex.EncodeToString(h[:])[:16] + ".png"
}

// ─── Context keys + registrations ──────────────────────────────────────────

type searchInputKey struct{}

func init() {
	if err := operator.RegisterOpFactory(
		"search_input_const",
		builtin.ContextValFactory[SearchInput](searchInputKey{}),
	); err != nil {
		log.Fatalf("register search_input_const: %v", err)
	}
	if err := operator.RegisterOpFactory(
		"MCPGoogleSearchURLsOp",
		newMCPGoogleSearchURLsOp,
	); err != nil {
		log.Fatalf("register MCPGoogleSearchURLsOp: %v", err)
	}
	if err := operator.RegisterOpFactory(
		"MCPScreenshotURLOp",
		newMCPScreenshotURLOp,
	); err != nil {
		log.Fatalf("register MCPScreenshotURLOp: %v", err)
	}
}

// ─── Graph ─────────────────────────────────────────────────────────────────

func buildGraph(outDir string) (*graph.Graph, error) {
	// playwright-mcp's internal action / navigation guards default to
	// 5000ms / 30000ms. Pages with heavy fonts or slow CDNs blow past the
	// 5 s action guard during browser_take_screenshot, so we widen both
	// here. These translate to playwright-mcp CLI flags.
	playwrightArgs := strings.Join([]string{
		"-y", "@playwright/mcp@latest",
		"--timeout-action", "30000",
		"--timeout-navigation", "60000",
	}, ",")
	playwrightParams := map[string]string{
		"command":         "npx",
		"args":            playwrightArgs,
		"init_timeout_ms": "120000",
		"call_timeout_ms": "90000",
		"max_retries":     "1",
	}
	shootParams := map[string]string{}
	for k, v := range playwrightParams {
		shootParams[k] = v
	}
	shootParams["out_dir"] = outDir
	shootParams["pool_size"] = "8"

	return graph.NewBuilder("mcp_google_search_screenshot").
		Vertex("search_input").Op("search_input_const").
		Output("Result", "search_input").
		Vertex("find_results").Op("MCPGoogleSearchURLsOp").
		Params(playwrightParams).
		Input("Input", "search_input").
		Output("Result", "result_urls").
		Vertex("shoot_each").
		Input("Items", "result_urls").
		MapOver("url").
		SubVertex("shoot").
		Op("MCPScreenshotURLOp").
		Params(shootParams).
		Input("Input", "url").
		Output("Result", "shot_result").
		CollectInto("shot_result", "screenshot_results").
		Build()
}

// ─── Driver ────────────────────────────────────────────────────────────────

type runResult struct {
	Query       string     `json:"query"`
	OutDir      string     `json:"out_dir"`
	Screenshots []shotInfo `json:"screenshots"`
	Errors      []shotErr  `json:"errors,omitempty"`
}

type shotInfo struct {
	URL   string `json:"url"`
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

type shotErr struct {
	URL   string `json:"url"`
	Error string `json:"error"`
}

func main() {
	query := flag.String("query", "Shizuoka", "Search query to enter into Google")
	outDir := flag.String("out-dir", "", "Absolute directory to write screenshots to (default: <cwd>/.playwright-mcp)")
	logLevel := flag.String("log-level", "info", "log level: debug, info, warn, error")
	flag.Parse()

	var lvl slog.LevelVar
	switch strings.ToLower(*logLevel) {
	case "debug":
		lvl.Set(slog.LevelDebug)
	case "warn":
		lvl.Set(slog.LevelWarn)
	case "error":
		lvl.Set(slog.LevelError)
	default:
		lvl.Set(slog.LevelInfo)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: &lvl})))

	if *outDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			log.Fatalf("os.Getwd: %v", err)
		}
		*outDir = filepath.Join(cwd, ".playwright-mcp")
	} else if !filepath.IsAbs(*outDir) {
		fmt.Fprintln(os.Stderr, "--out-dir must be absolute")
		os.Exit(2)
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		log.Fatalf("mkdir %s: %v", *outDir, err)
	}

	g, err := buildGraph(*outDir)
	if err != nil {
		log.Fatalf("build graph: %v", err)
	}

	pool, err := ants.NewPool(8)
	if err != nil {
		log.Fatalf("create pool: %v", err)
	}
	defer pool.Release()
	defer library.ShutdownMCPPool(context.Background())

	eng, err := dagor.NewEngine(g, pool, dagor.WithReporter(reporter.New(slog.Default())))
	if err != nil {
		log.Fatalf("create engine: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	ctx = context.WithValue(ctx, searchInputKey{}, SearchInput{Query: *query})

	if err := eng.Run(ctx); err != nil {
		log.Fatalf("run graph: %v", err)
	}

	results := getCollectedShotResults(eng, "screenshot_results")

	res := runResult{
		Query:       *query,
		OutDir:      *outDir,
		Screenshots: []shotInfo{},
	}
	for _, r := range results {
		if r.Error != "" {
			res.Errors = append(res.Errors, shotErr{URL: r.URL, Error: r.Error})
			continue
		}
		info := shotInfo{URL: r.URL, Path: r.Path}
		if st, statErr := os.Stat(r.Path); statErr == nil {
			info.Bytes = st.Size()
		}
		res.Screenshots = append(res.Screenshots, info)
	}

	// Human-readable summary on stderr; machine-readable JSON on stdout.
	fmt.Fprintf(os.Stderr, "captured %d screenshot(s), %d failure(s)\n",
		len(res.Screenshots), len(res.Errors))
	for _, s := range res.Screenshots {
		fmt.Fprintf(os.Stderr, "  ok  %s -> %s (%d bytes)\n", s.URL, s.Path, s.Bytes)
	}
	for _, e := range res.Errors {
		fmt.Fprintf(os.Stderr, "  err %s : %s\n", e.URL, e.Error)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		log.Fatalf("encode output: %v", err)
	}
}

// getCollectedShotResults reads a CollectInto-produced wire (*[]any of
// *ShotResult).
func getCollectedShotResults(eng *dagor.Engine, wire string) []ShotResult {
	raw, ok := eng.GetOutput(wire)
	if !ok {
		return nil
	}
	p, ok := raw.(*[]any)
	if !ok || p == nil {
		return nil
	}
	out := make([]ShotResult, 0, len(*p))
	for _, v := range *p {
		if sp, ok := v.(*ShotResult); ok && sp != nil {
			out = append(out, *sp)
			continue
		}
		if sv, ok := v.(ShotResult); ok {
			out = append(out, sv)
		}
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
