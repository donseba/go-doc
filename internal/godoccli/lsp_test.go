package godoccli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLSPDiagnosticsCatchUnknownFieldAndInvalidRange(t *testing.T) {
	idx := lspIndex{indexFile: indexFile{
		Types: map[string]goTypeIndex{
			"example.com/app.Page": {
				Name: "Page",
				Fields: map[string]fieldIndex{
					"Title": {Type: "string"},
					"Items": {Type: "[]Todo"},
				},
			},
			"example.com/app.Todo": {
				Name: "Todo",
				Fields: map[string]fieldIndex{
					"ID":    {Type: "int"},
					"Title": {Type: "string"},
				},
			},
		},
		Short: map[string][]string{
			"Page": {"example.com/app.Page"},
			"Todo": {"example.com/app.Todo"},
		},
	}}
	contract := templateIndex{
		Models: map[string]string{"page": "example.com/app.Page"},
	}
	text := `{{/*
@model bad Missing
*/}}
{{ _missing.Title }}
{{ page.Titel }}
{{ range page.Title }}{{ .ID }}{{ end }}
{{ range page.Items }}{{ .Nope }}{{ end }}`

	diagnostics := diagnosticsForText(text, idx, contract)
	if len(diagnostics) != 5 {
		t.Fatalf("len(diagnostics) = %d, want 5: %#v", len(diagnostics), diagnostics)
	}
	assertDiagnostic(t, diagnostics, "Unknown go-doc model type 'Missing'")
	assertDiagnostic(t, diagnostics, "Unknown go-doc accessor '_missing'")
	assertDiagnostic(t, diagnostics, "Unknown field 'Titel' on Page")
	assertDiagnostic(t, diagnostics, "Cannot range over 'page.Title' because it is string")
	assertDiagnostic(t, diagnostics, "Unknown field 'Nope' on Todo")
}

func TestLSPDiagnosticsIgnoreModelDeclarationAsTemplateCode(t *testing.T) {
	idx := lspIndex{indexFile: indexFile{
		Types: map[string]goTypeIndex{
			"github.com/donseba/go-doc/examples/todo.Todo": {
				Name: "Todo",
				Fields: map[string]fieldIndex{
					"Title": {Type: "string"},
				},
			},
		},
		Short: map[string][]string{"Todo": {"github.com/donseba/go-doc/examples/todo.Todo"}},
	}}
	contract := templateIndex{
		Models: map[string]string{"todo": "github.com/donseba/go-doc/examples/todo.Todo"},
	}
	text := `{{/*
@model todo github.com/donseba/go-doc/examples/todo.Todo
*/}}
{{ todo.Title }}`

	diagnostics := diagnosticsForText(text, idx, contract)
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want none", diagnostics)
	}
}

func TestLSPDiagnosticsWarnsWhenLenCannotApply(t *testing.T) {
	idx := lspIndex{indexFile: indexFile{
		Types: map[string]goTypeIndex{
			"example.com/app.Page": {
				Name: "Page",
				Fields: map[string]fieldIndex{
					"GeneratedAt": {Type: "time.Time"},
					"Title":       {Type: "string"},
				},
			},
		},
		Short: map[string][]string{"Page": {"example.com/app.Page"}},
	}}
	contract := templateIndex{
		Models: map[string]string{"Page": "example.com/app.Page"},
	}

	diagnostics := diagnosticsForText(`{{ len Page.GeneratedAt }}
{{ len Page.Title }}`, idx, contract)
	assertDiagnostic(t, diagnostics, "Cannot call len on 'Page.GeneratedAt' because it is time.Time")
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v, want only time.Time len diagnostic", diagnostics)
	}
}

