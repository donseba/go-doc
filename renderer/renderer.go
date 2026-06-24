// Package renderer provides small html/template helpers for go-doc contracts.
package renderer

import (
	"fmt"
	"html/template"
	"strings"
	"unicode"
)

// Binding binds a Go value to a go-doc template model name.
//
// A model named "page" becomes available as {{ _page.Title }}. The leading
// underscore mirrors go-doc's @model convention.
type Binding struct {
	Name  string
	Value any
}

// Model creates a model binding.
func Model(name string, value any) Binding {
	return Binding{Name: name, Value: value}
}

// Register installs model accessors on an existing template.
//
// Call Register before parsing templates, just like html/template.Funcs:
//
//	tmpl := template.New("dashboard.gohtml")
//	err := renderer.Register(tmpl, renderer.Model("page", page))
//	_, err = tmpl.ParseFiles("templates/dashboard.gohtml")
//
// Register mutates tmpl and only returns an error when a binding is invalid or
// html/template rejects the generated function map.
func Register(tmpl *template.Template, models ...Binding) (err error) {
	if tmpl == nil {
		return fmt.Errorf("register models: nil template")
	}
	funcs := make(template.FuncMap, len(models))
	for _, model := range models {
		name := strings.TrimSpace(model.Name)
		if !validModelName(name) {
			return fmt.Errorf("register models: invalid model name %q", model.Name)
		}
		accessor := "_" + name
		if _, exists := funcs[accessor]; exists {
			return fmt.Errorf("register models: duplicate model name %q", name)
		}
		captured := model.Value
		funcs[accessor] = func() any {
			return captured
		}
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("register models: %v", recovered)
		}
	}()
	tmpl.Funcs(funcs)
	return nil
}

func validModelName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if i == 0 {
			if r != '_' && !unicode.IsLetter(r) {
				return false
			}
			continue
		}
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
