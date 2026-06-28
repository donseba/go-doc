package renderer

import (
	"bytes"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testPage struct {
	Title string
}

func TestRegisterAddsModelAccessorsToTemplate(t *testing.T) {
	tmpl := template.New("page")
	if err := Register(tmpl, Root("page", testPage{Title: "Hello"})); err != nil {
		t.Fatal(err)
	}
	if _, err := tmpl.Parse(`{{ page.Title }}`); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := tmpl.Execute(&out, nil); err != nil {
		t.Fatal(err)
	}
	if out.String() != "Hello" {
		t.Fatalf("output = %q, want Hello", out.String())
	}
}

func TestRegisterRejectsInvalidRoot(t *testing.T) {
	err := Register(template.New("page"), Root("", testPage{}))
	if err == nil || !strings.Contains(err.Error(), "invalid root name") {
		t.Fatalf("err = %v, want invalid root name", err)
	}
}

func TestRegisterRejectsDuplicateRoot(t *testing.T) {
	err := Register(template.New("page"), Root("page", testPage{}), Root("page", testPage{}))
	if err == nil || !strings.Contains(err.Error(), "duplicate root name") {
		t.Fatalf("err = %v, want duplicate root name", err)
	}
}

func TestRegisterRejectsRootNamesThatCannotBeTemplateFunctions(t *testing.T) {
	err := Register(template.New("page"), Root("todo-item", testPage{}))
	if err == nil || !strings.Contains(err.Error(), "invalid root name") {
		t.Fatalf("err = %v, want invalid root name", err)
	}
}

func TestRegisterRejectsNilTemplate(t *testing.T) {
	err := Register(nil, Root("page", testPage{}))
	if err == nil || !strings.Contains(err.Error(), "nil template") {
		t.Fatalf("err = %v, want nil template", err)
	}
}

func TestRegisterFromFilesUsesRootDeclarationNames(t *testing.T) {
	root := t.TempDir()
	file := writeTemplate(t, root, "page.gohtml", `{{/*
@model Page github.com/donseba/go-doc/renderer.testPage
*/}}
{{ Page.Title }}`)

	tmpl := template.New("page.gohtml")
	if err := RegisterFromFiles(tmpl, []any{testPage{Title: "Hello from contract"}}, file); err != nil {
		t.Fatal(err)
	}
	if _, err := tmpl.ParseFiles(file); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "page.gohtml", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Hello from contract") {
		t.Fatalf("output = %q, want contract model value", out.String())
	}
}

func TestRendererDevelopmentModeScansOnRegister(t *testing.T) {
	root := t.TempDir()
	file := writeTemplate(t, root, "page.gohtml", `{{/*
@model Page github.com/donseba/go-doc/renderer.testPage
*/}}
{{ Page.Title }}`)

	r, err := New(Config{Mode: Development, Files: []string{file}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(`{{/*
@model Renamed github.com/donseba/go-doc/renderer.testPage
*/}}
{{ Renamed.Title }}`), 0o644); err != nil {
		t.Fatal(err)
	}

	tmpl := template.New("page.gohtml")
	if err := r.Register(tmpl, testPage{Title: "Development contract"}); err != nil {
		t.Fatal(err)
	}
	if _, err := tmpl.ParseFiles(file); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "page.gohtml", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Development contract") {
		t.Fatalf("output = %q, want development contract model value", out.String())
	}
}