func TestLSPCodeActionAddsMissingField(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n")
	writeTestFile(t, filepath.Join(root, "todo.go"), `package app

type Todo struct {
	Title string
}
`)
	idx := indexFile{
		Version: 2,
		Module:  "example.com/app",
		Templates: map[string]templateIndex{
			"todo.gohtml": {
				Models: map[string]string{"todo": "example.com/app.Todo"},
			},
		},
		Types: map[string]goTypeIndex{
			"example.com/app.Todo": {
				Name: "Todo",
				File: "todo.go",
				Fields: map[string]fieldIndex{
					"Title": {Type: "string"},
				},
			},
		},
		Short: map[string][]string{"Todo": {"example.com/app.Todo"}},
	}
	if err := writeJSON(idx, filepath.Join(root, ".go-doc", "index.json")); err != nil {
		t.Fatalf("writeJSON() error = %v", err)
	}
	templateText := `{{ todo.DueLabel }}`
	uri := uriFromPath(filepath.Join(root, "todo.gohtml"))
	server := &lspServer{
		root: root,
		idx:  idx,
		docs: map[string]string{uri: templateText},
	}

	actions := server.codeActions(codeActionParams{
		TextDocument: textDocumentIdentifier{URI: uri},
		Range:        rangeFromOffsets(templateText, strings.Index(templateText, "DueLabel"), strings.Index(templateText, "DueLabel")+len("DueLabel")),
	})
	if len(actions) != 1 {
		t.Fatalf("len(actions) = %d, want 1: %#v", len(actions), actions)
	}
	if actions[0].Command == nil || actions[0].Command.Command != "goDoc.missingFieldApplied" {
		t.Fatalf("action command = %#v, want goDoc.missingFieldApplied", actions[0].Command)
	}
	args, ok := actions[0].Command.Arguments[0].(missingFieldAppliedCommand)
	if !ok {
		t.Fatalf("command arguments = %#v, want missingFieldAppliedCommand", actions[0].Command.Arguments)
	}
	if args.TargetURI != uriFromPath(filepath.Join(root, "todo.go")) || args.NewText != "\n\tDueLabel string" {
		t.Fatalf("command edit = %#v, want missing string field edit", args)
	}
}

