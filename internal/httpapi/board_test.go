package httpapi

import (
	"bytes"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const wantCSP = "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; " +
	"img-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// checkBoardHeaders asserts the headers every Board response carries: the
// common ones, the content type, the Content-Security-Policy and no CORS
// header.
func checkBoardHeaders(t *testing.T, w interface{ Header() http.Header }, contentType string) {
	t.Helper()
	hd := w.Header()
	if got := hd.Get("Content-Type"); got != contentType {
		t.Errorf("Content-Type %q, want %q", got, contentType)
	}
	if got := hd.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control %q, want no-store", got)
	}
	if got := hd.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options %q, want nosniff", got)
	}
	if got := hd.Get("Content-Security-Policy"); got != wantCSP {
		t.Errorf("Content-Security-Policy %q, want %q", got, wantCSP)
	}
	for k := range hd {
		if strings.HasPrefix(strings.ToLower(k), "access-control-") {
			t.Errorf("CORS header %s sent", k)
		}
	}
}

// boardRoutes are the Board's routes, the files they serve and their types.
var boardRoutes = []struct{ path, file, contentType string }{
	{"/", "index.html", "text/html; charset=utf-8"},
	{"/?x=1", "index.html", "text/html; charset=utf-8"},
	{"/assets/board.css", "board.css", "text/css; charset=utf-8"},
	{"/assets/board.mjs", "board.mjs", "text/javascript; charset=utf-8"},
	{"/assets/order.mjs", "order.mjs", "text/javascript; charset=utf-8"},
}

func TestBoardRoutes(t *testing.T) {
	h, _ := newHandler(t)
	for _, rt := range boardRoutes {
		t.Run(rt.path, func(t *testing.T) {
			want, err := os.ReadFile("web/" + rt.file)
			if err != nil {
				t.Fatal(err)
			}
			w := do(h, http.MethodGet, rt.path, "127.0.0.1:7318", nil)
			if w.Code != http.StatusOK {
				t.Fatalf("status %d, body %s", w.Code, w.Body)
			}
			checkBoardHeaders(t, w, rt.contentType)
			if !bytes.Equal(w.Body.Bytes(), want) {
				t.Errorf("body is not the embedded web/%s", rt.file)
			}
			if got := w.Header().Get("Content-Length"); got != strconv.Itoa(len(want)) {
				t.Errorf("Content-Length %q, want %d", got, len(want))
			}

			w = do(h, http.MethodHead, rt.path, "localhost", nil)
			if w.Code != http.StatusOK {
				t.Errorf("HEAD status %d, want 200", w.Code)
			}
			checkBoardHeaders(t, w, rt.contentType)
			if w.Body.Len() != 0 {
				t.Errorf("HEAD body of %d bytes, want none", w.Body.Len())
			}
			if got := w.Header().Get("Content-Length"); got != strconv.Itoa(len(want)) {
				t.Errorf("HEAD Content-Length %q, want %d", got, len(want))
			}
		})
	}
}

// TestBoardPage checks the page loads only its own assets and opens only
// /events: no network beyond the Daemon (no CDN, no font service).
func TestBoardPage(t *testing.T) {
	h, _ := newHandler(t)
	page := do(h, http.MethodGet, "/", "localhost", nil).Body.String()
	for _, s := range []string{`href="/assets/board.css"`, `src="/assets/board.mjs"`, `type="module"`} {
		if !strings.Contains(page, s) {
			t.Errorf("page lacks %s", s)
		}
	}
	for _, rt := range boardRoutes {
		body := do(h, http.MethodGet, rt.path, "localhost", nil).Body.String()
		if strings.Contains(body, "http://") || strings.Contains(body, "https://") {
			t.Errorf("%s names a network URL", rt.path)
		}
		if strings.Contains(body, "fetch(") || strings.Contains(body, "XMLHttpRequest") || strings.Contains(body, "WebSocket") {
			t.Errorf("%s fetches something other than /events", rt.path)
		}
	}
	// The strips are one polite, non-atomic live region inside a header,
	// before the Boxes in reading order.
	header := strings.Index(page, "<header")
	strips := strings.Index(page, `<div class="strips" id="strips" role="status" aria-live="polite" aria-atomic="false">`)
	end := strings.Index(page, "</header>")
	if header < 0 || strips < header || end < strips || end > strings.Index(page, `id="board"`) {
		t.Error("page lacks the strips' polite, non-atomic live region in a header before the Board")
	}
	// The CSP forbids inline script and style: the page must carry none.
	for _, s := range []string{"<style", "style=", "<script>"} {
		if strings.Contains(page, s) {
			t.Errorf("page carries inline %q, which the CSP blocks", s)
		}
	}
	if regexp.MustCompile(`\son[a-z]+=`).MatchString(page) {
		t.Error("page carries an inline event handler, which the CSP blocks")
	}
	js := do(h, http.MethodGet, "/assets/board.mjs", "localhost", nil).Body.String()
	if n := strings.Count(js, "new EventSource("); n != 1 || !strings.Contains(js, `new EventSource("/events")`) {
		t.Errorf("board.mjs opens %d EventSources, want exactly one on /events", n)
	}
}

