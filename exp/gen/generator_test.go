package gen

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateNamespaceFuncMap(t *testing.T) {
	source, err := Generate(context.Background(), Options{
		PackagePath: "github.com/donseba/go-doc/exp/gen/testdata/helpers",
		Namespace:   "helpers",
		PackageName: "generated",
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	text := string(source)

	assertContains(t, text, `package generated`)
	assertContains(t, text, `helpers "github.com/donseba/go-doc/exp/gen/testdata/helpers"`)
	assertContains(t, text, `"helpers": func() HelpersNamespace { return HelpersNamespace{} }`)
	assertContains(t, text, `func (HelpersNamespace) Add(v0 int, v1 int) int`)
	assertContains(t, text, `return helpers.Add(v0, v1)`)
	assertContains(t, text, `func (HelpersNamespace) Format(v0 time.Time, v1 string) string`)
	assertContains(t, text, `func (HelpersNamespace) First(v0 []helpers.User) helpers.User`)
	assertContains(t, text, `func (HelpersNamespace) Join(v0 ...string) string`)
	assertContains(t, text, `return helpers.Join(v0...)`)

	assertNotContains(t, text, "SkipGeneric")
	assertNotContains(t, text, "hidden")

	if _, err := parser.ParseFile(token.NewFileSet(), "generated.go", source, parser.AllErrors); err != nil {
		t.Fatalf("generated source is not parseable: %v\n%s", err, text)
	}
}

func TestGenerateDefaultsNamespace(t *testing.T) {
	source, err := Generate(context.Background(), Options{
		PackagePath: "github.com/donseba/go-doc/exp/gen/testdata/helpers",
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	text := string(source)

	assertContains(t, text, `package godocgen`)
	assertContains(t, text, `"helpers": func() HelpersNamespace { return HelpersNamespace{} }`)
}

func TestGenerateRequiresPackagePath(t *testing.T) {
	_, err := Generate(context.Background(), Options{})
	if err == nil {
		t.Fatal("Generate() error = nil, want error")
	}
}

func TestDirectivesFromFiles(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "page.gohtml")
	err := os.WriteFile(file, []byte(`{{/*
@model Page github.com/example/app.Page
@gen view github.com/example/app/viewfuncs
*/}}`), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	directives, err := DirectivesFromFiles(file)
	if err != nil {
		t.Fatalf("DirectivesFromFiles() error = %v", err)
	}
	if len(directives) != 1 {
		t.Fatalf("len(directives) = %d, want 1", len(directives))
	}
	if directives[0].Namespace != "view" {
		t.Fatalf("Namespace = %q, want view", directives[0].Namespace)
	}
	if directives[0].PackagePath != "github.com/example/app/viewfuncs" {
		t.Fatalf("PackagePath = %q", directives[0].PackagePath)
	}
}

func TestDirectivesFromDir(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "templates", "page.gohtml"), `{{/* @gen view github.com/example/app/viewfuncs */}}`)
	writeTestFile(t, filepath.Join(dir, "templates", "card.tmpl"), `{{/* @gen money github.com/example/app/moneyfuncs */}}`)
	writeTestFile(t, filepath.Join(dir, "vendor", "skip.gohtml"), `{{/* @gen nope github.com/example/app/nope */}}`)
	writeTestFile(t, filepath.Join(dir, "gen", "generated.go"), `package gen`)

	directives, err := DirectivesFromDir(dir)
	if err != nil {
		t.Fatalf("DirectivesFromDir() error = %v", err)
	}
	if len(directives) != 2 {
		t.Fatalf("len(directives) = %d, want 2: %#v", len(directives), directives)
	}
	if directives[0].Namespace != "money" || directives[1].Namespace != "view" {
		t.Fatalf("directives = %#v, want sorted template scan with money and view", directives)
	}
}

func assertContains(t *testing.T, text, fragment string) {
	t.Helper()
	if !strings.Contains(text, fragment) {
		t.Fatalf("generated source does not contain %q\n%s", fragment, text)
	}
}

func assertNotContains(t *testing.T, text, fragment string) {
	t.Helper()
	if strings.Contains(text, fragment) {
		t.Fatalf("generated source contains %q\n%s", fragment, text)
	}
}

func writeTestFile(t *testing.T, file, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