func TestRendererRegisterInstallsDefaultFuncs(t *testing.T) {
	root := t.TempDir()
	file := writeTemplate(t, root, "page.gohtml", `{{/*
@model Page github.com/donseba/go-doc/renderer.testPage
*/}}
{{ upper Page.Title }}`)

	r, err := New(Config{
		Mode:  Development,
		Files: []string{file},
		Funcs: template.FuncMap{
			"upper": strings.ToUpper,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	tmpl := template.New("page.gohtml")
	if err := r.Register(tmpl, testPage{Title: "default funcs"}); err != nil {
		t.Fatal(err)
	}
	if _, err := tmpl.ParseFiles(file); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "page.gohtml", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "DEFAULT FUNCS") {
		t.Fatalf("output = %q, want default func output", out.String())
	}
}

func TestRendererDefaultFuncsAreCloned(t *testing.T) {
	root := t.TempDir()
	file := writeTemplate(t, root, "page.gohtml", `{{/*
@model Page github.com/donseba/go-doc/renderer.testPage
*/}}
{{ suffix Page.Title }}`)
	funcs := template.FuncMap{
		"suffix": func(value string) string { return value + "-original" },
	}

	r, err := New(Config{Mode: Development, Files: []string{file}, Funcs: funcs})
	if err != nil {
		t.Fatal(err)
	}
	funcs["suffix"] = func(value string) string { return value + "-changed" }

	tmpl := template.New("page.gohtml")
	if err := r.Register(tmpl, testPage{Title: "value"}); err != nil {
		t.Fatal(err)
	}
	if _, err := tmpl.ParseFiles(file); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "page.gohtml", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "value-original") {
		t.Fatalf("output = %q, want cloned default funcs", out.String())
	}
}

func TestUseFuncsRejectsInvalidFuncMap(t *testing.T) {
	err := UseFuncs(template.New("page"), template.FuncMap{"bad-name": func() string { return "" }})
	if err == nil || !strings.Contains(err.Error(), "function name") {
		t.Fatalf("err = %v, want function name error", err)
	}
}

func TestRendererProductionModeUsesStartupContracts(t *testing.T) {
	root := t.TempDir()
	file := writeTemplate(t, root, "page.gohtml", `{{/*
@model Page github.com/donseba/go-doc/renderer.testPage
*/}}
{{ Page.Title }}`)

	r, err := New(Config{Mode: Production, Files: []string{file}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(`{{/*
@model Renamed github.com/donseba/go-doc/renderer.testPage
*/}}
{{ Renamed.Title }}`), 0o644); err != nil {
		t.Fatal(err)
	}

	tmpl := template.New("page.gohtml")
	if err := r.Register(tmpl, testPage{Title: "stale"}); err != nil {
		t.Fatal(err)
	}
	if _, err := tmpl.ParseFiles(file); err == nil || !strings.Contains(err.Error(), "function \"Renamed\" not defined") {
		t.Fatalf("ParseFiles() error = %v, want stale contract parse error", err)
	}
}

func TestRendererDefaultsToProductionMode(t *testing.T) {
	root := t.TempDir()
	file := writeTemplate(t, root, "page.gohtml", `{{/*
@model Page github.com/donseba/go-doc/renderer.testPage
*/}}
{{ Page.Title }}`)

	r, err := New(Config{Files: []string{file}})
	if err != nil {
		t.Fatal(err)
	}
	if r.mode != Production {
		t.Fatalf("mode = %q, want production", r.mode)
	}
}

func TestRendererRejectsUnknownMode(t *testing.T) {
	_, err := New(Config{Mode: "staging"})
	if err == nil || !strings.Contains(err.Error(), "unknown renderer mode") {
		t.Fatalf("err = %v, want unknown mode error", err)
	}
}

func TestScanContractsCanBeReusedWithRegisterFromContracts(t *testing.T) {
	root := t.TempDir()
	file := writeTemplate(t, root, "page.gohtml", `{{/*
@model Page github.com/donseba/go-doc/renderer.testPage
*/}}
{{ Page.Title }}`)

	contracts, err := ScanContracts(file)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := template.New("page.gohtml")
	if err := RegisterFromContracts(tmpl, contracts, testPage{Title: "Cached contract"}); err != nil {
		t.Fatal(err)
	}
	if _, err := tmpl.ParseFiles(file); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "page.gohtml", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Cached contract") {
		t.Fatalf("output = %q, want cached contract model value", out.String())
	}
}

func TestLoadContractsCanBeReusedAtStartup(t *testing.T) {
	root := t.TempDir()
	file := writeTemplate(t, root, "page.gohtml", `{{/*
@model Page github.com/donseba/go-doc/renderer.testPage
*/}}
{{ Page.Title }}`)

	contracts, err := LoadContracts(file)
	if err != nil {
		t.Fatal(err)
	}
	if got := contracts.Roots()["Page"]; got != "github.com/donseba/go-doc/renderer.testPage" {
		t.Fatalf("contract Page = %q", got)
	}

	tmpl := template.New("page.gohtml")
	if err := contracts.Register(tmpl, testPage{Title: "Startup contract"}); err != nil {
		t.Fatal(err)
	}
	if _, err := tmpl.ParseFiles(file); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "page.gohtml", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Startup contract") {
		t.Fatalf("output = %q, want startup contract model value", out.String())
	}
}

