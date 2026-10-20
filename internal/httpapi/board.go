package httpapi

import (
	"embed"
	"net/http"
	"path"
	"strconv"
	"strings"
)

// web holds the Board's files (Story 3.7): hand-written HTML, CSS and ES
// modules, with no build step. They are named one by one so that the node
// tests beside them are not embedded.
//
//go:embed web/index.html web/board.css web/board.mjs web/order.mjs
var web embed.FS

// The Board's routes (ADR-0007).
const (
	// pathBoard is the Board page: the root path only.
	pathBoard = "/"
	// prefixAssets is where the Board's other files are served.
	prefixAssets = "/assets/"
)

// boardCSP is the Content-Security-Policy of every response under / and
// /assets/: the page may load only its own files and connect only to its own
// origin (/events), with no inline script or style.
const boardCSP = "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; " +
	"img-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// boardFile is one embedded file with its content type.
type boardFile struct {
	contentType string
	body        []byte
}

// boardPage is the Board page, served at /.
var boardPage = mustBoardFile("index.html")

// boardAssets are the files under /assets/, by name.
var boardAssets = map[string]boardFile{
	"board.css": mustBoardFile("board.css"),
	"board.mjs": mustBoardFile("board.mjs"),
	"order.mjs": mustBoardFile("order.mjs"),
}

// contentTypes maps the Board's file extensions to their content types; it
// is explicit so a machine's MIME tables cannot change what is served.
var contentTypes = map[string]string{
	".html": "text/html; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".mjs":  "text/javascript; charset=utf-8",
}

func mustBoardFile(name string) boardFile {
	body, err := web.ReadFile("web/" + name)
	if err != nil {
		panic("httpapi: embedded Board file missing: " + name)
	}
	ct, ok := contentTypes[path.Ext(name)]
	if !ok {
		panic("httpapi: no content type for Board file " + name)
	}
	return boardFile{contentType: ct, body: body}
}

// boardAsset returns the asset a path under /assets/ names, if any.
func boardAsset(p string) (boardFile, bool) {
	name, ok := strings.CutPrefix(p, prefixAssets)
	if !ok {
		return boardFile{}, false
	}
	f, ok := boardAssets[name]
	return f, ok
}

// serveBoardFile writes f with its length; a HEAD gets the headers only.
func serveBoardFile(w http.ResponseWriter, r *http.Request, f boardFile) {
	w.Header().Set("Content-Type", f.contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(f.body)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		w.Write(f.body)
	}
}
