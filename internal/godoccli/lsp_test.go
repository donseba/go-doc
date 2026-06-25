package godoccli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
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

func TestLSPTypeReferenceOnlyUsesDeclarationTail(t *testing.T) {
	idx := lspIndex{indexFile: indexFile{
		Types: map[string]goTypeIndex{
			"github.com/donseba/go-doc/examples/todo.TodoPage": {Name: "TodoPage"},
		},
		Short: map[string][]string{"TodoPage": {"github.com/donseba/go-doc/examples/todo.TodoPage"}},
	}}
	text := `{{/*
@model Page github.com/donseba/go-doc/examples/todo.TodoPage
*/}}`

	if _, ok := typeReferenceAt(text, strings.Index(text, "github.com")+1, idx); ok {
		t.Fatal("package path should not be a model type reference")
	}
	ref, ok := typeReferenceAt(text, strings.Index(text, "TodoPage")+1, idx)
	if !ok {
		t.Fatal("TodoPage tail should be a model type reference")
	}
	if got := text[ref.start:ref.end]; got != "TodoPage" {
		t.Fatalf("reference range = %q, want TodoPage", got)
	}
}

func TestLSPDiagnosticsIgnoreQuotedTemplateNames(t *testing.T) {
	idx := lspIndex{indexFile: indexFile{
		Types: map[string]goTypeIndex{
			"example.com/app.Page": {
				Name:   "Page",
				Fields: map[string]fieldIndex{"Title": {Type: "string"}},
			},
		},
	}}
	contract := templateIndex{
		Models: map[string]string{"Page": "example.com/app.Page"},
	}

	diagnostics := diagnosticsForText(`{{ template "todo_list.gohtml" }}
{{ template "todo_detail.gohtml" }}
{{ Page.Title }}`, idx, contract)
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want none", diagnostics)
	}
}

func TestLSPDiagnosticsValidateTemplateIncludeDotType(t *testing.T) {
	idx := lspIndex{indexFile: indexFile{
		Templates: map[string]templateIndex{
			"templates/page.gohtml": {
				Models: map[string]string{"Page": "example.com/app.Page"},
			},
			"templates/user_row.gohtml": {
				Dot: "example.com/app.User",
			},
			"templates/rows.gohtml#table_row": {
				Name:   "table_row",
				Dot:    "example.com/app.User",
				Source: "templates/rows.gohtml",
				Line:   3,
			},
		},
		Types: map[string]goTypeIndex{
			"example.com/app.Page": {
				Name: "Page",
				Fields: map[string]fieldIndex{
					"Users": {Type: "[]User"},
					"Title": {Type: "string"},
				},
			},
			"example.com/app.User": {
				Name:   "User",
				Fields: map[string]fieldIndex{"Name": {Type: "string"}},
			},
		},
		Short: map[string][]string{
			"User": {"example.com/app.User"},
		},
	}}
	contract := idx.Templates["templates/page.gohtml"]

	valid := diagnosticsForText(`{{ range Page.Users }}{{ template "user_row.gohtml" . }}{{ end }}`, idx, contract)
	if len(valid) != 0 {
		t.Fatalf("diagnostics = %#v, want valid row include", valid)
	}

	invalidSlice := diagnosticsForText(`{{ template "user_row.gohtml" Page.Users }}`, idx, contract)
	assertDiagnostic(t, invalidSlice, "Template user_row.gohtml expects User, got []User")

	invalidString := diagnosticsForText(`{{ template "user_row.gohtml" Page.Title }}`, idx, contract)
	assertDiagnostic(t, invalidString, "Template user_row.gohtml expects User, got string")

	validBlock := diagnosticsForText(`{{ range Page.Users }}{{ block "table_row" . }}{{ end }}{{ end }}`, idx, contract)
	if len(validBlock) != 0 {
		t.Fatalf("diagnostics = %#v, want valid named block include", validBlock)
	}

	invalidBlock := diagnosticsForText(`{{ block "table_row" Page.Title }}{{ end }}`, idx, contract)
	assertDiagnostic(t, invalidBlock, "Template table_row expects User, got string")
}

