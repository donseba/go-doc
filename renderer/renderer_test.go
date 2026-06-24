package renderer

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
)

type testPage struct {
	Title string
}

func TestRegisterAddsModelAccessorsToTemplate(t *testing.T) {
	tmpl := template.New("page")
	if err := Register(tmpl, Model("page", testPage{Title: "Hello"})); err != nil {
		t.Fatal(err)
	}
	if _, err := tmpl.Parse(`{{ _page.Title }}`); err != nil {
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

func TestRegisterRejectsInvalidModel(t *testing.T) {
	err := Register(template.New("page"), Model("", testPage{}))
	if err == nil || !strings.Contains(err.Error(), "invalid model name") {
		t.Fatalf("err = %v, want invalid model name", err)
	}
}

func TestRegisterRejectsDuplicateModel(t *testing.T) {
	err := Register(template.New("page"), Model("page", testPage{}), Model("page", testPage{}))
	if err == nil || !strings.Contains(err.Error(), "duplicate model name") {
		t.Fatalf("err = %v, want duplicate model name", err)
	}
}

func TestRegisterRejectsModelNamesThatCannotBeTemplateFunctions(t *testing.T) {
	err := Register(template.New("page"), Model("todo-item", testPage{}))
	if err == nil || !strings.Contains(err.Error(), "invalid model name") {
		t.Fatalf("err = %v, want invalid model name", err)
	}
}

func TestRegisterRejectsNilTemplate(t *testing.T) {
	err := Register(nil, Model("page", testPage{}))
	if err == nil || !strings.Contains(err.Error(), "nil template") {
		t.Fatalf("err = %v, want nil template", err)
	}
}
