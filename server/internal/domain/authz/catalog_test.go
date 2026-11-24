package authz

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var permKeyRegex = regexp.MustCompile(`^[a-z]+:[a-z_]+$`)

// permConstRegex matches the names of the permission key constants.
var permConstRegex = regexp.MustCompile(`^Perm[A-Z]`)

func TestAllPermissions_NoDuplicates(t *testing.T) {
	seen := make(map[string]bool)
	for _, key := range AllPermissions() {
		if seen[key] {
			t.Errorf("duplicate permission key in catalog: %q", key)
		}
		seen[key] = true
	}
}

func TestAllPermissions_KeyShape(t *testing.T) {
	for _, key := range AllPermissions() {
		if !permKeyRegex.MatchString(key) {
			t.Errorf("permission key %q does not match %s", key, permKeyRegex)
		}
	}
}

// TestCatalogCompleteness fails when a Perm constant of this package is not
// in the catalog, or when a catalog key has no Perm constant. It reads the
// constants from the package source, so a new constant needs no change here.
func TestCatalogCompleteness(t *testing.T) {
	want := declaredPermissionKeys(t)
	if len(want) == 0 {
		t.Fatal("found no Perm constant in the package source")
	}
	sort.Strings(want)

	got := AllPermissionsSorted()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("catalog drift:\n  catalog:   %v\n  constants: %v", got, want)
	}
}

// declaredPermissionKeys returns the value of each Perm constant in the
// non-test source files of the package directory.
func declaredPermissionKeys(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}

	var keys []string
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, ident := range valueSpec.Names {
					if !permConstRegex.MatchString(ident.Name) {
						continue
					}
					if i >= len(valueSpec.Values) {
						t.Fatalf("constant %s has no value", ident.Name)
					}
					lit, ok := valueSpec.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						t.Fatalf("constant %s is not a string literal", ident.Name)
					}
					key, err := strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatalf("constant %s: %v", ident.Name, err)
					}
					keys = append(keys, key)
				}
			}
		}
	}
	return keys
}

func TestAllPermissions_FreshCopy(t *testing.T) {
	a := AllPermissions()
	a[0] = "tampered"

	b := AllPermissions()
	if b[0] == "tampered" {
		t.Fatal("AllPermissions returned a shared backing array; caller mutation leaked into subsequent calls")
	}
}

func TestCatalog_FreshCopy(t *testing.T) {
	a := Catalog()
	a[0].Key = "tampered"

	b := Catalog()
	if b[0].Key == "tampered" {
		t.Fatal("Catalog returned a shared backing array; caller mutation leaked into subsequent calls")
	}
}

func TestLookup_UnknownKeyReturnsFalse(t *testing.T) {
	if _, ok := Lookup("does:not_exist"); ok {
		t.Fatal("Lookup returned ok for an unknown key")
	}
}