func TestLSPTemplateIncludeHoverAndDefinition(t *testing.T) {
	root := t.TempDir()
	idx := indexFile{
		Templates: map[string]templateIndex{
			"templates/page.gohtml":           {Models: map[string]string{"Page": "example.com/app.Page"}},
			"templates/user_row.gohtml":       {Dot: "example.com/app.User"},
			"templates/rows.gohtml#table_row": {Name: "table_row", Dot: "example.com/app.User", Source: "templates/rows.gohtml", Line: 5, Column: 3},
		},
		Types: map[string]goTypeIndex{
			"example.com/app.Page": {Name: "Page"},
			"example.com/app.User": {Name: "User", File: "models.go", Line: 7, Column: 6},
		},
		Short: map[string][]string{"User": {"example.com/app.User"}},
	}
	text := `{{ template "user_row.gohtml" Page }}`
	uri := uriFromPath(filepath.Join(root, "templates", "page.gohtml"))
	server := &lspServer{
		root: root,
		idx:  idx,
		docs: map[string]string{uri: text},
	}
	pos := positionAt(text, strings.Index(text, "user_row.gohtml")+1)

	hoverResult := server.hover(textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: uri},
		Position:     pos,
	})
	gotHover, ok := hoverResult.(hover)
	if !ok {
		t.Fatalf("hover result = %#v", hoverResult)
	}
	contents, ok := gotHover.Contents.(map[string]string)
	if !ok || !strings.Contains(contents["value"], "Expects `User`.") {
		t.Fatalf("hover contents = %#v, want expected child dot type", gotHover.Contents)
	}

	definitionResult := server.definition(textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: uri},
		Position:     pos,
	})
	gotDefinition, ok := definitionResult.(location)
	if !ok {
		t.Fatalf("definition result = %#v", definitionResult)
	}
	if !strings.Contains(filepath.ToSlash(gotDefinition.URI), "templates/user_row.gohtml") {
		t.Fatalf("definition URI = %q, want child template", gotDefinition.URI)
	}

	blockText := `{{ block "table_row" Page }}{{ end }}`
	blockURI := uriFromPath(filepath.Join(root, "templates", "table.gohtml"))
	server.docs[blockURI] = blockText
	blockPos := positionAt(blockText, strings.Index(blockText, "table_row")+1)
	blockDefinition := server.definition(textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: blockURI},
		Position:     blockPos,
	})
	gotBlockDefinition, ok := blockDefinition.(location)
	if !ok {
		t.Fatalf("block definition result = %#v", blockDefinition)
	}
	if !strings.Contains(filepath.ToSlash(gotBlockDefinition.URI), "templates/rows.gohtml") ||
		gotBlockDefinition.Range.Start.Line != 4 {
		t.Fatalf("block definition = %#v, want named define location", gotBlockDefinition)
	}
}

func TestLSPWarnsAndMovesLeadingDefineContract(t *testing.T) {
	text := `{{/*
@dot example.com/app.User
*/}}
{{ define "table_row" }}
<tr><td>{{ .Name }}</td></tr>
{{ end }}`
	idx := lspIndex{indexFile: indexFile{
		Types: map[string]goTypeIndex{
			"example.com/app.User": {Name: "User", Fields: map[string]fieldIndex{"Name": {Type: "string"}}},
		},
	}}
	contract := templateIndex{Dot: "example.com/app.User"}
	diagnostics := diagnosticsForText(text, idx, contract)
	assertDiagnostic(t, diagnostics, `Move go-doc annotations inside define "table_row"`)

	uri := "file:///rows.gohtml"
	server := &lspServer{
		root: ".",
		idx:  idx.indexFile,
		docs: map[string]string{uri: text},
	}
	actions := server.codeActions(codeActionParams{
		TextDocument: textDocumentIdentifier{URI: uri},
		Range:        diagnostics[0].Range,
		Context:      codeActionContext{Diagnostics: []diagnostic{diagnostics[0]}},
	})
	if len(actions) != 1 || actions[0].Edit == nil {
		t.Fatalf("actions = %#v, want move edit", actions)
	}
	edits := actions[0].Edit.Changes[uri]
	if len(edits) != 2 {
		t.Fatalf("edits = %#v, want delete and insert", edits)
	}
	if edits[1].NewText != "\n{{/*\n@dot example.com/app.User\n*/}}\n" {
		t.Fatalf("insert text = %q", edits[1].NewText)
	}
}

func TestLSPUnderstandsDeclaredTemplateFunctions(t *testing.T) {
	idx := lspIndex{indexFile: indexFile{
		Funcs: map[string]goFuncIndex{
			"example.com/app.Mul": {Name: "Mul", Doc: "Mul multiplies two integers.", Signature: "func Mul(x, y int) int", Params: []string{"int", "int"}},
		},
	}}
	contract := templateIndex{
		Funcs: map[string]string{"mul": "example.com/app.Mul"},
	}

	diagnostics := diagnosticsForText(`{{/*
@func mul example.com/app.Mul
*/}}
{{ mul 10 2 }}`, idx, contract)
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want none", diagnostics)
	}

	items := accessorCompletionItems(idx, contract, "m")
	if len(items) != 1 || items[0].Label != "mul" || items[0].InsertText != "mul " {
		t.Fatalf("items = %#v, want mul function completion", items)
	}

	tokens := semanticTokensForText(`{{ mul 10 2 }}`, idx, contract)
	if len(tokens) != 1 || tokens[0].tokenType != semanticFunction {
		t.Fatalf("tokens = %#v, want custom function token", tokens)
	}
}

func TestLSPDiagnosticsWarnsForInvalidDeclaredFunctionArgument(t *testing.T) {
	idx := lspIndex{indexFile: indexFile{
		Funcs: map[string]goFuncIndex{
			"example.com/app.Div": {Name: "Div", Signature: "func Div(x, y int) int", Params: []string{"int", "int"}},
		},
	}}
	contract := templateIndex{
		Funcs: map[string]string{"div": "example.com/app.Div"},
	}

	diagnostics := diagnosticsForText(`{{ div 10 "something" }}`, idx, contract)
	assertDiagnostic(t, diagnostics, "Cannot pass string to div argument 2 because it expects int")
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v, want one invalid argument diagnostic", diagnostics)
	}
}