func TestBoardUnknown(t *testing.T) {
	h, _ := newHandler(t)
	for _, p := range []string{
		"/assets/nope.js", "/assets/order.js", "/assets/order.test.mjs", "/assets/index.html", "/assets/board.css/",
		"/assets/../order.mjs", "/assets//order.mjs", "/assets/web/order.mjs", "/assets", "/index.html", "/board.mjs",
		"/order.mjs", "/web/order.mjs",
	} {
		checkError(t, do(h, http.MethodGet, p, "127.0.0.1:7318", nil), http.StatusNotFound, "not_found")
	}
}

func TestBoardMethod(t *testing.T) {
	h, _ := newHandler(t)
	for _, p := range []string{"/", "/assets/order.mjs"} {
		for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodOptions} {
			w := do(h, m, p, "127.0.0.1:7318", nil)
			checkError(t, w, http.StatusMethodNotAllowed, "method_not_allowed")
			if got := w.Header().Get("Allow"); got != "GET, HEAD" {
				t.Errorf("%s %s: Allow %q, want GET, HEAD", m, p, got)
			}
		}
	}
}

func TestBoardBadHost(t *testing.T) {
	h, _ := newHandler(t)
	for _, host := range []string{"evil.example", "", "evil.example:7318", "127.0.0.2", "localhost.evil.example"} {
		for _, p := range []string{"/", "/assets/board.mjs", "/assets/nope.js"} {
			for _, m := range []string{http.MethodGet, http.MethodPost} {
				checkError(t, do(h, m, p, host, nil), http.StatusForbidden, "host_not_allowed")
			}
			// A HEAD gets the error's status and headers, with no body.
			if w := do(h, http.MethodHead, p, host, nil); w.Code != http.StatusForbidden || w.Body.Len() != 0 {
				t.Errorf("HEAD %s Host %q: status %d with %d bytes, want 403 and none", p, host, w.Code, w.Body.Len())
			}
		}
	}
}

// TestBoardServesEveryFile walks web/: every file but the node tests is
// served (index.html at /, the rest under /assets/), and no test file is,
// nor anything in web/check/ (hand-run checks).
func TestBoardServesEveryFile(t *testing.T) {
	h, _ := newHandler(t)
	entries, err := os.ReadDir("web")
	if err != nil {
		t.Fatal(err)
	}
	tests := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			if name != "check" {
				t.Errorf("unexpected directory web/%s", name)
				continue
			}
			checks, err := os.ReadDir("web/check")
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range checks {
				if w := do(h, http.MethodGet, "/assets/check/"+c.Name(), "localhost", nil); w.Code != http.StatusNotFound {
					t.Errorf("check file %s: status %d, want 404", c.Name(), w.Code)
				}
				if _, err := web.ReadFile("web/check/" + c.Name()); err == nil {
					t.Errorf("check file %s is embedded", c.Name())
				}
			}
			continue
		}
		p := "/assets/" + name
		if name == "index.html" {
			p = "/"
		}
		w := do(h, http.MethodGet, p, "localhost", nil)
		if strings.Contains(name, ".test.") {
			tests++
			if w.Code != http.StatusNotFound {
				t.Errorf("test file %s: status %d, want 404", name, w.Code)
			}
			if _, err := web.ReadFile("web/" + name); err == nil {
				t.Errorf("test file %s is embedded", name)
			}
			continue
		}
		if w.Code != http.StatusOK {
			t.Errorf("%s at %s: status %d, want 200", name, p, w.Code)
		}
	}
	if tests == 0 {
		t.Error("no test file found in web/")
	}
}
