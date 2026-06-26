package godoccli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildIndexScansTemplateContractsAndFieldMetadata(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/app\n\ngo 1.26\n")
	writeFile(t, root, "todo.go", `package app

// Todo is rendered by todo templates.
type Todo struct {
	// Title is the visible task title.
	Title string
	Done bool
	private string
}

// Label returns the template label.
func (t Todo) Label() string {
	return t.Title
}

type Page struct {
	Items []Todo
}

type User struct {
	Name string
}

// CurrentUser returns the active user.
func CurrentUser() User {
	return User{Name: "Ada"}
}

type privateState struct {
	Token string
}
`)
	writeFile(t, root, "templates/todos.gohtml", `{{/*
@model page Page
*/}}
{{ range $todo := page.Items }}{{ $todo.Title }}{{ end }}
`)

	idx, err := buildIndex(root)
	if err != nil {
		t.Fatalf("buildIndex() error = %v", err)
	}
	if len(idx.Problems) != 0 {
		t.Fatalf("expected no problems, got %#v", idx.Problems)
	}

	todoType := idx.Types["example.com/app.Todo"]
	if todoType.Name != "Todo" {
		t.Fatalf("Todo type missing from index: %#v", idx.Types)
	}
	if todoType.Doc == "" {
		t.Fatal("expected type doc")
	}
	title := todoType.Fields["Title"]
	if title.Type != "string" {
		t.Fatalf("Title type = %q, want string", title.Type)
	}
	if title.Doc == "" {
		t.Fatal("expected field doc")
	}
	if title.Line == 0 || title.Column == 0 || title.File == "" {
		t.Fatalf("expected field source position, got %#v", title)
	}
	if _, ok := todoType.Fields["private"]; ok {
		t.Fatal("unexported field should not be indexed")
	}
	method := todoType.Methods["Label"]
	if method.Type != "string" {
		t.Fatalf("Label return type = %q, want string", method.Type)
	}
	if method.Signature == "" || method.Doc == "" || method.Line == 0 {
		t.Fatalf("expected method metadata, got %#v", method)
	}
	if _, ok := idx.Types["example.com/app.User"]; !ok {
		t.Fatal("unused exported structs should stay indexed for @model completion")
	}
	fn := idx.Funcs["example.com/app.CurrentUser"]
	if fn.Result != "User" || fn.Signature == "" || fn.Doc == "" {
		t.Fatalf("expected function result metadata, got %#v", fn)
	}
	if _, ok := idx.Types["example.com/app.privateState"]; ok {
		t.Fatal("unexported structs should not be indexed")
	}

	contract := idx.Templates["templates/todos.gohtml"]
	if contract.Models["page"] != "example.com/app.Page" {
		t.Fatalf("@model page = %q", contract.Models["page"])
	}
}

func TestBuildIndexScansDotContract(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/app\n\ngo 1.26\n")
	writeFile(t, root, "user.go", `package app

type User struct {
	Name string
}
`)
	writeFile(t, root, "templates/user_row.gohtml", `{{/*
@dot User
*/}}
<td>{{ .Name }}</td>
`)

	idx, needed, err := buildTemplateIndex(root)
	if err != nil {
		t.Fatalf("buildTemplateIndex() error = %v", err)
	}
	if !needed {
		t.Fatal("index should be needed for @dot annotations")
	}
	contract := idx.Templates["templates/user_row.gohtml"]
	if contract.Dot != "example.com/app.User" {
		t.Fatalf("@dot = %q, want User", contract.Dot)
	}
}

func TestBuildIndexScansOneLineTemplateCommentContracts(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/app\n\ngo 1.26\n")
	writeFile(t, root, "user.go", `package app

type Page struct {
	Users []User
}

type User struct {
	Name string
}

func FirstUser() User {
	return User{}
}
`)
	writeFile(t, root, "templates/page.gohtml", `{{/* @model Page Page */}}
{{/* @func firstUser FirstUser */}}
{{ define "table_row" }}{{/* @dot User */}}<td>{{ .Name }}</td>{{ end }}`)

	idx, needed, err := buildTemplateIndex(root)
	if err != nil {
		t.Fatalf("buildTemplateIndex() error = %v", err)
	}
	if !needed {
		t.Fatal("index should be needed for one-line annotations")
	}
	page := idx.Templates["templates/page.gohtml"]
	if page.Models["Page"] != "example.com/app.Page" {
		t.Fatalf("Page model = %q", page.Models["Page"])
	}
	if page.Funcs["firstUser"] != "FirstUser" {
		t.Fatalf("firstUser func = %q", page.Funcs["firstUser"])
	}
	row := idx.Templates["templates/page.gohtml#table_row"]
	if row.Dot != "example.com/app.User" {
		t.Fatalf("row dot = %q", row.Dot)
	}
}