func TestLSPUnderstandsElseIfConditions(t *testing.T) {
	idx := lspIndex{indexFile: indexFile{
		Types: map[string]goTypeIndex{
			"example.com/app.Page": {
				Name: "Page",
				Fields: map[string]fieldIndex{
					"Items":       {Type: "[]Todo"},
					"Ready":       {Type: "bool"},
					"Title":       {Type: "string"},
					"GeneratedAt": {Type: "time.Time"},
				},
			},
		},
		Funcs: map[string]goFuncIndex{
			"example.com/app.IsReady": {Name: "IsReady", Signature: "func IsReady(v bool) bool", Params: []string{"bool"}, Result: "bool"},
		},
		Short: map[string][]string{"Todo": {"example.com/app.Todo"}},
	}}
	contract := templateIndex{
		Models: map[string]string{"Page": "example.com/app.Page"},
		Funcs:  map[string]string{"isReady": "example.com/app.IsReady"},
	}

	if got := diagnosticsForText(`{{ if Page.Ready }}A{{ else if Page.Ready }}B{{ end }}`, idx, contract); len(got) != 0 {
		t.Fatalf("diagnostics = %#v, want no diagnostics for valid else-if model condition", got)
	}

	diagnostics := diagnosticsForText(`{{ if Page.Ready }}A{{ else if Page.Missing }}B{{ end }}`, idx, contract)
	assertDiagnostic(t, diagnostics, "Unknown field 'Missing' on Page")

	diagnostics = diagnosticsForText(`{{ if Page.Ready }}A{{ else if isReady Page.Title }}B{{ end }}`, idx, contract)
	assertDiagnostic(t, diagnostics, "Cannot pass string to isReady argument 1 because it expects bool")

	diagnostics = diagnosticsForText(`{{ if Page.Ready }}A{{ else if len Page.GeneratedAt }}B{{ end }}`, idx, contract)
	assertDiagnostic(t, diagnostics, "Cannot call len on 'Page.GeneratedAt' because it is time.Time")

	text := `{{ if Page.Ready }}A{{ else if isReady Page.Ready }}B{{ end }}`
	tokens := semanticTokensForText(text, idx, contract)
	foundFunction := false
	foundField := false
	for _, token := range tokens {
		value := text[token.start : token.start+token.length]
		if token.tokenType == semanticFunction && value == "isReady" {
			foundFunction = true
		}
		if token.tokenType == semanticField && value == "Ready" {
			foundField = true
		}
	}
	if !foundFunction || !foundField {
		t.Fatalf("tokens = %#v, want else-if function and field tokens", tokens)
	}
}

func TestLSPDiagnosticsWarnsForInvalidDeclaredFunctionArity(t *testing.T) {
	idx := lspIndex{indexFile: indexFile{
		Funcs: map[string]goFuncIndex{
			"example.com/app.Div":  {Name: "Div", Signature: "func Div(x, y int) int", Params: []string{"int", "int"}, Result: "int"},
			"example.com/app.Join": {Name: "Join", Signature: "func Join(first string, rest ...string) string", Params: []string{"string", "...string"}, Result: "string"},
			"example.com/app.Now":  {Name: "Now", Signature: "func Now() time.Time", Result: "time.Time"},
		},
	}}
	contract := templateIndex{
		Funcs: map[string]string{
			"div":  "example.com/app.Div",
			"join": "example.com/app.Join",
			"now":  "example.com/app.Now",
		},
	}

	diagnostics := diagnosticsForText(`{{ div 10 }}
{{ div 10 2 3 }}
{{ join }}
{{ join "a" }}
{{ join "a" "b" }}
{{ now 1 }}`, idx, contract)
	assertDiagnostic(t, diagnostics, "Function div expects 2 argument(s), got 1")
	assertDiagnostic(t, diagnostics, "Function div expects 2 argument(s), got 3")
	assertDiagnostic(t, diagnostics, "Function join expects at least 1 argument(s), got 0")
	assertDiagnostic(t, diagnostics, "Function now expects 0 argument(s), got 1")
	if len(diagnostics) != 4 {
		t.Fatalf("diagnostics = %#v, want four arity diagnostics", diagnostics)
	}
}

func TestLSPDiagnosticsUnderstandNestedFunctionArguments(t *testing.T) {
	idx := lspIndex{indexFile: indexFile{
		Types: map[string]goTypeIndex{
			"example.com/app.User": {Name: "User"},
		},
		Funcs: map[string]goFuncIndex{
			"example.com/app.CurrentID":   {Name: "CurrentID", Result: "int"},
			"example.com/app.CurrentUser": {Name: "CurrentUser", Result: "User"},
			"example.com/app.UserByID":    {Name: "UserByID", Result: "User", Params: []string{"int"}},
		},
		Short: map[string][]string{"User": {"example.com/app.User"}},
	}}
	contract := templateIndex{
		Funcs: map[string]string{
			"currentID":   "example.com/app.CurrentID",
			"currentUser": "example.com/app.CurrentUser",
			"userByID":    "example.com/app.UserByID",
		},
	}

	diagnostics := diagnosticsForText(`{{ userByID (currentID) }}
{{ userByID (currentUser) }}`, idx, contract)
	assertDiagnostic(t, diagnostics, "Cannot pass example.com/app.User to userByID argument 1 because it expects int")
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v, want one nested argument diagnostic", diagnostics)
	}
}

