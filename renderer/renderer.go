// Package renderer provides small html/template helpers for go-doc contracts.
package renderer

import (
	"fmt"
	"html/template"
	"maps"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// Mode controls when template contracts are read.
type Mode string

const (
	// Development scans template files every time Register is called.
	//
	// Use this while editing templates: changing @model Page to @model Dashboard
	// is picked up on the next render without restarting the process.
	Development Mode = "development"

	// Production scans template files once when the Renderer is created.
	//
	// Use this for deployed applications: the render path avoids repeated file
	// reads, and contract changes require an application restart.
	Production Mode = "production"
)

// Config configures a reusable Renderer.
type Config struct {
	Mode  Mode
	Files []string
}

// Renderer registers go-doc model accessors for one template set.
//
// Controllers can call Register the same way in development and production; the
// mode only changes when template contracts are scanned.
type Renderer struct {
	mode      Mode
	files     []string
	contracts Contracts
}

// Binding binds a Go value to a template function name.
//
// For contract-driven rendering, prefer RegisterFromFiles or
// RegisterFromContracts. They register @model declarations directly as
// template accessors.
type Binding struct {
	Name  string
	Value any
}

// LookupFunc resolves a value by key. A zero/nil result means not found when
// used with RegisterFromLookup.
type LookupFunc[K comparable, V any] = func(K) V

// ModelLookupFunc resolves a declared @model type name to a Go value.
//
// The key is the normalized contract type, for example:
//
//	github.com/example/app.Page
type ModelLookupFunc = LookupFunc[string, any]

// Contracts contains template @model declarations scanned from files.
//
// Create it once at startup for production-style rendering, then reuse it for
// each request with Register. Use RegisterFromFiles directly when you want
// development-style rendering that re-reads declarations before each parse.
type Contracts struct {
	models map[string]string
}

// Model creates a model binding.
func Model(name string, value any) Binding {
	return Binding{Name: name, Value: value}
}

// New creates a reusable Renderer for a template set.
//
// If Mode is empty, Production is used. Development is useful when templates are
// still changing; Production is the predictable default for applications.
func New(config Config) (Renderer, error) {
	mode := config.Mode
	if mode == "" {
		mode = Production
	}
	files := slices.Clone(config.Files)
	r := Renderer{mode: mode, files: files}

	switch mode {
	case Development:
		return r, nil
	case Production:
		contracts, err := LoadContracts(files...)
		if err != nil {
			return Renderer{}, err
		}
		r.contracts = contracts
		return r, nil
	default:
		return Renderer{}, fmt.Errorf("register models: unknown renderer mode %q", mode)
	}
}

// Register installs model accessors on tmpl using the configured mode.
func (r Renderer) Register(tmpl *template.Template, values ...any) error {
	switch r.mode {
	case Development:
		return RegisterFromFiles(tmpl, values, r.files...)
	case Production:
		return r.contracts.Register(tmpl, values...)
	default:
		return fmt.Errorf("register models: unknown renderer mode %q", r.mode)
	}
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
		if _, exists := funcs[name]; exists {
			return fmt.Errorf("register models: duplicate model name %q", name)
		}
		captured := model.Value
		funcs[name] = func() any {
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

// RegisterFromFiles scans @model declarations in template files and registers
// them by matching those declarations to the provided Go values.
//
// The template contract owns the public model name:
//
//	{{/*
//	@model Page github.com/example/app.Page
//	*/}}
//	{{ Page.Title }}
//
// The controller only passes values:
//
//	tmpl := template.New("page.gohtml")
//	err := renderer.RegisterFromFiles(tmpl, []any{page}, "templates/page.gohtml")
//	_, err = tmpl.ParseFiles("templates/page.gohtml")
//
// RegisterFromFiles must run before parsing templates, just like Register.
func RegisterFromFiles(tmpl *template.Template, values []any, files ...string) error {
	contracts, err := LoadContracts(files...)
	if err != nil {
		return err
	}
	return contracts.Register(tmpl, values...)
}

// RegisterFromContracts registers model accessors from a parsed contract map.
// It is useful when an application already has contract metadata.
func RegisterFromContracts(tmpl *template.Template, contracts map[string]string, values ...any) error {
	return Contracts{models: maps.Clone(contracts)}.Register(tmpl, values...)
}

// Register registers model accessors from a pre-scanned contract set.
func (contracts Contracts) Register(tmpl *template.Template, values ...any) error {
	lookup, err := NewModelLookup(values...)
	if err != nil {
		return err
	}
	return contracts.RegisterFromLookup(tmpl, lookup)
}

// RegisterFromLookup registers model accessors from a pre-scanned contract set
// using a custom type lookup.
func (contracts Contracts) RegisterFromLookup(tmpl *template.Template, lookup ModelLookupFunc) error {
	return RegisterFromLookup(tmpl, contracts.models, lookup)
}

// Models returns a copy of the scanned model contract map.
func (contracts Contracts) Models() map[string]string {
	return maps.Clone(contracts.models)
}

// RegisterFromLookup registers model accessors using a custom type lookup.
//
// The lookup receives each normalized @model type and should return the matching
// value. Return nil when the type cannot be resolved.
func RegisterFromLookup(tmpl *template.Template, contracts map[string]string, lookup ModelLookupFunc) error {
	models, err := bindingsFromLookup(contracts, lookup)
	if err != nil {
		return err
	}
	return Register(tmpl, models...)
}

// NewModelLookup builds a type lookup from concrete Go values.
func NewModelLookup(values ...any) (ModelLookupFunc, error) {
	models := make([]modelValue, 0, len(values))
	for _, value := range values {
		model, ok := newModelValue(value)
		if !ok {
			return nil, fmt.Errorf("register models: nil model value")
		}
		models = append(models, model)
	}

	return func(declaredType string) any {
		matches := matchingValues(normalizeType(declaredType), models)
		if len(matches) != 1 {
			return nil
		}
		return matches[0]
	}, nil
}

func bindingsFromLookup(contracts map[string]string, lookup ModelLookupFunc) ([]Binding, error) {
	if lookup == nil {
		return nil, fmt.Errorf("register models: nil model lookup")
	}
	models := make([]Binding, 0, len(contracts))
	for name, declaredType := range contracts {
		if !validModelName(name) {
			return nil, fmt.Errorf("register models: invalid model name %q", name)
		}
		normalizedType := normalizeType(declaredType)
		value := lookup(normalizedType)
		if value == nil {
			return nil, fmt.Errorf("register models: @model %s %s has no matching value", name, declaredType)
		}
		models = append(models, Model(name, value))
	}
	return models, nil
}

type modelValue struct {
	fullName  string
	shortName string
	value     any
}

func newModelValue(value any) (modelValue, bool) {
	typeName, ok := valueTypeName(value)
	if !ok {
		return modelValue{}, false
	}
	return modelValue{
		fullName:  typeName,
		shortName: shortTypeName(typeName),
		value:     value,
	}, true
}

func matchingValues(declaredType string, values []modelValue) []any {
	matches := make([]any, 0, 1)
	for _, value := range values {
		if value.fullName == declaredType {
			matches = append(matches, value.value)
		}
	}
	if len(matches) > 0 {
		return matches
	}

	// Types declared in package main can reflect as "main.Type" at runtime,
	// even when the template contract uses the module import path.
	declaredShortName := shortTypeName(declaredType)
	for _, value := range values {
		if strings.HasPrefix(value.fullName, "main.") && value.shortName == declaredShortName {
			matches = append(matches, value.value)
		}
	}
	return matches
}

// LoadContracts reads template files and returns reusable @model contracts.
//
// Applications can call LoadContracts once at startup and reuse the returned
// value on each render. Changes to template declarations are picked up only
// after calling LoadContracts again.
func LoadContracts(files ...string) (Contracts, error) {
	models, err := ScanContracts(files...)
	if err != nil {
		return Contracts{}, err
	}
	return Contracts{models: models}, nil
}

// ScanContracts reads template files and returns the declared @model contracts.
func ScanContracts(files ...string) (map[string]string, error) {
	contracts := make(map[string]string)
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("register models: read %s: %w", file, err)
		}
		for _, match := range modelPattern.FindAllStringSubmatch(string(data), -1) {
			name := strings.TrimSpace(match[1])
			typeName := normalizeType(match[2])
			if previous, exists := contracts[name]; exists && previous != typeName {
				return nil, fmt.Errorf("register models: @model %s is declared as both %s and %s", name, previous, typeName)
			}
			contracts[name] = typeName
		}
	}
	return contracts, nil
}

func valueTypeName(value any) (string, bool) {
	if value == nil {
		return "", false
	}
	typ := reflect.TypeOf(value)
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Name() == "" || typ.PkgPath() == "" {
		return "", false
	}
	return typ.PkgPath() + "." + typ.Name(), true
}

func normalizeType(typ string) string {
	lastSlash := strings.LastIndex(typ, "/")
	lastDot := strings.LastIndex(typ, ".")
	if lastSlash > lastDot {
		return typ[:lastSlash] + "." + typ[lastSlash+1:]
	}
	return typ
}

func shortTypeName(typ string) string {
	lastDot := strings.LastIndex(typ, ".")
	if lastDot < 0 || lastDot == len(typ)-1 {
		return typ
	}
	return typ[lastDot+1:]
}

func validModelName(name string) bool {
	if name == "" {
		return false
	}
	if reservedTemplateNames[name] {
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

var reservedTemplateNames = map[string]bool{
	"and":      true,
	"block":    true,
	"call":     true,
	"define":   true,
	"else":     true,
	"end":      true,
	"eq":       true,
	"ge":       true,
	"gt":       true,
	"html":     true,
	"if":       true,
	"index":    true,
	"js":       true,
	"le":       true,
	"len":      true,
	"lt":       true,
	"ne":       true,
	"not":      true,
	"or":       true,
	"print":    true,
	"printf":   true,
	"println":  true,
	"range":    true,
	"slice":    true,
	"template": true,
	"urlquery": true,
	"with":     true,
}

var modelPattern = regexp.MustCompile(`(?m)^\s*@model\s+([A-Za-z_][A-Za-z0-9_]*)\s+([A-Za-z_][A-Za-z0-9_./\[\]*-]*)\s*$`)