func TestLSPCodeActionMissingFieldRefreshesInMemoryIndex(t *testing.T) {
	root := t.TempDir()
	idx := indexFile{
		Version: 2,
		Module:  "example.com/app",
		Templates: map[string]templateIndex{
			"todo.gohtml": {
				Models: map[string]string{"todo": "example.com/app.Todo"},
			},
		},
		Types: map[string]goTypeIndex{
			"example.com/app.Todo": {
				Name: "Todo",
				File: "todo.go",
				Fields: map[string]fieldIndex{
					"Title": {Type: "string"},
				},
			},
		},
		Short: map[string][]string{"Todo": {"example.com/app.Todo"}},
	}
	templateText := `{{ todo.DueLabel }}`
	uri := uriFromPath(filepath.Join(root, "todo.gohtml"))
	var out bytes.Buffer
	server := &lspServer{
		root: root,
		idx:  idx,
		docs: map[string]string{uri: templateText},
		out:  &out,
	}
	if diagnostics := diagnosticsForText(templateText, server.index(), idx.Templates["todo.gohtml"]); len(diagnostics) != 1 {
		t.Fatalf("diagnostics before command = %#v, want one", diagnostics)
	}

	args, err := json.Marshal([]missingFieldAppliedCommand{{
		Root:      root,
		OwnerType: "example.com/app.Todo",
		FieldName: "DueLabel",
		FieldType: "string",
		TargetURI: uriFromPath(filepath.Join(root, "todo.go")),
		Range:     lspRange{},
		NewText:   "\n\tDueLabel string",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if rpcErr := server.executeCommand(executeCommandParams{Command: "goDoc.missingFieldApplied", Arguments: args}); rpcErr != nil {
		t.Fatalf("executeCommand() error = %#v", rpcErr)
	}
	output := out.String()
	if strings.Count(output, `"method":"textDocument/publishDiagnostics"`) < 2 {
		t.Fatalf("executeCommand output = %q, want clear and republish diagnostics", output)
	}
	if !strings.Contains(output, `"diagnostics":[]`) {
		t.Fatalf("executeCommand output = %q, want explicit diagnostic clear", output)
	}
	if diagnostics := diagnosticsForText(templateText, server.index(), idx.Templates["todo.gohtml"]); len(diagnostics) != 0 {
		t.Fatalf("diagnostics after command = %#v, want none", diagnostics)
	}
}

func TestLSPCodeActionCreatesMissingModel(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n")
	writeTestFile(t, filepath.Join(root, "handler.go"), "package app\n")
	idx := indexFile{
		Version:   2,
		Module:    "example.com/app",
		Templates: map[string]templateIndex{},
		Types:     map[string]goTypeIndex{},
		Short:     map[string][]string{},
	}
	if err := writeJSON(idx, filepath.Join(root, ".go-doc", "index.json")); err != nil {
		t.Fatalf("writeJSON() error = %v", err)
	}
	templateText := `{{/*
@model page example.com/app.Page
*/}}`
	uri := uriFromPath(filepath.Join(root, "page.gohtml"))
	server := &lspServer{
		root: root,
		idx:  idx,
		docs: map[string]string{uri: templateText},
	}

	actions := server.codeActions(codeActionParams{
		TextDocument: textDocumentIdentifier{URI: uri},
		Range:        rangeFromOffsets(templateText, strings.Index(templateText, "example.com"), strings.Index(templateText, "Page")+len("Page")),
	})
	if len(actions) != 1 {
		t.Fatalf("len(actions) = %d, want 1: %#v", len(actions), actions)
	}
	edit := actions[0].Edit.Changes[uriFromPath(filepath.Join(root, "go_doc_models.go"))][0]
	if !strings.Contains(edit.NewText, "package app") || !strings.Contains(edit.NewText, "type Page struct") {
		t.Fatalf("edit.NewText = %q, want Page model file", edit.NewText)
	}
}

func TestLSPCompletionUsesRangeDotType(t *testing.T) {
	idx := lspIndex{indexFile: indexFile{
		Types: map[string]goTypeIndex{
			"example.com/app.Page": {
				Name: "Page",
				Fields: map[string]fieldIndex{
					"Items": {Type: "[]Todo"},
				},
			},
			"example.com/app.Todo": {
				Name: "Todo",
				Fields: map[string]fieldIndex{
					"ID":       {Type: "int"},
					"Priority": {Type: "string"},
					"Title":    {Type: "string", Doc: "visible title"},
				},
			},
		},
		Short: map[string][]string{
			"Todo": {"example.com/app.Todo"},
		},
	}}
	contract := templateIndex{
		Models: map[string]string{"page": "example.com/app.Page"},
	}
	text := `{{ range page.Items }}{{ . }}{{ end }}`
	offset := len(`{{ range page.Items }}{{ .`)

	target, ok := fieldTargetBeforeCaret(text, offset, idx, contract)
	if !ok || target != "example.com/app.Todo" {
		t.Fatalf("fieldTargetBeforeCaret() = %q, %v", target, ok)
	}
}

func TestLSPCompletionOffersModelsInEmptyAction(t *testing.T) {
	idx := lspIndex{indexFile: indexFile{
		Types: map[string]goTypeIndex{
			"example.com/app.Todo": {Name: "Todo"},
		},
	}}
	contract := templateIndex{
		Models: map[string]string{"todo": "example.com/app.Todo"},
	}
	text := `{{  }}`
	offset := len(`{{ `)

	prefix, ok := accessorPrefixBeforeCaret(text, offset)
	if !ok || prefix != "" {
		t.Fatalf("accessorPrefixBeforeCaret() = %q, %v; want empty prefix", prefix, ok)
	}
	items := accessorCompletionItems(idx, contract, prefix)
	if !hasCompletionLabel(items, "todo") || !hasCompletionLabel(items, "len") {
		t.Fatalf("items = %#v, want todo", items)
	}
}

func TestWriteRPCMessageIncludesNullResult(t *testing.T) {
	var out bytes.Buffer
	err := writeRPCMessage(&out, rpcMessage{
		JSONRPC: "2.0",
		ID:      []byte(`1`),
		Result:  nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"result":null`) {
		t.Fatalf("response = %q, want explicit null result", out.String())
	}
	if strings.Contains(out.String(), `"error"`) {
		t.Fatalf("response = %q, did not expect error property", out.String())
	}
}

func TestLSPDiagnosticsUseRangeDotType(t *testing.T) {
	idx := lspIndex{indexFile: indexFile{
		Types: map[string]goTypeIndex{
			"example.com/app.Page": {
				Name: "Page",
				Fields: map[string]fieldIndex{
					"Projects": {Type: "[]Project"},
				},
			},
			"example.com/app.Project": {
				Name: "Project",
				Fields: map[string]fieldIndex{
					"ID":     {Type: "int"},
					"Owner":  {Type: "User"},
					"Status": {Type: "string"},
				},
				Methods: map[string]methodIndex{
					"Label": {Type: "string", Signature: "func() string"},
				},
			},
			"example.com/app.User": {
				Name: "User",
				Fields: map[string]fieldIndex{
					"Name": {Type: "string"},
				},
			},
		},
		Short: map[string][]string{
			"Page":    {"example.com/app.Page"},
			"Project": {"example.com/app.Project"},
			"User":    {"example.com/app.User"},
		},
	}}
	contract := templateIndex{
		Models: map[string]string{"page": "example.com/app.Page"},
	}
	text := `{{ range page.Projects }}
{{ .ID }}
<a href="/projects/{{ .ID }}">{{ .Label }}</a>
<span>{{ .Status }}</span>
<small>{{ .Owner.Name }}</small>
{{ end }}`

	diagnostics := diagnosticsForText(text, idx, contract)
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want none", diagnostics)
	}
}

func TestLSPDiagnosticsResolveSlashPackageModelDeclaration(t *testing.T) {
	idx := lspIndex{indexFile: indexFile{
		Types: map[string]goTypeIndex{
			"github.com/donseba/go-doc/examples/todo.TodoPage": {
				Name:   "TodoPage",
				Fields: map[string]fieldIndex{"Title": {Type: "string"}},
			},
		},
		Short: map[string][]string{
			"TodoPage": {"github.com/donseba/go-doc/examples/todo.TodoPage"},
		},
	}}
	contract := templateIndex{
		Models: map[string]string{"page": "github.com/donseba/go-doc/examples/todo.TodoPage"},
	}
	text := `{{/*
@model page github.com/donseba/go-doc/examples/todo.TodoPage
*/}}
{{ page.Title }}`

	diagnostics := diagnosticsForText(text, idx, contract)
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want none", diagnostics)
	}
}

func TestLSPIndexForURIUsesNearestNestedIndex(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "examples", "todo")
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/root\n")
	writeTestFile(t, filepath.Join(nested, "go.mod"), "module github.com/donseba/go-doc/examples/todo\n")
	writeTestFile(t, filepath.Join(nested, "templates", "main.gohtml"), "")
	if err := writeJSON(indexFile{
		Version: 2,
		Module:  "github.com/donseba/go-doc/examples/todo",
		Templates: map[string]templateIndex{
			"templates/main.gohtml": {
				Models: map[string]string{"page": "github.com/donseba/go-doc/examples/todo.TodoPage"},
			},
		},
		Types: map[string]goTypeIndex{
			"github.com/donseba/go-doc/examples/todo.TodoPage": {Name: "TodoPage", Fields: map[string]fieldIndex{"Title": {Type: "string"}}},
		},
		Short: map[string][]string{"TodoPage": {"github.com/donseba/go-doc/examples/todo.TodoPage"}},
	}, filepath.Join(nested, ".go-doc", "index.json")); err != nil {
		t.Fatalf("writeJSON() error = %v", err)
	}
	server := &lspServer{
		root:    root,
		idx:     indexFile{Version: 2, Module: "example.com/root", Templates: map[string]templateIndex{}, Types: map[string]goTypeIndex{}, Short: map[string][]string{}},
		indexes: make(map[string]cachedLSPIndex),
	}

	idx := server.indexForURI(uriFromPath(filepath.Join(nested, "templates", "main.gohtml")))
	if idx.rootPath != nested {
		t.Fatalf("rootPath = %q, want %q", idx.rootPath, nested)
	}
	if _, ok := idx.Types["github.com/donseba/go-doc/examples/todo.TodoPage"]; !ok {
		t.Fatalf("nested type not loaded: %#v", idx.Types)
	}
}

func TestLSPDiagnosticsUseNestedIndexForDocument(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "examples", "todo")
	templatePath := filepath.Join(nested, "templates", "main.gohtml")
	text := `{{/*
@model page github.com/donseba/go-doc/examples/todo.TodoPage
*/}}
{{ page.Title }}`
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/root\n")
	writeTestFile(t, filepath.Join(nested, "go.mod"), "module github.com/donseba/go-doc/examples/todo\n")
	writeTestFile(t, templatePath, text)
	if err := writeJSON(indexFile{
		Version: 2,
		Module:  "github.com/donseba/go-doc/examples/todo",
		Templates: map[string]templateIndex{
			"templates/main.gohtml": {
				Models: map[string]string{"page": "github.com/donseba/go-doc/examples/todo.TodoPage"},
			},
		},
		Types: map[string]goTypeIndex{
			"github.com/donseba/go-doc/examples/todo.TodoPage": {Name: "TodoPage", Fields: map[string]fieldIndex{"Title": {Type: "string"}}},
		},
		Short: map[string][]string{"TodoPage": {"github.com/donseba/go-doc/examples/todo.TodoPage"}},
	}, filepath.Join(nested, ".go-doc", "index.json")); err != nil {
		t.Fatalf("writeJSON() error = %v", err)
	}
	server := &lspServer{
		root:    root,
		idx:     indexFile{Version: 2, Module: "example.com/root", Templates: map[string]templateIndex{}, Types: map[string]goTypeIndex{}, Short: map[string][]string{}},
		indexes: make(map[string]cachedLSPIndex),
		docs:    map[string]string{uriFromPath(templatePath): text},
	}

	idx := server.indexForURI(uriFromPath(templatePath))
	contract, ok := server.contractForURI(uriFromPath(templatePath), idx)
	if !ok {
		t.Fatal("contractForURI() = false, want true")
	}
	if diagnostics := diagnosticsForText(text, idx, contract); len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want none", diagnostics)
	}
}

func TestLSPDiagnosticsRebuildWhenSourceNewerThanIndex(t *testing.T) {
	root := t.TempDir()
	templatePath := filepath.Join(root, "templates", "todo.gohtml")
	indexPath := filepath.Join(root, ".go-doc", "index.json")
	text := `{{/*
@model todo example.com/app.Todo
*/}}
{{ todo.Un }}`
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n")
	writeTestFile(t, filepath.Join(root, "todo.go"), `package app

type Todo struct {
	Title string
	Un    string
}
`)
	writeTestFile(t, templatePath, text)
	if err := writeJSON(indexFile{
		Version: 2,
		Module:  "example.com/app",
		Templates: map[string]templateIndex{
			"templates/todo.gohtml": {
				Models: map[string]string{"todo": "example.com/app.Todo"},
			},
		},
		Types: map[string]goTypeIndex{
			"example.com/app.Todo": {Name: "Todo", Fields: map[string]fieldIndex{"Title": {Type: "string"}}},
		},
		Short: map[string][]string{"Todo": {"example.com/app.Todo"}},
	}, indexPath); err != nil {
		t.Fatalf("writeJSON() error = %v", err)
	}
	oldTime := time.Now().Add(-2 * time.Hour)
	newTime := time.Now().Add(-1 * time.Hour)
	if err := os.Chtimes(indexPath, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes(index) error = %v", err)
	}
	if err := os.Chtimes(filepath.Join(root, "todo.go"), newTime, newTime); err != nil {
		t.Fatalf("Chtimes(go) error = %v", err)
	}

	server := &lspServer{
		root:    root,
		idx:     indexFile{Version: 2, Module: "example.com/app", Templates: map[string]templateIndex{}, Types: map[string]goTypeIndex{}, Short: map[string][]string{}},
		indexes: make(map[string]cachedLSPIndex),
		docs:    map[string]string{uriFromPath(templatePath): text},
	}

	idx := server.indexForURI(uriFromPath(templatePath))
	contract, ok := server.contractForURI(uriFromPath(templatePath), idx)
	if !ok {
		t.Fatal("contractForURI() = false, want true")
	}
	if _, ok := idx.Types["example.com/app.Todo"].Fields["Un"]; !ok {
		t.Fatalf("rebuilt index does not include Un: %#v", idx.Types["example.com/app.Todo"].Fields)
	}
	if diagnostics := diagnosticsForText(text, idx, contract); len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want none", diagnostics)
	}
}

func TestLSPDiagnosticsRecoverAfterRangeSourceEdit(t *testing.T) {
	idx := lspIndex{indexFile: indexFile{
		Types: map[string]goTypeIndex{
			"example.com/app.Page": {
				Name: "Page",
				Fields: map[string]fieldIndex{
					"GeneratedAt": {Type: "time.Time"},
					"Projects":    {Type: "[]Project"},
				},
			},
			"example.com/app.Project": {
				Name: "Project",
				Fields: map[string]fieldIndex{
					"ID":     {Type: "int"},
					"Owner":  {Type: "User"},
					"Status": {Type: "string"},
				},
				Methods: map[string]methodIndex{
					"Label": {Type: "string", Signature: "func() string"},
				},
			},
			"example.com/app.User": {
				Name:   "User",
				Fields: map[string]fieldIndex{"Name": {Type: "string"}},
			},
		},
		Short: map[string][]string{
			"Page":    {"example.com/app.Page"},
			"Project": {"example.com/app.Project"},
			"User":    {"example.com/app.User"},
		},
	}}
	contract := templateIndex{
		Models: map[string]string{"page": "example.com/app.Page"},
	}
	text := `{{ range page.Projects }}
{{ .ID }}
<a href="/projects/{{ .ID }}">{{ .Label }}</a>
<span>{{ .Status }}</span>
<small>{{ .Owner.Name }}</small>
{{ end }}`

	invalid := strings.Replace(text, "Projects", "GeneratedAt", 1)
	if diagnostics := diagnosticsForText(invalid, idx, contract); len(diagnostics) == 0 {
		t.Fatal("expected invalid range diagnostic after switching to GeneratedAt")
	}

	current := text
	start := strings.Index(current, "Projects")
	current = applyTextChange(current, &lspRange{
		Start: positionAt(current, start),
		End:   positionAt(current, start+len("Projects")),
	}, "GeneratedAt")
	start = strings.Index(current, "GeneratedAt")
	current = applyTextChange(current, &lspRange{
		Start: positionAt(current, start),
		End:   positionAt(current, start+len("GeneratedAt")),
	}, "Projects")

	if current != text {
		t.Fatalf("current text did not recover:\n%s", current)
	}
	diagnostics := diagnosticsForText(current, idx, contract)
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want none", diagnostics)
	}
}

func TestLSPCompletionUsesPointerSliceMapAndMethods(t *testing.T) {
	idx := lspIndex{indexFile: indexFile{
		Types: map[string]goTypeIndex{
			"example.com/app.Page": {
				Name: "Page",
				Fields: map[string]fieldIndex{
					"Items": {Type: "[]*Todo"},
					"ByID":  {Type: "map[int]*Todo"},
				},
			},
			"example.com/app.Todo": {
				Name: "Todo",
				Fields: map[string]fieldIndex{
					"Title": {Type: "string"},
				},
				Methods: map[string]methodIndex{
					"Label": {Type: "string", Signature: "func() string"},
				},
			},
		},
		Short: map[string][]string{
			"Todo": {"example.com/app.Todo"},
		},
	}}
	contract := templateIndex{
		Models: map[string]string{"page": "example.com/app.Page"},
	}
	text := `{{ range page.Items }}{{ . }}{{ end }}`
	offset := len(`{{ range page.Items }}{{ .`)

	target, ok := fieldTargetBeforeCaret(text, offset, idx, contract)
	if !ok || target != "example.com/app.Todo" {
		t.Fatalf("fieldTargetBeforeCaret(slice) = %q, %v", target, ok)
	}

	text = `{{ range page.ByID }}{{ . }}{{ end }}`
	offset = len(`{{ range page.ByID }}{{ .`)
	target, ok = fieldTargetBeforeCaret(text, offset, idx, contract)
	if !ok || target != "example.com/app.Todo" {
		t.Fatalf("fieldTargetBeforeCaret(map) = %q, %v", target, ok)
	}

	ref, ok := fieldReferenceAt(`{{ page.Items }}`, len(`{{ page.Items`), idx, contract)
	if !ok || ref.fieldName != "Items" {
		t.Fatalf("fieldReferenceAt(field) = %#v, %v", ref, ok)
	}
	valueType := resolveFieldValuePath(idx, "example.com/app.Todo", []string{"Label"})
	if valueType != "string" {
		t.Fatalf("method return type = %q, want string", valueType)
	}
}

func TestLSPCompletesModelsAndModelTypes(t *testing.T) {
	idx := indexFile{
		Types: map[string]goTypeIndex{
			"example.com/app.Page": {Name: "Page", Package: "example.com/app", Fields: map[string]fieldIndex{}},
		},
		Short: map[string][]string{"Page": {"example.com/app.Page"}},
		Templates: map[string]templateIndex{
			"template.gohtml": {
				Models: map[string]string{"page": "example.com/app.Page"},
			},
		},
	}
	server := &lspServer{root: ".", idx: idx, docs: map[string]string{"file:///template.gohtml": `{{ pa }}`}}
	items := server.completions(textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: "file:///template.gohtml"},
		Position:     position{Line: 0, Character: len(`{{ pa`)},
	})
	if len(items) != 1 || items[0].Label != "page" || items[0].InsertText != "page." {
		t.Fatalf("accessor completions = %#v", items)
	}

	server.docs["file:///template.gohtml"] = `{{/*
@model page 
*/}}`
	items = server.completions(textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: "file:///template.gohtml"},
		Position:     position{Line: 1, Character: len(`@model page `)},
	})
	if len(items) != 1 || items[0].Label != "example.com/app.Page" {
		t.Fatalf("model completions = %#v", items)
	}
}

func TestLSPSemanticTokensHighlightModelAccessorAndField(t *testing.T) {
	idx := lspIndex{indexFile: indexFile{
		Types: map[string]goTypeIndex{
			"example.com/app.Page": {
				Name: "Page",
				Fields: map[string]fieldIndex{
					"Title": {Type: "string"},
				},
			},
		},
		Short: map[string][]string{"Page": {"example.com/app.Page"}},
	}}
	contract := templateIndex{
		Models: map[string]string{"page": "example.com/app.Page"},
	}
	text := `{{/*
@model page example.com/app.Page
*/}}
{{ page.Title }}`

	tokens := semanticTokensForText(text, idx, contract)
	if len(tokens) != 3 {
		t.Fatalf("len(tokens) = %d, want 3: %#v", len(tokens), tokens)
	}
	if tokens[0].tokenType != semanticType || text[tokens[0].start:tokens[0].start+tokens[0].length] != "Page" {
		t.Fatalf("type token = %#v", tokens[0])
	}
	if tokens[1].tokenType != semanticAccessor || text[tokens[1].start:tokens[1].start+tokens[1].length] != "page" {
		t.Fatalf("accessor token = %#v", tokens[1])
	}
	if tokens[2].tokenType != semanticField || text[tokens[2].start:tokens[2].start+tokens[2].length] != "Title" {
		t.Fatalf("field token = %#v", tokens[2])
	}
}

func TestLSPBuiltInTemplateFunctionCompletionHoverAndHighlight(t *testing.T) {
	idx := lspIndex{indexFile: indexFile{}}
	contract := templateIndex{}
	text := `{{ len "abc" }}`

	items := accessorCompletionItems(idx, contract, "le")
	if !hasCompletionLabel(items, "len") || !hasCompletionLabel(items, "le") {
		t.Fatalf("built-in completions = %#v, want len and le", items)
	}
	tokens := semanticTokensForText(text, idx, contract)
	if len(tokens) != 1 || tokens[0].tokenType != semanticFunction || text[tokens[0].start:tokens[0].start+tokens[0].length] != "len" {
		t.Fatalf("semantic tokens = %#v, want len function token", tokens)
	}
	name, _, _, ok := builtInFunctionAt(text, strings.Index(text, "len")+1)
	if !ok || name != "len" {
		t.Fatalf("builtInFunctionAt() = %q, %v; want len", name, ok)
	}
}

func TestLSPHoverReturnsSourceRange(t *testing.T) {
	idx := indexFile{
		Types: map[string]goTypeIndex{
			"example.com/app.Page": {
				Name: "Page",
				Fields: map[string]fieldIndex{
					"Title": {Type: "string"},
				},
			},
		},
		Short: map[string][]string{"Page": {"example.com/app.Page"}},
		Templates: map[string]templateIndex{
			"template.gohtml": {
				Models: map[string]string{"page": "example.com/app.Page"},
			},
		},
	}
	text := `{{/*
@model page example.com/app.Page
*/}}
{{ page.Title }}`
	server := &lspServer{root: ".", idx: idx, docs: map[string]string{"file:///template.gohtml": text}}
	result := server.hover(textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: "file:///template.gohtml"},
		Position:     positionAt(text, strings.Index(text, "Title")+1),
	})
	got, ok := result.(hover)
	if !ok {
		t.Fatalf("hover result = %#v", result)
	}
	if got.Range.Start.Line != 3 || got.Range.Start.Character != 8 || got.Range.End.Character != 13 {
		t.Fatalf("hover range = %#v", got.Range)
	}
}

func TestLSPRefreshIndexReloadsGeneratedFile(t *testing.T) {
	root := t.TempDir()
	indexPath := filepath.Join(root, ".go-doc", "index.json")
	if err := os.MkdirAll(filepath.Dir(indexPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(indexPath, []byte(`{
		"module": "example.com/app",
		"templates": {
			"page.gohtml": {
				"models": {"page": "example.com/app.Page"}
			}
		},
		"types": {
			"example.com/app.Page": {
				"name": "Page",
				"fields": {"Title": {"type": "string"}}
			}
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	server := &lspServer{
		root:      root,
		indexPath: indexPath,
		idx:       indexFile{Templates: map[string]templateIndex{}, Types: map[string]goTypeIndex{}},
	}
	server.refreshIndex()

	if _, ok := server.idx.Templates["page.gohtml"]; !ok {
		t.Fatalf("template was not reloaded: %#v", server.idx.Templates)
	}
	if _, ok := server.idx.Types["example.com/app.Page"]; !ok {
		t.Fatalf("type was not reloaded: %#v", server.idx.Types)
	}
}

func TestLSPCompletionUsesAssignedVariableType(t *testing.T) {
	idx := lspIndex{indexFile: indexFile{
		Types: map[string]goTypeIndex{
			"example.com/app.Page": {
				Name: "Page",
				Fields: map[string]fieldIndex{
					"Current": {Type: "Todo"},
				},
			},
			"example.com/app.Todo": {
				Name: "Todo",
				Fields: map[string]fieldIndex{
					"Done":  {Type: "bool"},
					"Title": {Type: "string"},
				},
			},
		},
		Short: map[string][]string{
			"Todo": {"example.com/app.Todo"},
		},
	}}
	contract := templateIndex{
		Models: map[string]string{"page": "example.com/app.Page"},
	}
	text := `{{ $todo := page.Current }}{{ $todo. }}`
	offset := len(`{{ $todo := page.Current }}{{ $todo.`)

	target, ok := fieldTargetBeforeCaret(text, offset, idx, contract)
	if !ok || target != "example.com/app.Todo" {
		t.Fatalf("fieldTargetBeforeCaret() = %q, %v", target, ok)
	}
}

func assertDiagnostic(t *testing.T, diagnostics []diagnostic, message string) {
	t.Helper()
	for _, diagnostic := range diagnostics {
		if diagnostic.Message == message {
			return
		}
	}
	t.Fatalf("missing diagnostic %q in %#v", message, diagnostics)
}

func hasCompletionLabel(items []completionItem, label string) bool {
	for _, item := range items {
		if item.Label == label {
			return true
		}
	}
	return false
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
