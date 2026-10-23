package herddash_test

// This test enforces the architecture spine's import graph
// (docs/planning/architecture/architecture-herddash-2026-10-14/ARCHITECTURE-SPINE.md).
// The arrows in its diagram are the only permitted import directions between this
// module's packages. Changing the set below is a structural change and needs an ADR.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/vilosource/herddash"

// permitted maps each spine node to the nodes it may import. A node is
// cmd/herddash ("cmd") or a top-level directory under internal/; subpackages
// such as internal/herdr/gen belong to their top-level node.
var permitted = map[string][]string{
	"cmd":      {"engine", "httpapi", "importer", "config", "store"},
	"engine":   {"core", "herdr", "procenv", "store"},
	"httpapi":  {"core", "engine", "store"},
	"importer": {"core", "store", "config"},
	"store":    {"core"},
	"herdr":    {"core"},
	"config":   {"core"},
	"core":     {},
	"procenv":  {},
}

// stdlibOnly lists nodes that may import nothing outside the standard library,
// apart from the module's own packages, which the edge rules govern. "herdr"
// covers internal/herdr/gen and genwire too (ADR-0011).
var stdlibOnly = map[string]bool{"core": true, "herdr": true}

// genPath is herdr's generated wire types. It and every package under it may be
// imported only from internal/herdr or a package under it (ADR-0011).
const genPath = modulePath + "/internal/herdr/gen"

// inHerdr reports whether a module-relative directory is internal/herdr or under it.
func inHerdr(dir string) bool {
	return dir == "internal/herdr" || strings.HasPrefix(dir, "internal/herdr/")
}

// noClock lists nodes whose non-test code must never read or wait on the clock.
var noClock = map[string]bool{"core": true}

// clockFuncs are the time package functions a noClock node must not call.
var clockFuncs = map[string]bool{
	"Now": true, "Since": true, "Until": true, "After": true, "Tick": true,
	"NewTimer": true, "NewTicker": true, "AfterFunc": true, "Sleep": true,
}

// skipDirs are directory names never walked, wherever they appear.
var skipDirs = map[string]bool{".git": true, "docs": true, "_bmad": true, "testdata": true}

// nodeOf maps a module-relative directory ("internal/herdr/gen") to its spine
// node, or "" if the directory is not part of the spine.
func nodeOf(rel string) string {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	switch {
	case len(parts) >= 2 && parts[0] == "cmd" && parts[1] == "herddash":
		return "cmd"
	case len(parts) >= 2 && parts[0] == "internal":
		if _, ok := permitted[parts[1]]; ok {
			return parts[1]
		}
	}
	return ""
}