func TestLSPDiagnosticsUnderstandPipelineFunctionArguments(t *testing.T) {
	idx := lspIndex{indexFile: indexFile{
		Types: map[string]goTypeIndex{
			"example.com/app.Page": {
				Name: "Page",
				Fields: map[string]fieldIndex{
					"DoneCount": {Type: "int"},
					"Title":     {Type: "string"},
				},
			},
		},
		Funcs: map[string]goFuncIndex{
			"example.com/app.Div": {Name: "Div", Result: "int", Params: []string{"int", "int"}},
		},
	}}
	contract := templateIndex{
		Models: map[string]string{"Page": "example.com/app.Page"},
		Funcs:  map[string]string{"div": "example.com/app.Div"},
	}

	diagnostics := diagnosticsForText(`{{ Page.DoneCount | div 2 }}
{{ Page.DoneCount | div }}
{{ Page.DoneCount | div "bad" }}
{{ Page.Title | div 2 }}`, idx, contract)
	assertDiagnostic(t, diagnostics, "Function div expects 2 argument(s), got 1")
	assertDiagnostic(t, diagnostics, "Cannot pass string to div argument 1 because it expects int")
	assertDiagnostic(t, diagnostics, "Cannot pipe string to div argument 2 because it expects int")
	if len(diagnostics) != 3 {
		t.Fatalf("diagnostics = %#v, want three pipeline diagnostics", diagnostics)
	}
}

func TestLSPDiagnosticsValidateFunctionReturnShape(t *testing.T) {
	idx := lspIndex{indexFile: indexFile{
		Funcs: map[string]goFuncIndex{
			"example.com/app.Lookup": {Name: "Lookup", Results: []string{"string", "error"}, ReturnOK: true},
			"example.com/app.Bad":    {Name: "Bad", Results: []string{"string", "bool"}, ReturnOK: true},
			"example.com/app.Noop":   {Name: "Noop", ReturnOK: true},
		},
	}}
	contract := templateIndex{
		Funcs: map[string]string{
			"lookup": "example.com/app.Lookup",
			"bad":    "example.com/app.Bad",
			"noop":   "example.com/app.Noop",
		},
	}

	diagnostics := diagnosticsForText(`{{ lookup }}
{{ bad }}
{{ noop }}`, idx, contract)
	assertDiagnostic(t, diagnostics, "Function bad has unsupported template return values (string, bool); use one value or (value, error)")
	assertDiagnostic(t, diagnostics, "Function noop cannot be used in a template action because it returns no value")
	if len(diagnostics) != 2 {
		t.Fatalf("diagnostics = %#v, want two return-shape diagnostics", diagnostics)
	}
}

func TestLSPUsesDeclaredFunctionReturnTypes(t *testing.T) {
	idx := lspIndex{indexFile: indexFile{
		Types: map[string]goTypeIndex{
			"example.com/app.User": {
				Name: "User",
				Fields: map[string]fieldIndex{
					"Name":    {Type: "string"},
					"Email":   {Type: "string"},
					"Profile": {Type: "Profile"},
				},
			},
			"example.com/app.Profile": {
				Name:   "Profile",
				Fields: map[string]fieldIndex{"City": {Type: "string"}},
			},
		},
		Funcs: map[string]goFuncIndex{
			"example.com/app.CurrentUser": {Name: "CurrentUser", Result: "User"},
			"example.com/app.ActiveUsers": {Name: "ActiveUsers", Result: "[]User"},
		},
		Short: map[string][]string{
			"User":    {"example.com/app.User"},
			"Profile": {"example.com/app.Profile"},
		},
	}}
	contract := templateIndex{
		Funcs: map[string]string{
			"currentUser": "example.com/app.CurrentUser",
			"activeUsers": "example.com/app.ActiveUsers",
		},
	}

	text := `{{ with currentUser }}{{ .Name }}{{ .Profile.City }}{{ end }}
{{ range activeUsers }}{{ .Email }}{{ end }}
{{ $user := currentUser }}{{ $user.Name }}
{{ currentUser.Profile.City }}
{{ (currentUser).Profile.City }}`

	if diagnostics := diagnosticsForText(text, idx, contract); len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want none", diagnostics)
	}
}

