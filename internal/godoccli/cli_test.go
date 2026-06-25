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
