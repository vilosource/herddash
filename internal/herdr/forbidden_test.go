package herdr

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// forbiddenMethods are herdr methods and subscriptions herddash must never
// build (AD-5): focus marks a completion seen, and reading pane output is not
// passive. gen/ guards its own constants in TestNoForbiddenRequests.
var forbiddenMethods = []string{
	"pane.focus", "agent.focus", "pane.read", "agent.read", "pane.wait_for_output", "pane.output_matched",
}

// forbiddenNameParts must not appear in the name of any *Client method.
var forbiddenNameParts = []string{"Focus", "Read", "Output"}

// forbiddenLiterals parses every non-test .go file under root, skipping gen/,
// and returns "file:line: literal" for each string literal naming a forbidden
// method.
func forbiddenLiterals(root string) ([]string, error) {
	var found []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && (d.Name() == "gen" || d.Name() == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || (lit.Kind != token.STRING && lit.Kind != token.CHAR) {
				return true
			}
			value, err := strconv.Unquote(lit.Value)
			if err != nil {
				value = lit.Value
			}
			for _, m := range forbiddenMethods {
				if strings.Contains(value, m) {
					rel, _ := filepath.Rel(root, path)
					found = append(found, fmt.Sprintf("%s:%d: %q names %s", rel, fset.Position(lit.Pos()).Line, value, m))
				}
			}
			return true
		})
		return nil
	})
	sort.Strings(found)
	return found, err
}

func TestNoForbiddenMethodLiterals(t *testing.T) {
	found, err := forbiddenLiterals(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range found {
		t.Errorf("forbidden herdr method outside gen/ (AD-5): %s", f)
	}
}

// TestForbiddenLiteralsNamesTheFile proves the check above can fail, and that
// it names the offending file.
func TestForbiddenLiteralsNamesTheFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, src string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("bad.go", "package herdr\n\nconst m = \"pane.focus\"\n")
	write("raw.go", "package herdr\n\nvar r = `{\"method\":\"agent.read\"}`\n")
	write("ok_test.go", "package herdr\n\nconst m = \"pane.focus\"\n")
	write("gen/wire.go", "package gen\n\nconst m = \"pane.read\"\n")
	write("fine.go", "package herdr\n\nconst m = \"pane.process_info\"\n")

	found, err := forbiddenLiterals(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 || !strings.HasPrefix(found[0], "bad.go:3:") || !strings.HasPrefix(found[1], "raw.go:3:") {
		t.Errorf("found %q, want bad.go and raw.go only", found)
	}
}

func TestClientHasNoFocusOrOutputMethod(t *testing.T) {
	typ := reflect.TypeFor[*Client]()
	if typ.NumMethod() == 0 {
		t.Fatal("*Client has no exported methods to check")
	}
	for i := range typ.NumMethod() {
		name := typ.Method(i).Name
		for _, part := range forbiddenNameParts {
			if strings.Contains(name, part) {
				t.Errorf("*Client.%s: a method named with %q is forbidden (AD-5)", name, part)
			}
		}
	}
}