func TestLSPDiagnosticsUseDeclaredFunctionReturnTypes(t *testing.T) {
	idx := lspIndex{indexFile: indexFile{
		Types: map[string]goTypeIndex{
			"example.com/app.User": {
				Name:   "User",
				Fields: map[string]fieldIndex{"Name": {Type: "string"}},
			},
		},
		Funcs: map[string]goFuncIndex{
			"example.com/app.CurrentUser": {Name: "CurrentUser", Result: "User"},
		},
		Short: map[string][]string{"User": {"example.com/app.User"}},
	}}
	contract := templateIndex{
		Funcs: map[string]string{"currentUser": "example.com/app.CurrentUser"},
	}

	diagnostics := diagnosticsForText(`{{ currentUser.Nope }}
{{ range currentUser }}{{ .Name }}{{ end }}`, idx, contract)
	assertDiagnostic(t, diagnostics, "Unknown field 'Nope' on User")
	assertDiagnostic(t, diagnostics, "Cannot range over 'currentUser' because it is example.com/app.User")
	if len(diagnostics) != 2 {
		t.Fatalf("diagnostics = %#v, want two diagnostics", diagnostics)
	}
}

func TestLSPUsesParenthesizedFunctionReturnWithArguments(t *testing.T) {
	idx := lspIndex{indexFile: indexFile{
		Types: map[string]goTypeIndex{
			"example.com/app.User": {
				Name: "User",
				Fields: map[string]fieldIndex{
					"Name":  {Type: "string"},
					"Email": {Type: "string"},
				},
			},
		},
		Funcs: map[string]goFuncIndex{
			"example.com/app.UserByID": {Name: "UserByID", Result: "User", Params: []string{"int"}},
		},
		Short: map[string][]string{"User": {"example.com/app.User"}},
	}}
	contract := templateIndex{
		Funcs: map[string]string{"userByID": "example.com/app.UserByID"},
	}

	text := `{{ (userByID 42).Name }}
{{ (userByID 42).Missing }}`
	diagnostics := diagnosticsForText(text, idx, contract)
	assertDiagnostic(t, diagnostics, "Unknown field 'Missing' on User")
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v, want one diagnostic", diagnostics)
	}

	ref, ok := fieldReferenceAt(text, strings.Index(text, "Name")+1, idx, contract)
	if !ok {
		t.Fatal("expected field reference for parenthesized function result")
	}
	if ref.ownerType != "example.com/app.User" || ref.fieldName != "Name" {
		t.Fatalf("ref = %#v, want User.Name", ref)
	}

	for _, text := range []string{`{{ (userByID 42). }}`, `{{ (userByID 42).`} {
		items := completionsForText(t, text, idx, contract, strings.Index(text, ".")+1)
		if len(items) != 2 {
			t.Fatalf("items for %q = %#v, want User fields after parenthesized function call", text, items)
		}
		if items[0].Label != "Email" || items[1].Label != "Name" {
			t.Fatalf("items for %q = %#v, want Email and Name field completions", text, items)
		}
	}

	longText := strings.Repeat("<p>padding</p>\n", 60) + `{{ (userByID 42). }}`
	items := completionsForText(t, longText, idx, contract, strings.LastIndex(longText, ".")+1)
	if len(items) != 2 {
		t.Fatalf("items for long template = %#v, want User fields after parenthesized function call", items)
	}
}

func TestLSPSemanticTokensHighlightDeclaredFunctionTypes(t *testing.T) {
	idx := lspIndex{indexFile: indexFile{
		Funcs: map[string]goFuncIndex{
			"github.com/donseba/go-doc/examples/table.FirstUser": {Name: "FirstUser"},
		},
	}}
	contract := templateIndex{}
	text := `{{/*
@func firstUser github.com/donseba/go-doc/examples/table.FirstUser
*/}}`

	tokens := semanticTokensForText(text, idx, contract)
	found := false
	for _, token := range tokens {
		if token.tokenType == semanticFunction && text[token.start:token.start+token.length] == "FirstUser" {
			found = true
		}
		if token.tokenType == semanticFunction && strings.Contains(text[token.start:token.start+token.length], "github.com") {
			t.Fatalf("function declaration token should only cover the tail, got %q", text[token.start:token.start+token.length])
		}
	}
	if !found {
		t.Fatalf("tokens = %#v, want semantic function token for FirstUser tail", tokens)
	}
}

