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

type Page struct {
	Items []Todo
}
`)
	writeFile(t, root, "templates/todos.gohtml", `{{/*
@param page Page
@var $todo Todo
*/}}
{{ range $todo := _page.Items }}{{ $todo.Title }}{{ end }}
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

	contract := idx.Templates["templates/todos.gohtml"]
	if contract.Params["page"] != "example.com/app.Page" {
		t.Fatalf("@param page = %q", contract.Params["page"])
	}
	if contract.Vars["$todo"] != "example.com/app.Todo" {
		t.Fatalf("@var $todo = %q", contract.Vars["$todo"])
	}
	if contract.Accessors["_page"] != "example.com/app.Page" {
		t.Fatalf("_page accessor = %q", contract.Accessors["_page"])
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