func TestBuildIndexScansNamedDefineContracts(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/app\n\ngo 1.26\n")
	writeFile(t, root, "user.go", `package app

type User struct {
	Name string
}
`)
	writeFile(t, root, "templates/rows.gohtml", `{{/*
@dot User
*/}}
{{ define "table_row" }}
<tr><td>{{ .Name }}</td></tr>
{{ end }}
`)

	idx, needed, err := buildTemplateIndex(root)
	if err != nil {
		t.Fatalf("buildTemplateIndex() error = %v", err)
	}
	if !needed {
		t.Fatal("index should be needed for @dot annotations in named defines")
	}
	contract := idx.Templates["templates/rows.gohtml#table_row"]
	if contract.Name != "table_row" {
		t.Fatalf("table_row name = %q", contract.Name)
	}
	if contract.Dot != "example.com/app.User" {
		t.Fatalf("table_row @dot = %q, want User", contract.Dot)
	}
	if contract.Source != "templates/rows.gohtml" || contract.Line != 4 || contract.Column != 1 {
		t.Fatalf("table_row source = %#v, want define source position", contract)
	}
}

func TestBuildIndexPrefersInsideDefineContract(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/app\n\ngo 1.26\n")
	writeFile(t, root, "user.go", `package app

type User struct {
	Name string
}

type Admin struct {
	Name string
}
`)
	writeFile(t, root, "templates/rows.gohtml", `{{/*
@dot User
*/}}
{{ define "table_row" }}
{{/*
@dot Admin
*/}}
<tr><td>{{ .Name }}</td></tr>
{{ end }}
`)

	idx, _, err := buildTemplateIndex(root)
	if err != nil {
		t.Fatalf("buildTemplateIndex() error = %v", err)
	}
	contract := idx.Templates["templates/rows.gohtml#table_row"]
	if contract.Dot != "example.com/app.Admin" {
		t.Fatalf("table_row @dot = %q, want inside define contract to win", contract.Dot)
	}
}

func TestBuildIndexUsesGoTypesForImportsAliasesAndGenerics(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/app\n\ngo 1.26\n")
	writeFile(t, root, "todo.go", `package app

import "time"

type Timestamp = time.Time

type Box[T any] struct {
	Value T
}

type Page struct {
	GeneratedAt Timestamp
	Names Box[string]
}

func Lookup() (Page, error) {
	return Page{}, nil
}
`)
	writeFile(t, root, "templates/page.gohtml", `{{/*
@model Page Page
@func lookup Lookup
*/}}
{{ Page.GeneratedAt.Format "15:04:05" }}`)

	idx, err := buildIndex(root)
	if err != nil {
		t.Fatalf("buildIndex() error = %v", err)
	}
	page := idx.Types["example.com/app.Page"]
	if page.Fields["GeneratedAt"].Type != "time.Time" {
		t.Fatalf("GeneratedAt type = %q, want time.Time", page.Fields["GeneratedAt"].Type)
	}
	timeType := idx.Types["time.Time"]
	format, ok := timeType.Methods["Format"]
	if !ok {
		t.Fatalf("time.Time methods = %#v, want Format method for imported named type", timeType.Methods)
	}
	if len(format.Params) != 1 || format.Params[0] != "string" {
		t.Fatalf("time.Time.Format params = %#v, want string", format.Params)
	}
	if page.Fields["Names"].Type != "Box[string]" {
		t.Fatalf("Names type = %q, want Box[string]", page.Fields["Names"].Type)
	}
	fn := idx.Funcs["example.com/app.Lookup"]
	if fn.Result != "Page" || len(fn.Results) != 2 || fn.Results[1] != "error" {
		t.Fatalf("Lookup metadata = %#v, want Page,error result metadata", fn)
	}
}

func TestBuildTemplateIndexRequiresParamContract(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/app\n\ngo 1.26\n")
	writeFile(t, root, "todo.go", `package app

type Todo struct {
	Title string
}
`)
	writeFile(t, root, "templates/no_contract.gohtml", `{{ .Title }}`)

	idx, needed, err := buildTemplateIndex(root)
	if err != nil {
		t.Fatalf("buildTemplateIndex() error = %v", err)
	}
	if needed {
		t.Fatal("index should not be needed without at least one @model annotation")
	}
	if len(idx.Types) != 0 {
		t.Fatalf("types should not be scanned when index is not needed, got %#v", idx.Types)
	}
}