func TestLSPFindsDeclaredFunctionOperands(t *testing.T) {
	idx := lspIndex{indexFile: indexFile{
		Types: map[string]goTypeIndex{
			"example.com/app.User": {
				Name: "User",
				Fields: map[string]fieldIndex{
					"Name": {Type: "string"},
				},
			},
		},
		Funcs: map[string]goFuncIndex{
			"example.com/app.FirstUser": {Name: "FirstUser", Result: "User"},
			"example.com/app.UserByID":  {Name: "UserByID", Result: "User", Params: []string{"int"}},
		},
		Short: map[string][]string{"User": {"example.com/app.User"}},
	}}
	contract := templateIndex{
		Funcs: map[string]string{
			"firstUser": "example.com/app.FirstUser",
			"userByID":  "example.com/app.UserByID",
		},
	}
	text := `{{ with firstUser }}{{ .Name }}{{ end }}
{{ (userByID 2).Name }}`

	name, _, _, ok := templateFunctionAt(text, strings.Index(text, "firstUser")+1, idx, contract)
	if !ok || name != "firstUser" {
		t.Fatalf("templateFunctionAt(firstUser) = %q, %v; want firstUser", name, ok)
	}
	name, _, _, ok = templateFunctionAt(text, strings.Index(text, "userByID")+1, idx, contract)
	if !ok || name != "userByID" {
		t.Fatalf("templateFunctionAt(userByID) = %q, %v; want userByID", name, ok)
	}

	tokens := semanticTokensForText(text, idx, contract)
	foundFirstUser := false
	foundUserByID := false
	for _, token := range tokens {
		if token.tokenType != semanticFunction {
			continue
		}
		switch text[token.start : token.start+token.length] {
		case "firstUser":
			foundFirstUser = true
		case "userByID":
			foundUserByID = true
		}
	}
	if !foundFirstUser || !foundUserByID {
		t.Fatalf("tokens = %#v, want semantic function tokens for firstUser and userByID", tokens)
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

func TestLSPRangeDiagnosticIsErrorSeverity(t *testing.T) {
	idx := lspIndex{indexFile: indexFile{
		Types: map[string]goTypeIndex{
			"example.com/app.Page": {
				Name:   "Page",
				Fields: map[string]fieldIndex{"GeneratedAt": {Type: "time.Time"}},
			},
		},
	}}
	contract := templateIndex{
		Models: map[string]string{"Page": "example.com/app.Page"},
	}

	diagnostics := diagnosticsForText(`{{ range Page.GeneratedAt }}{{ end }}`, idx, contract)
	assertDiagnostic(t, diagnostics, "Cannot range over 'Page.GeneratedAt' because it is time.Time")
	if len(diagnostics) != 1 || diagnostics[0].Severity != 2 {
		t.Fatalf("diagnostics = %#v, want one error severity diagnostic", diagnostics)
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
	text := `{{ range _.page.Items }}{{ . }}{{ end }}`
	offset := len(`{{ range _.page.Items }}{{ .`)

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
		t.Fatalf("items = %#v, want todo and len", items)
	}
	if hasCompletionLabel(items, "_") {
		t.Fatalf("items = %#v, did not expect hidden compatibility scope", items)
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
	text := `{{ range _.page.Projects }}
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

func TestLSPDiagnosticsUseDotContractAsRootDotType(t *testing.T) {
	idx := lspIndex{indexFile: indexFile{
		Types: map[string]goTypeIndex{
			"example.com/app.User": {
				Name: "User",
				Fields: map[string]fieldIndex{
					"ID":    {Type: "int"},
					"Name":  {Type: "string"},
					"Email": {Type: "string"},
				},
				Methods: map[string]methodIndex{
					"Status": {Type: "string", Signature: "func() string"},
				},
			},
		},
	}}
	contract := templateIndex{
		Dot: "example.com/app.User",
	}

	diagnostics := diagnosticsForText(`{{ .ID }} {{ .Name }} {{ .Email }} {{ .Status }}`, idx, contract)
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want none", diagnostics)
	}
}

func TestLSPDiagnosticsDoNotUseSingleModelAsImplicitRootDotType(t *testing.T) {
	idx := lspIndex{indexFile: indexFile{
		Types: map[string]goTypeIndex{
			"example.com/app.User": {
				Name:   "User",
				Fields: map[string]fieldIndex{"Name": {Type: "string"}},
			},
		},
	}}
	contract := templateIndex{
		Models: map[string]string{"User": "example.com/app.User"},
	}

	if _, ok := fieldReferenceAt(`{{ .Name }}`, len(`{{ .Name`)-1, idx, contract); ok {
		t.Fatal("fieldReferenceAt resolved .Name without @dot; single @model should not type root dot")
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
	text := `{{ range _.page.Items }}{{ . }}{{ end }}`
	offset := len(`{{ range _.page.Items }}{{ .`)

	target, ok := fieldTargetBeforeCaret(text, offset, idx, contract)
	if !ok || target != "example.com/app.Todo" {
		t.Fatalf("fieldTargetBeforeCaret(slice) = %q, %v", target, ok)
	}

	text = `{{ range _.page.ByID }}{{ . }}{{ end }}`
	offset = len(`{{ range _.page.ByID }}{{ .`)
	target, ok = fieldTargetBeforeCaret(text, offset, idx, contract)
	if !ok || target != "example.com/app.Todo" {
		t.Fatalf("fieldTargetBeforeCaret(map) = %q, %v", target, ok)
	}

	ref, ok := fieldReferenceAt(`{{ _.page.Items }}`, len(`{{ _.page.Items`), idx, contract)
	if !ok || ref.fieldName != "Items" {
		t.Fatalf("fieldReferenceAt(field) = %#v, %v", ref, ok)
	}
	valueType := resolveFieldValuePath(idx, "example.com/app.Todo", []string{"Label"})
	if valueType != "string" {
		t.Fatalf("method return type = %q, want string", valueType)
	}
}

func TestLSPCompletesModelScopeModelsAndModelTypes(t *testing.T) {
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
		t.Fatalf("model completions = %#v", items)
	}

	server.docs["file:///template.gohtml"] = `{{ _.pa }}`
	items = server.completions(textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: "file:///template.gohtml"},
		Position:     position{Line: 0, Character: len(`{{ _.pa`)},
	})
	if len(items) != 1 || items[0].Label != "page" || items[0].InsertText != "page." {
		t.Fatalf("scope model completions = %#v", items)
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
{{ _.page.Title }}`

	tokens := semanticTokensForText(text, idx, contract)
	if len(tokens) != 4 {
		t.Fatalf("len(tokens) = %d, want 4: %#v", len(tokens), tokens)
	}
	if tokens[0].tokenType != semanticType || text[tokens[0].start:tokens[0].start+tokens[0].length] != "Page" {
		t.Fatalf("type token = %#v", tokens[0])
	}
	if tokens[1].tokenType != semanticAccessor || text[tokens[1].start:tokens[1].start+tokens[1].length] != "_" {
		t.Fatalf("accessor token = %#v", tokens[1])
	}
	if tokens[2].tokenType != semanticType || text[tokens[2].start:tokens[2].start+tokens[2].length] != "page" {
		t.Fatalf("scope model token = %#v", tokens[2])
	}
	if tokens[3].tokenType != semanticField || text[tokens[3].start:tokens[3].start+tokens[3].length] != "Title" {
		t.Fatalf("field token = %#v", tokens[3])
	}
}

func TestLSPDiagnosticsUnderstandModelScope(t *testing.T) {
	idx := lspIndex{indexFile: indexFile{
		Types: map[string]goTypeIndex{
			"example.com/app.Page": {
				Name: "Page",
				Fields: map[string]fieldIndex{
					"Items": {Type: "[]Todo"},
					"Title": {Type: "string"},
				},
			},
			"example.com/app.Todo": {
				Name: "Todo",
				Fields: map[string]fieldIndex{
					"Title": {Type: "string"},
				},
			},
		},
		Short: map[string][]string{"Todo": {"example.com/app.Todo"}},
	}}
	contract := templateIndex{Models: map[string]string{"Page": "example.com/app.Page"}}

	if got := diagnosticsForText(`{{ _.Page.Title }}`, idx, contract); len(got) != 0 {
		t.Fatalf("diagnostics = %#v, want none", got)
	}
	if got := diagnosticsForText(`{{ Page.Title }}`, idx, contract); len(got) != 0 {
		t.Fatalf("diagnostics = %#v, want direct model access to work", got)
	}

	diagnostics := diagnosticsForText(`{{ range _.Page.Title }}{{ end }}`, idx, contract)
	assertDiagnostic(t, diagnostics, "Cannot range over '_.Page.Title' because it is string")

	diagnostics = diagnosticsForText(`{{ _.Missing.Title }}`, idx, contract)
	assertDiagnostic(t, diagnostics, "Unknown go-doc model 'Missing'")

	diagnostics = diagnosticsForText(`{{ Missing.Title }}`, idx, contract)
	assertDiagnostic(t, diagnostics, "Unknown go-doc accessor 'Missing'")
}

func TestLSPCompletionUsesModelScopeFields(t *testing.T) {
	idx := lspIndex{indexFile: indexFile{
		Types: map[string]goTypeIndex{
			"example.com/app.Page": {
				Name: "Page",
				Fields: map[string]fieldIndex{
					"Title": {Type: "string"},
				},
			},
		},
	}}
	contract := templateIndex{Models: map[string]string{"Page": "example.com/app.Page"}}
	text := `{{ _.Page. }}`
	offset := len(`{{ _.Page.`)

	target, ok := fieldTargetBeforeCaret(text, offset, idx, contract)
	if !ok || target != "example.com/app.Page" {
		t.Fatalf("fieldTargetBeforeCaret() = %q, %v", target, ok)
	}
	ref, ok := fieldReferenceAt(`{{ _.Page.Title }}`, len(`{{ _.Page.Title`), idx, contract)
	if !ok || ref.ownerType != "example.com/app.Page" || ref.fieldName != "Title" {
		t.Fatalf("fieldReferenceAt() = %#v, %v", ref, ok)
	}
	items := scopeModelCompletionItems(idx, contract, "Pa")
	if len(items) != 1 || items[0].Label != "Page" {
		t.Fatalf("scopeModelCompletionItems() = %#v", items)
	}
	items = accessorCompletionItems(idx, contract, "Pa")
	if len(items) != 1 || items[0].Label != "Page" || items[0].InsertText != "Page." {
		t.Fatalf("accessorCompletionItems() = %#v", items)
	}
}

func TestLSPModelScopeDrivesRangeDotType(t *testing.T) {
	idx := lspIndex{indexFile: indexFile{
		Types: map[string]goTypeIndex{
			"example.com/app.Page": {
				Name:   "Page",
				Fields: map[string]fieldIndex{"Items": {Type: "[]Todo"}},
			},
			"example.com/app.Todo": {
				Name:   "Todo",
				Fields: map[string]fieldIndex{"Title": {Type: "string"}},
			},
		},
		Short: map[string][]string{"Todo": {"example.com/app.Todo"}},
	}}
	contract := templateIndex{Models: map[string]string{"Page": "example.com/app.Page"}}
	text := `{{ range _.Page.Items }}{{ .Title }}{{ end }}`
	offset := strings.Index(text, ".Title") + len(".Title")

	ref, ok := fieldReferenceAt(text, offset, idx, contract)
	if !ok || ref.ownerType != "example.com/app.Todo" || ref.fieldName != "Title" {
		t.Fatalf("fieldReferenceAt() = %#v, %v", ref, ok)
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
{{ _.page.Title }}`
	server := &lspServer{root: ".", idx: idx, docs: map[string]string{"file:///template.gohtml": text}}
	result := server.hover(textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: "file:///template.gohtml"},
		Position:     positionAt(text, strings.Index(text, "Title")+1),
	})
	got, ok := result.(hover)
	if !ok {
		t.Fatalf("hover result = %#v", result)
	}
	if got.Range.Start.Line != 3 || got.Range.Start.Character != 10 || got.Range.End.Character != 15 {
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

func TestLSPInlineModelContractReplacesIndexedNames(t *testing.T) {
	idx := lspIndex{indexFile: indexFile{
		Types: map[string]goTypeIndex{
			"example.com/app.Page": {
				Name:   "Page",
				Fields: map[string]fieldIndex{"Title": {Type: "string"}},
			},
		},
		Templates: map[string]templateIndex{
			"page.gohtml": {
				Models: map[string]string{"TODO": "example.com/app.Page"},
			},
		},
	}}
	text := `{{/*
@model BLA example.com/app.Page
*/}}
{{ _.BLA.Title }}
{{ _.TODO.Title }}`
	server := &lspServer{
		root: ".",
		idx:  idx.indexFile,
		docs: map[string]string{"file:///page.gohtml": text},
	}

	contract, ok := server.contractForURI("file:///page.gohtml", idx)
	if !ok {
		t.Fatal("contractForURI() = false, want true")
	}
	if contract.Models["BLA"] != "example.com/app.Page" {
		t.Fatalf("BLA contract = %q, want Page", contract.Models["BLA"])
	}
	if _, ok := contract.Models["TODO"]; ok {
		t.Fatalf("stale TODO contract survived inline merge: %#v", contract.Models)
	}

	diagnostics := diagnosticsForText(text, idx, contract)
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v, want one stale TODO diagnostic", diagnostics)
	}
	assertDiagnostic(t, diagnostics, "Unknown go-doc model 'TODO'")
}

func TestLSPRenameModelNameUpdatesDeclarationAndScopeReferences(t *testing.T) {
	idx := indexFile{
		Types: map[string]goTypeIndex{
			"example.com/app.Page": {
				Name:   "Page",
				Fields: map[string]fieldIndex{"Title": {Type: "string"}},
			},
		},
		Templates: map[string]templateIndex{
			"page.gohtml": {
				Models: map[string]string{"page": "example.com/app.Page"},
			},
		},
	}
	text := `{{/*
@model page example.com/app.Page
*/}}
{{ _.page.Title }}
{{ page.Title }}`
	uri := "file:///page.gohtml"
	server := &lspServer{root: ".", idx: idx, docs: map[string]string{uri: text}}

	result := server.rename(renameParams{
		TextDocument: textDocumentIdentifier{URI: uri},
		Position:     positionAt(text, strings.Index(text, "@model page")+len("@model pa")),
		NewName:      "something",
	})
	edit, ok := result.(workspaceEdit)
	if !ok {
		t.Fatalf("rename result = %#v, want workspaceEdit", result)
	}
	edits := edit.Changes[uri]
	if len(edits) != 3 {
		t.Fatalf("len(edits) = %d, want 3: %#v", len(edits), edits)
	}

	renamed := applyTextEdits(text, edits)
	if !strings.Contains(renamed, "@model something example.com/app.Page") {
		t.Fatalf("renamed text missing declaration:\n%s", renamed)
	}
	if !strings.Contains(renamed, "{{ _.something.Title }}") {
		t.Fatalf("renamed text missing scoped use:\n%s", renamed)
	}
	if !strings.Contains(renamed, "{{ something.Title }}") {
		t.Fatalf("renamed text missing direct use:\n%s", renamed)
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
	text := `{{ $todo := _.page.Current }}{{ $todo. }}`
	offset := len(`{{ $todo := _.page.Current }}{{ $todo.`)

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

func completionsForText(t *testing.T, text string, idx lspIndex, contract templateIndex, offset int) []completionItem {
	t.Helper()
	uri := "file:///template.gohtml"
	server := &lspServer{
		idx: indexFile{
			Version:   idx.Version,
			Module:    idx.Module,
			Templates: map[string]templateIndex{"template.gohtml": contract},
			Types:     idx.Types,
			Funcs:     idx.Funcs,
			Short:     idx.Short,
		},
		docs: map[string]string{uri: text},
	}
	return server.completions(textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: uri},
		Position:     positionAt(text, offset),
	})
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

func applyTextEdits(text string, edits []textEdit) string {
	sort.Slice(edits, func(i, j int) bool {
		return offsetAt(text, edits[i].Range.Start) > offsetAt(text, edits[j].Range.Start)
	})
	for _, edit := range edits {
		start := offsetAt(text, edit.Range.Start)
		end := offsetAt(text, edit.Range.End)
		text = text[:start] + edit.NewText + text[end:]
	}
	return text
}