// isStdlib reports whether an import path belongs to the standard library, by
// the usual rule that non-standard paths have a dot in their first element.
func isStdlib(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

// checkTree walks the module tree at root and returns every violation of the
// spine's import rules, sorted. Paths in violations are relative to root.
func checkTree(root string) ([]string, error) {
	allowed := map[string]map[string]bool{}
	for from, tos := range permitted {
		allowed[from] = map[string]bool{}
		for _, to := range tos {
			allowed[from][to] = true
		}
	}

	seen := map[string]bool{}
	offSpine := map[string]bool{}
	var violations []string
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (skipDirs[name] || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}
			return nil
		}
		// _test.go files are deliberately exempt: tests may import whatever they need.
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)
		dir := filepath.ToSlash(filepath.Dir(relPath))
		from := nodeOf(dir)
		if from == "" {
			if !offSpine[dir] {
				offSpine[dir] = true
				violations = append(violations, dir+": package is not part of the architecture spine")
			}
			return nil
		}
		seen[from] = true

		mode := parser.ImportsOnly
		if noClock[from] {
			mode = parser.SkipObjectResolution // the clock check needs function bodies
		}
		file, err := parser.ParseFile(fset, path, nil, mode)
		if err != nil {
			return err
		}

		timeNames := map[string]bool{}
		for _, imp := range file.Imports {
			ipath, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return err
			}
			if ipath == "time" && noClock[from] {
				name := "time"
				if imp.Name != nil {
					name = imp.Name.Name
				}
				if name == "." {
					violations = append(violations, relPath+": "+from+" must not dot-import time")
				}
				timeNames[name] = true
			}

			if ipath == modulePath || strings.HasPrefix(ipath, modulePath+"/") {
				if (ipath == genPath || strings.HasPrefix(ipath, genPath+"/")) && !inHerdr(dir) {
					violations = append(violations, relPath+": imports "+ipath+", which only internal/herdr may import")
				}
				rel := strings.TrimPrefix(strings.TrimPrefix(ipath, modulePath), "/")
				to := nodeOf(rel)
				switch {
				case to == "":
					violations = append(violations, relPath+": imports "+ipath+", which is not part of the architecture spine")
				case to == from:
					// Imports within one node, such as herdr -> herdr/gen, are free.
				case !allowed[from][to]:
					violations = append(violations, relPath+": forbidden import edge "+from+" -> "+to+" ("+ipath+")")
				}
				continue
			}
			if stdlibOnly[from] && !isStdlib(ipath) {
				violations = append(violations, relPath+": "+from+" may import only the standard library, imports "+ipath)
			}
		}

		if noClock[from] && len(timeNames) > 0 {
			ast.Inspect(file, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok || !clockFuncs[sel.Sel.Name] {
					return true
				}
				if id, ok := sel.X.(*ast.Ident); ok && timeNames[id.Name] {
					pos := fset.Position(sel.Pos())
					violations = append(violations, relPath+":"+strconv.Itoa(pos.Line)+": "+from+" must not call time."+sel.Sel.Name+"; take the clock as a parameter")
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	for node := range permitted {
		if !seen[node] {
			violations = append(violations, "spine package "+node+" has no non-test Go files")
		}
	}
	sort.Strings(violations)
	return violations, nil
}

func TestImportGraph(t *testing.T) {
	violations, err := checkTree(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range violations {
		t.Error(v)
	}
}

// TestCheckTreeCatchesViolations runs the checker on a fixture tree that breaks
// each rule once, so a checker that stops enforcing a rule fails here.
func TestCheckTreeCatchesViolations(t *testing.T) {
	violations, err := checkTree(filepath.Join("testdata", "badtree"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"internal/core/bad.go: forbidden import edge core -> store (github.com/vilosource/herddash/internal/store)",
		"internal/core/bad.go: core may import only the standard library, imports example.com/notstd",
		"internal/core/bad.go:11: core must not call time.Since; take the clock as a parameter",
		"internal/core/dot.go: core must not dot-import time",
		"internal/engine/leak.go: imports github.com/vilosource/herddash/internal/herdr/gen, which only internal/herdr may import",
		"internal/engine/leak.go: imports github.com/vilosource/herddash/internal/herdr/gen/genwire, which only internal/herdr may import",
		"internal/foo: package is not part of the architecture spine",
		"internal/herdr/bad.go: herdr may import only the standard library, imports example.com/notstd",
		"internal/herdr/gen/genwire/bad.go: herdr may import only the standard library, imports example.com/notstd",
		"offspine: package is not part of the architecture spine",
	}
	got := map[string]bool{}
	for _, v := range violations {
		got[v] = true
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing violation %q\ngot:\n  %s", w, strings.Join(violations, "\n  "))
		}
	}
	// The permitted edges (cmd -> store, engine -> herdr), imports of gen from
	// internal/herdr and a package under it, and the exempt _test.go files must
	// not be reported.
	for _, v := range violations {
		if strings.Contains(v, "cmd -> store") || strings.Contains(v, "engine -> herdr") ||
			strings.Contains(v, "ok_test.go") || strings.Contains(v, "leak_test.go") ||
			(strings.HasPrefix(v, "internal/herdr/bad.go:") && strings.Contains(v, "internal/herdr/gen")) ||
			(strings.HasPrefix(v, "internal/herdr/gen/genwire/bad.go:") && strings.Contains(v, "only internal/herdr may import")) {
			t.Errorf("unexpected violation: %s", v)
		}
	}
}
