package herddash_test

// These tests enforce the record's single-writer rule (AD-4, AD-9; epic 2):
// only internal/engine, internal/importer and internal/store write to the
// record. Outside them, no non-test Go file may call or take the methods
// Append, AppendResyncMarker or SaveServerState of *store.Store or of the
// engine's Recorder. SaveServerState is guarded too because the epoch it saves
// keys every future row. The Retention sweep, Sweep, is narrower still: only
// internal/engine (and internal/store, which defines it) may call it, so the
// importer may not (FR-9). AppendBackfill, the import's one-transaction
// write, is narrower the other way: only internal/importer (and
// internal/store) may call it, so the engine may not (AD-13).
// AppendAcknowledgements, the acknowledgement write (ADR-0014), is the
// engine's alone: only internal/engine (and internal/store) may call it, so
// neither httpapi nor the importer may.
//
// It reads syntax, not types, so it is deliberately broad: outside the
// permitted packages, any selector of a guarded name whose left side is not an
// imported package's name is reported. A package-level function such as
// reflect.Append is not.
//
// TestOnlyDelete enforces that the sweep is the only delete in the codebase:
// DELETE, in any case, appears in no non-test Go string literal and no .sql
// file outside internal/store/sweep.go. SQL comments are not statements, so a
// .sql file's comments are not searched.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// recordWriters are the module directories whose code may append to the record.
var recordWriters = []string{"internal/engine", "internal/importer", "internal/store"}

// recordSweepers are the module directories whose code may sweep the record.
var recordSweepers = []string{"internal/engine", "internal/store"}

// recordAcknowledgers are the module directories whose code may record an
// acknowledgement.
var recordAcknowledgers = []string{"internal/engine", "internal/store"}

// recordImporters are the module directories whose code may import history.
var recordImporters = []string{"internal/importer", "internal/store"}

// guarded maps each method name that writes to the record to what it does and
// the directories that may call it.
var guarded = map[string]struct {
	does    string
	allowed []string
}{
	"Append":             {"writes the record", recordWriters},
	"AppendResyncMarker": {"writes the record", recordWriters},
	"SaveServerState":    {"writes the record", recordWriters},
	"Sweep":              {"deletes from the record", recordSweepers},
	"AppendBackfill":     {"imports into the record", recordImporters},

	"AppendAcknowledgements": {"acknowledges in the record", recordAcknowledgers},
}

// mayCallAll reports whether code in dir may call every guarded method, so
// its files need no reading.
func mayCallAll(dir string) bool {
	for _, g := range guarded {
		if !within(dir, g.allowed) {
			return false
		}
	}
	return true
}

