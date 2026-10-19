package herddash_test

// This test enforces the record's single-writer rule (AD-4, AD-9; epic 2):
// only internal/engine, internal/importer and internal/store write to the
// record. Outside them, no non-test Go file may call or take the methods
// Append, AppendResyncMarker or SaveServerState of *store.Store or of the
// engine's Recorder. SaveServerState is guarded too because the epoch it saves
// keys every future row.
//
// It reads syntax, not types, so it is deliberately broad: outside those three
// packages, any selector .Append, .AppendResyncMarker or .SaveServerState
// whose left side is not an imported package's name is reported. A package-level function such as
// reflect.Append is not.

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

// recordWriters are the module directories whose code may append to the record.
var recordWriters = []string{"internal/engine", "internal/importer", "internal/store"}

// recordAppends are the method names that write to the record.
var recordAppends = map[string]bool{"Append": true, "AppendResyncMarker": true, "SaveServerState": true}

func mayWriteRecord(dir string) bool {
	for _, w := range recordWriters {
		if dir == w || strings.HasPrefix(dir, w+"/") {
			return true
		}
	}
	return false
}

// checkCallSites walks the module tree at root and returns, sorted, every
// record append outside the permitted packages. Paths are relative to root.
func checkCallSites(root string) ([]string, error) {
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
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if mayWriteRecord(filepath.ToSlash(filepath.Dir(rel))) {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		imported := map[string]bool{}
		for _, imp := range file.Imports {
			ipath, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return err
			}
			name := ipath[strings.LastIndex(ipath, "/")+1:]
			if imp.Name != nil {
				name = imp.Name.Name
			}
			imported[name] = true
		}
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || !recordAppends[sel.Sel.Name] {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && imported[id.Name] {
				return true // a package-level function, not a method
			}
			pos := fset.Position(sel.Sel.Pos())
			violations = append(violations, rel+":"+strconv.Itoa(pos.Line)+": "+sel.Sel.Name+
				" writes the record; only "+strings.Join(recordWriters, ", ")+" may call it")
			return true
		})
		return nil
	})
	sort.Strings(violations)
	return violations, err
}

func TestRecordCallSites(t *testing.T) {
	violations, err := checkCallSites(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range violations {
		t.Error(v)
	}
}

// TestCheckCallSitesCatchesViolations runs the checker on a fixture tree: a
// cmd file that appends, through a call and a method value, is reported; the
// engine's own appends, a _test.go file and a package-level function are not.
func TestCheckCallSitesCatchesViolations(t *testing.T) {
	got, err := checkCallSites(filepath.Join("testdata", "callsite"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"cmd/herddash/bad.go:11: Append writes the record; only internal/engine, internal/importer, internal/store may call it",
		"cmd/herddash/bad.go:12: AppendResyncMarker writes the record; only internal/engine, internal/importer, internal/store may call it",
		"cmd/herddash/bad.go:13: SaveServerState writes the record; only internal/engine, internal/importer, internal/store may call it",
		"cmd/herddash/bad.go:17: Append writes the record; only internal/engine, internal/importer, internal/store may call it",
		"internal/httpapi/bad.go:9: Append writes the record; only internal/engine, internal/importer, internal/store may call it",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("violations\n got %s\nwant %s", strings.Join(got, "\n     "), strings.Join(want, "\n     "))
	}
}