func TestLoadedContractsDoNotChangeWhenTemplateDeclarationChanges(t *testing.T) {
	root := t.TempDir()
	file := writeTemplate(t, root, "page.gohtml", `{{/*
@model Page github.com/donseba/go-doc/renderer.testPage
*/}}
{{ Page.Title }}`)

	contracts, err := LoadContracts(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(`{{/*
@model Renamed github.com/donseba/go-doc/renderer.testPage
*/}}
{{ Renamed.Title }}`), 0o644); err != nil {
		t.Fatal(err)
	}

	tmpl := template.New("page.gohtml")
	if err := contracts.Register(tmpl, testPage{Title: "stale"}); err != nil {
		t.Fatal(err)
	}
	if _, err := tmpl.ParseFiles(file); err == nil || !strings.Contains(err.Error(), "function \"Renamed\" not defined") {
		t.Fatalf("ParseFiles() error = %v, want stale contract parse error", err)
	}
}

func TestRegisterFromFilesSeesTemplateDeclarationChanges(t *testing.T) {
	root := t.TempDir()
	file := writeTemplate(t, root, "page.gohtml", `{{/*
@model Page github.com/donseba/go-doc/renderer.testPage
*/}}
{{ Page.Title }}`)

	if err := os.WriteFile(file, []byte(`{{/*
@model Renamed github.com/donseba/go-doc/renderer.testPage
*/}}
{{ Renamed.Title }}`), 0o644); err != nil {
		t.Fatal(err)
	}

	tmpl := template.New("page.gohtml")
	if err := RegisterFromFiles(tmpl, []any{testPage{Title: "Dynamic contract"}}, file); err != nil {
		t.Fatal(err)
	}
	if _, err := tmpl.ParseFiles(file); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "page.gohtml", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Dynamic contract") {
		t.Fatalf("output = %q, want dynamic contract model value", out.String())
	}
}

func TestRegisterFromFilesUsesRenamedRootDeclaration(t *testing.T) {
	root := t.TempDir()
	file := writeTemplate(t, root, "page.gohtml", `{{/*
@model XXX github.com/donseba/go-doc/renderer.testPage
*/}}
{{ XXX.Title }}`)

	tmpl := template.New("page.gohtml")
	if err := RegisterFromFiles(tmpl, []any{testPage{Title: "Renamed contract"}}, file); err != nil {
		t.Fatal(err)
	}
	if _, err := tmpl.ParseFiles(file); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "page.gohtml", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Renamed contract") {
		t.Fatalf("output = %q, want renamed contract model value", out.String())
	}
}

func TestRegisterFromFilesMatchesPointerValues(t *testing.T) {
	root := t.TempDir()
	file := writeTemplate(t, root, "page.gohtml", `{{/*
@model Page github.com/donseba/go-doc/renderer.testPage
*/}}
{{ Page.Title }}`)

	tmpl := template.New("page.gohtml")
	if err := RegisterFromFiles(tmpl, []any{&testPage{Title: "Pointer"}}, file); err != nil {
		t.Fatal(err)
	}
	if _, err := tmpl.ParseFiles(file); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "page.gohtml", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Pointer") {
		t.Fatalf("output = %q, want pointer model value", out.String())
	}
}

func TestRegisterFromFilesScansOneLineTemplateComment(t *testing.T) {
	root := t.TempDir()
	file := writeTemplate(t, root, "page.gohtml", `{{/* @model Page github.com/donseba/go-doc/renderer.testPage */}}
{{ Page.Title }}`)

	tmpl := template.New("page.gohtml")
	if err := RegisterFromFiles(tmpl, []any{testPage{Title: "One line"}}, file); err != nil {
		t.Fatal(err)
	}
	if _, err := tmpl.ParseFiles(file); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "page.gohtml", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "One line") {
		t.Fatalf("output = %q, want one-line contract model value", out.String())
	}
}