// within reports whether dir is one of dirs or below one.
func within(dir string, dirs []string) bool {
	for _, w := range dirs {
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
		dir := filepath.ToSlash(filepath.Dir(rel))
		if mayCallAll(dir) {
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
			if !ok {
				return true
			}
			g, ok := guarded[sel.Sel.Name]
			if !ok || within(dir, g.allowed) {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && imported[id.Name] {
				return true // a package-level function, not a method
			}
			pos := fset.Position(sel.Sel.Pos())
			violations = append(violations, rel+":"+strconv.Itoa(pos.Line)+": "+sel.Sel.Name+
				" "+g.does+"; only "+strings.Join(g.allowed, ", ")+" may call it")
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
// cmd file that appends or sweeps, through a call and a method value, is
// reported, and so is the importer sweeping; the engine's own appends and
// sweep, the importer's appends and backfill, a _test.go file and a
// package-level function are not. The engine importing backfill is reported.
// An acknowledgement recorded from cmd, httpapi or the importer is reported;
// the engine's is not.
func TestCheckCallSitesCatchesViolations(t *testing.T) {
	got, err := checkCallSites(filepath.Join("testdata", "callsite"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"cmd/herddash/backfill.go:6: AppendBackfill imports into the record; only internal/importer, internal/store may call it",
		"cmd/herddash/bad.go:11: Append writes the record; only internal/engine, internal/importer, internal/store may call it",
		"cmd/herddash/bad.go:12: AppendResyncMarker writes the record; only internal/engine, internal/importer, internal/store may call it",
		"cmd/herddash/bad.go:13: SaveServerState writes the record; only internal/engine, internal/importer, internal/store may call it",
		"cmd/herddash/bad.go:14: Sweep deletes from the record; only internal/engine, internal/store may call it",
		"cmd/herddash/bad.go:15: AppendAcknowledgements acknowledges in the record; only internal/engine, internal/store may call it",
		"cmd/herddash/bad.go:20: Append writes the record; only internal/engine, internal/importer, internal/store may call it",
		"cmd/herddash/bad.go:21: Sweep deletes from the record; only internal/engine, internal/store may call it",
		"internal/engine/backfill.go:5: AppendBackfill imports into the record; only internal/importer, internal/store may call it",
		"internal/httpapi/bad.go:12: Append writes the record; only internal/engine, internal/importer, internal/store may call it",
		"internal/httpapi/bad.go:13: AppendAcknowledgements acknowledges in the record; only internal/engine, internal/store may call it",
		"internal/importer/bad.go:10: Sweep deletes from the record; only internal/engine, internal/store may call it",
		"internal/importer/bad.go:13: AppendAcknowledgements acknowledges in the record; only internal/engine, internal/store may call it",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("violations\n got %s\nwant %s", strings.Join(got, "\n     "), strings.Join(want, "\n     "))
	}
}

// deleteSource is the one file whose code may delete from the record.
const deleteSource = "internal/store/sweep.go"

// deleteWord matches DELETE as a word, in any case: "deleted" is not it.
var deleteWord = regexp.MustCompile(`(?i)\bdelete\b`)

// sqlComment matches an SQL comment, line or block.
var sqlComment = regexp.MustCompile(`(?s)--[^\n]*|/\*.*?\*/`)

// checkDeletes walks the module tree at root and returns, sorted, every DELETE
// in a non-test Go string literal or a .sql file outside deleteSource. Paths
// are relative to root.
func checkDeletes(root string) ([]string, error) {
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
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == deleteSource {
			return nil
		}
		switch {
		case strings.HasSuffix(path, ".sql"):
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			// Blank the comments but keep their newlines, so lines still count.
			text := sqlComment.ReplaceAllStringFunc(string(b), func(c string) string {
				return strings.Repeat("\n", strings.Count(c, "\n"))
			})
			for i, line := range strings.Split(text, "\n") {
				if deleteWord.MatchString(line) {
					violations = append(violations, rel+":"+strconv.Itoa(i+1)+": DELETE outside "+deleteSource)
				}
			}
		case strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go"):
			file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				v, err := strconv.Unquote(lit.Value)
				if err != nil {
					v = lit.Value
				}
				if deleteWord.MatchString(v) {
					pos := fset.Position(lit.Pos())
					violations = append(violations, rel+":"+strconv.Itoa(pos.Line)+": DELETE outside "+deleteSource)
				}
				return true
			})
		}
		return nil
	})
	sort.Strings(violations)
	return violations, err
}

func TestOnlyDelete(t *testing.T) {
	violations, err := checkDeletes(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range violations {
		t.Error(v)
	}
	// The sweep itself is still there: the rule is not vacuous.
	b, err := os.ReadFile(deleteSource)
	if err != nil {
		t.Fatal(err)
	}
	if !deleteWord.Match(b) {
		t.Errorf("%s holds no DELETE", deleteSource)
	}
}

// TestCheckDeletesCatchesViolations runs the checker on a fixture tree: a Go
// string and a .sql statement are reported, in any case; the sweep's file, a
// _test.go file, a Go comment, the builtin delete and an SQL comment are not.
func TestCheckDeletesCatchesViolations(t *testing.T) {
	got, err := checkDeletes(filepath.Join("testdata", "deletes"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"internal/engine/bad.go:5: DELETE outside internal/store/sweep.go",
		"internal/engine/bad.go:7: DELETE outside internal/store/sweep.go",
		"internal/store/migrations/0009_bad.sql:5: DELETE outside internal/store/sweep.go",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("violations\n got %s\nwant %s", strings.Join(got, "\n     "), strings.Join(want, "\n     "))
	}
}