func TestBuildIndexUsesConfigIncludeExclude(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/app\n\ngo 1.26\n")
	writeFile(t, root, ".go-doc/config.json", `{
  "include": ["/"],
  "exclude": ["vendor", "internal/secret"]
}`)
	writeFile(t, root, "public.go", `package app

type Public struct {
	Title string
}
`)
	writeFile(t, root, "vendor/vendor.go", `package vendor

type VendorType struct {
	Title string
}
`)
	writeFile(t, root, "internal/secret/secret.go", `package secret

type Secret struct {
	Title string
}
`)
	writeFile(t, root, "templates/public.gohtml", `{{/*
@model public Public
*/}}
{{ public.Title }}`)

	idx, err := buildIndex(root)
	if err != nil {
		t.Fatalf("buildIndex() error = %v", err)
	}
	if _, ok := idx.Types["example.com/app.Public"]; !ok {
		t.Fatal("public type should be indexed")
	}
	if _, ok := idx.Types["example.com/app/vendor.VendorType"]; ok {
		t.Fatal("vendor type should be excluded")
	}
	if _, ok := idx.Types["example.com/app/internal/secret.Secret"]; ok {
		t.Fatal("configured excluded type should be excluded")
	}
}

func TestBuildTemplateIndexHonorsDisabledConfig(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/app\n\ngo 1.26\n")
	writeFile(t, root, ".go-doc/config.json", `{
  "enabled": false,
  "functions": {
    "asset": "example.com/app.Asset"
  }
}`)
	writeFile(t, root, "page.go", `package app

type Page struct {
	Title string
}

func Asset(path string) string {
	return path
}
`)
	writeFile(t, root, "templates/page.gohtml", `{{/*
@model Page example.com/app.Page
*/}}
{{ Page.Title }}`)

	idx, needed, err := buildTemplateIndex(root)
	if err != nil {
		t.Fatalf("buildTemplateIndex() error = %v", err)
	}
	if needed {
		t.Fatal("disabled config should not require an index")
	}
	if len(idx.Templates) != 0 || len(idx.Types) != 0 || len(idx.Funcs) != 0 {
		t.Fatalf("disabled config should not scan project, got templates=%d types=%d funcs=%d", len(idx.Templates), len(idx.Types), len(idx.Funcs))
	}
}

func TestBuildIndexUsesConfigDefaultFunctions(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/app\n\ngo 1.26\n")
	writeFile(t, root, ".go-doc/config.json", `{
  "functions": {
    "asset": "example.com/app.Asset"
  }
}`)
	writeFile(t, root, "page.go", `package app

type Page struct {
	Title string
}

func Asset(path string) string {
	return "/assets/" + path
}
`)
	writeFile(t, root, "templates/page.gohtml", `{{/*
@model page Page
*/}}
<link rel="stylesheet" href="{{ asset "app.css" }}">
{{ page.Title }}`)

	idx, err := buildIndex(root)
	if err != nil {
		t.Fatalf("buildIndex() error = %v", err)
	}
	tmpl := idx.Templates["templates/page.gohtml"]
	if got := tmpl.Funcs["asset"]; got != "example.com/app.Asset" {
		t.Fatalf("default asset func = %q", got)
	}
	if len(idx.Problems) != 0 {
		t.Fatalf("unexpected problems: %#v", idx.Problems)
	}
}

func TestBuildIndexProjectsGenNamespace(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/app\n\ngo 1.26\n")
	writeFile(t, root, "page.go", `package app

import "time"

type Page struct {
	GeneratedAt time.Time
}
`)
	writeFile(t, root, "viewfuncs/viewfuncs.go", `package viewfuncs

import "time"

// FormatTime formats t.
func FormatTime(t time.Time, layout string) string {
	return t.Format(layout)
}
`)
	writeFile(t, root, "templates/page.gohtml", `{{/*
@model Page Page
@gen view example.com/app/viewfuncs
*/}}
{{ view.FormatTime Page.GeneratedAt "15:04:05" }}`)

	idx, err := buildIndex(root)
	if err != nil {
		t.Fatalf("buildIndex() error = %v", err)
	}
	if len(idx.Problems) != 0 {
		t.Fatalf("unexpected problems: %#v", idx.Problems)
	}
	tmpl := idx.Templates["templates/page.gohtml"]
	if tmpl.Gens["view"] != "example.com/app/viewfuncs" {
		t.Fatalf("@gen view = %q", tmpl.Gens["view"])
	}
	genType := tmpl.Models["view"]
	if genType == "" {
		t.Fatalf("@gen namespace was not projected into models: %#v", tmpl)
	}
	viewType := idx.Types[genType]
	if viewType.File != "viewfuncs/viewfuncs.go" || viewType.Line != 1 || viewType.Column != 1 {
		t.Fatalf("generated namespace target = %#v, want source package file anchor", viewType)
	}
	if viewType.Methods["FormatTime"].Type != "string" {
		t.Fatalf("FormatTime method = %#v", viewType.Methods["FormatTime"])
	}
}