func TestRegisterFromFilesRejectsMissingRootValue(t *testing.T) {
	root := t.TempDir()
	file := writeTemplate(t, root, "page.gohtml", `{{/*
@model Page github.com/donseba/go-doc/renderer.testPage
*/}}`)

	err := RegisterFromFiles(template.New("page.gohtml"), nil, file)
	if err == nil || !strings.Contains(err.Error(), "has no matching value") {
		t.Fatalf("err = %v, want missing value error", err)
	}
}

func TestRegisterFromFilesRejectsAmbiguousRootValue(t *testing.T) {
	root := t.TempDir()
	file := writeTemplate(t, root, "page.gohtml", `{{/*
@model Page github.com/donseba/go-doc/renderer.testPage
*/}}`)

	err := RegisterFromFiles(template.New("page.gohtml"), []any{testPage{}, testPage{}}, file)
	if err == nil || !strings.Contains(err.Error(), "has no matching value") {
		t.Fatalf("err = %v, want unresolved value error", err)
	}
}

func TestRegisterFromLookupUsesCustomResolver(t *testing.T) {
	tmpl := template.New("page.gohtml")
	contracts := map[string]string{
		"Page": "github.com/example/app.Page",
	}
	lookup := RootLookupFunc(func(typeName string) any {
		if typeName == "github.com/example/app.Page" {
			return testPage{Title: "from lookup"}
		}
		return nil
	})

	if err := RegisterFromLookup(tmpl, contracts, lookup); err != nil {
		t.Fatal(err)
	}
	if _, err := tmpl.Parse(`{{ Page.Title }}`); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := tmpl.Execute(&out, nil); err != nil {
		t.Fatal(err)
	}
	if out.String() != "from lookup" {
		t.Fatalf("output = %q, want from lookup", out.String())
	}
}

func TestRegisterFromLookupRejectsMissingValue(t *testing.T) {
	tmpl := template.New("page.gohtml")
	contracts := map[string]string{
		"Page": "github.com/example/app.Page",
	}

	err := RegisterFromLookup(tmpl, contracts, func(string) any { return nil })
	if err == nil || !strings.Contains(err.Error(), "has no matching value") {
		t.Fatalf("err = %v, want missing value error", err)
	}
}

func TestRegisterFromLookupRejectsNilLookup(t *testing.T) {
	err := RegisterFromLookup(template.New("page.gohtml"), map[string]string{"Page": "github.com/example/app.Page"}, nil)
	if err == nil || !strings.Contains(err.Error(), "nil root lookup") {
		t.Fatalf("err = %v, want nil lookup error", err)
	}
}

func TestRegisterFromLookupRejectsReservedRootName(t *testing.T) {
	err := RegisterFromLookup(template.New("page.gohtml"), map[string]string{"len": "github.com/example/app.Page"}, func(string) any {
		return testPage{}
	})
	if err == nil || !strings.Contains(err.Error(), "invalid root name") {
		t.Fatalf("err = %v, want invalid root name error", err)
	}
}

func TestMatchingValuesAcceptsMainPackageRuntimeType(t *testing.T) {
	values := []rootValue{
		{fullName: "main.Page", shortName: "Page", value: testPage{Title: "from main"}},
	}

	matches := matchingValues("github.com/example/app.Page", values)
	if len(matches) != 1 {
		t.Fatalf("len(matches) = %d, want 1", len(matches))
	}
	if page, ok := matches[0].(testPage); !ok || page.Title != "from main" {
		t.Fatalf("match = %#v, want testPage", matches[0])
	}
}

func TestMatchingValuesDoesNotShortMatchNonMainPackages(t *testing.T) {
	values := []rootValue{
		{fullName: "github.com/other/app.Page", shortName: "Page", value: testPage{Title: "wrong"}},
	}

	matches := matchingValues("github.com/example/app.Page", values)
	if len(matches) != 0 {
		t.Fatalf("matches = %#v, want none", matches)
	}
}

func TestMatchingValuesDetectsAmbiguousMainPackageRuntimeTypes(t *testing.T) {
	values := []rootValue{
		{fullName: "main.Page", shortName: "Page", value: testPage{Title: "one"}},
		{fullName: "main.Page", shortName: "Page", value: testPage{Title: "two"}},
	}

	matches := matchingValues("github.com/example/app.Page", values)
	if len(matches) != 2 {
		t.Fatalf("len(matches) = %d, want 2", len(matches))
	}
}

func writeTemplate(t *testing.T, root, name, content string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}
