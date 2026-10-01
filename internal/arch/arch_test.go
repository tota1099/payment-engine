// Package arch enforces docs/architecture.md: Clean Architecture layers inside
// each context, dependencies pointing inward, and sealed contexts. Every
// internal/ directory not listed in infra is a context.
package arch

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const module = "github.com/renanporto/payment-engine/internal/"

var infra = []string{"arch", "db", "eventbus", "httpx", "kernel", "psp", "testdb"}

// layers lists, innermost first, what each layer may import besides the stdlib.
// "@" stands for the context's own path.
var layers = map[string][]string{
	"domain":           {"github.com/google/uuid", module + "kernel"},
	"app":              {"github.com/google/uuid", module + "kernel", module + "psp", "@/domain"},
	"adapter/postgres": {"github.com/google/uuid", module + "kernel", module + "psp", module + "db", "github.com/jackc/pgx/v5", "@/domain", "@/app"},
	"adapter/rest":     {"github.com/google/uuid", module + "kernel", module + "psp", module + "httpx", "@/domain", "@/app"},
}

// Tests may also build fixtures on a real database.
var testExtras = []string{module + "testdb", module + "db"}

// The inner layers stay free of transport and storage.
var innerForbidden = []string{"net/http", "database/sql"}

func TestLayers(t *testing.T) {
	entries, err := os.ReadDir("..")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() || slices.Contains(infra, e.Name()) {
			continue
		}
		ctx := e.Name()
		_ = filepath.WalkDir(filepath.Join("..", ctx), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			layer := filepath.ToSlash(filepath.Dir(strings.TrimPrefix(path, filepath.Join("..", ctx)+string(filepath.Separator))))
			rel := ctx + "/" + layer + "/" + d.Name()
			allowed, ok := layers[layer]
			if !ok {
				t.Errorf("%s: unknown layer %q (want domain, app, adapter/postgres or adapter/rest)", rel, layer)
				return nil
			}
			for _, imp := range imports(t, path) {
				if msg := check(ctx, layer, strings.HasSuffix(path, "_test.go"), allowed, imp); msg != "" {
					t.Errorf("%s imports %s: %s", rel, imp, msg)
				}
			}
			return nil
		})
	}
}

func TestKernelIsLeaf(t *testing.T) {
	files, _ := filepath.Glob("../kernel/*.go")
	for _, f := range files {
		for _, imp := range imports(t, f) {
			if !isStdlib(imp) && imp != "github.com/google/uuid" {
				t.Errorf("kernel/%s imports %s: the shared kernel depends on nothing", filepath.Base(f), imp)
			}
		}
	}
}

func check(ctx, layer string, test bool, allowed []string, imp string) string {
	own := module + ctx + "/"
	resolved := make([]string, len(allowed))
	for i, a := range allowed {
		resolved[i] = strings.Replace(a, "@/", own, 1)
	}
	if test {
		resolved = append(resolved, testExtras...)
	}
	switch {
	case isStdlib(imp):
		if (layer == "domain" || layer == "app") && slices.ContainsFunc(innerForbidden, func(p string) bool { return strings.HasPrefix(imp, p) }) {
			return "domain and app stay transport- and storage-free"
		}
		return ""
	case slices.Contains(resolved, imp):
		return ""
	case strings.HasPrefix(imp, own):
		return "dependencies point inward: domain ← app ← adapters"
	case strings.HasPrefix(imp, module):
		return "contexts are sealed; declare an output port in app and wire it in cmd/*"
	}
	return "not allowed in this layer (see docs/architecture.md)"
}

func isStdlib(path string) bool { return !strings.Contains(strings.Split(path, "/")[0], ".") }

func imports(t *testing.T, file string) []string {
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		out = append(out, p)
	}
	return out
}