func TestBuildIndexLetsLocalFunctionOverrideConfigDefault(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/app\n\ngo 1.26\n")
	writeFile(t, root, ".go-doc/config.json", `{
  "functions": {
    "format": "example.com/app.GlobalFormat"
  }
}`)
	writeFile(t, root, "format.go", `package app

type Page struct {
	Title string
}

func GlobalFormat(value string) string {
	return value
}

func LocalFormat(value string) string {
	return value
}
`)
	writeFile(t, root, "templates/page.gohtml", `{{/*
@model page Page
@func format example.com/app.LocalFormat
*/}}
{{ format page.Title }}`)

	idx, err := buildIndex(root)
	if err != nil {
		t.Fatalf("buildIndex() error = %v", err)
	}
	tmpl := idx.Templates["templates/page.gohtml"]
	if got := tmpl.Funcs["format"]; got != "example.com/app.LocalFormat" {
		t.Fatalf("local format func = %q", got)
	}
	if len(idx.Problems) != 0 {
		t.Fatalf("unexpected problems: %#v", idx.Problems)
	}
}

func TestBuildTemplateIndexUsesConfigFunctionWithoutModelContract(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/app\n\ngo 1.26\n")
	writeFile(t, root, ".go-doc/config.json", `{
  "functions": {
    "asset": "example.com/app.Asset"
  }
}`)
	writeFile(t, root, "assets.go", `package app

func Asset(path string) string {
	return "/assets/" + path
}
`)
	writeFile(t, root, "templates/layout.gohtml", `<script src="{{ asset "app.js" }}"></script>`)

	idx, needed, err := buildTemplateIndex(root)
	if err != nil {
		t.Fatalf("buildTemplateIndex() error = %v", err)
	}
	if !needed {
		t.Fatal("index should be needed when config declares default functions")
	}
	tmpl := idx.Templates["templates/layout.gohtml"]
	if got := tmpl.Funcs["asset"]; got != "example.com/app.Asset" {
		t.Fatalf("default asset func = %q", got)
	}
}

func TestPathMatchesNormalizesWindowsSeparators(t *testing.T) {
	tests := []struct {
		relative string
		pattern  string
		want     bool
	}{
		{relative: "internal/secret/model.go", pattern: `internal\secret`, want: true},
		{relative: `internal\secret\model.go`, pattern: "internal/secret", want: true},
		{relative: "templates/main.gohtml", pattern: "/", want: true},
		{relative: "templates/main.gohtml", pattern: "vendor", want: false},
	}
	for _, test := range tests {
		if got := pathMatches(test.relative, test.pattern); got != test.want {
			t.Fatalf("pathMatches(%q, %q) = %v, want %v", test.relative, test.pattern, got, test.want)
		}
	}
}

func TestBuildIndexSkipsNestedModules(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/app\n\ngo 1.26\n")
	writeFile(t, root, "page.go", `package app

type Page struct {
	Title string
}
`)
	writeFile(t, root, "templates/page.gohtml", `{{/*
@model page Page
*/}}
{{ page.Title }}`)
	writeFile(t, root, "examples/todo/go.mod", "module example.com/app/examples/todo\n\ngo 1.26\n")
	writeFile(t, root, "examples/todo/todo.go", `package main

type Todo struct {
	Title string
}
`)
	writeFile(t, root, "examples/todo/templates/todo.gohtml", `{{/*
@model todo github.com/example/app/examples/todo.Todo
*/}}
{{ todo.Title }}`)

	idx, err := buildIndex(root)
	if err != nil {
		t.Fatalf("buildIndex() error = %v", err)
	}
	if _, ok := idx.Templates["templates/page.gohtml"]; !ok {
		t.Fatal("root template should be indexed")
	}
	if _, ok := idx.Templates["examples/todo/templates/todo.gohtml"]; ok {
		t.Fatal("nested module template should not be indexed by parent module")
	}
	if _, ok := idx.Types["example.com/app/examples/todo.Todo"]; ok {
		t.Fatal("nested module type should not be indexed by parent module")
	}
}

func TestIndexCommandRemovesStaleOutputWhenNoParamContractExists(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/app\n\ngo 1.26\n")
	writeFile(t, root, "templates/no_contract.gohtml", `{{ .Title }}`)
	out := filepath.Join(root, ".go-doc", "index.json")
	writeFile(t, root, ".go-doc/index.json", `{"stale": true}`)

	if err := Run([]string{"index", "-o", out, root}); err != nil {
		t.Fatalf("Run(index) error = %v", err)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("expected stale index to be removed, stat err = %v", err)
	}
}

func TestWriteJSONWritesOutputFile(t *testing.T) {
	out := filepath.Join(t.TempDir(), ".go-doc", "index.json")
	if err := writeJSON(indexFile{Version: 2, Module: "example.com/app"}, out); err != nil {
		t.Fatalf("writeJSON() error = %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if len(data) == 0 || data[0] != '{' {
		t.Fatalf("expected utf-8 json object, got %q", data)
	}
}

func writeFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
