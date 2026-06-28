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

	"github.com/donseba/go-doc/internal/godoccli"
)

// Mode controls when template contracts are read.
type Mode string

const (
	// Development scans template files every time Register is called.
	//
	// Use this while editing templates: changing @model Page to @struct Dashboard
	// or any other typed-root declaration is picked up on the next render
	// without restarting the process.
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
	Funcs template.FuncMap
}

// Renderer registers go-doc typed-root accessors for one template set.
//
// Controllers can call Register the same way in development and production; the
// mode only changes when template contracts are scanned.
type Renderer struct {
	mode      Mode
	files     []string
	funcs     template.FuncMap
	contracts Contracts
}

// Binding binds a Go value to a template function name.
//
// For contract-driven rendering, prefer RegisterFromFiles or
// RegisterFromContracts. They register typed-root declarations directly as
// template accessors.
type Binding struct {
	Name  string
	Value any
}

// LookupFunc resolves a value by key. A zero/nil result means not found when
// used with RegisterFromLookup.
type LookupFunc[K comparable, V any] = func(K) V

// RootLookupFunc resolves a declared typed-root type name to a Go value.
//
// The key is the normalized contract type, for example:
//
//	github.com/example/app.Page
type RootLookupFunc = LookupFunc[string, any]

// Contracts contains typed-root declarations scanned from files.
//
// Create it once at startup for production-style rendering, then reuse it for
// each request with Register. Use RegisterFromFiles directly when you want
// development-style rendering that re-reads declarations before each parse.
type Contracts struct {
	roots map[string]string
}

// Root creates a typed-root binding.
func Root(name string, value any) Binding {
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
	r := Renderer{mode: mode, files: files, funcs: maps.Clone(config.Funcs)}

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
		return Renderer{}, fmt.Errorf("register typed roots: unknown renderer mode %q", mode)
	}
}

// Register installs typed-root accessors on tmpl using the configured mode.
func (r Renderer) Register(tmpl *template.Template, values ...any) error {
	if err := UseFuncs(tmpl, r.funcs); err != nil {
		return err
	}
	switch r.mode {
	case Development:
		return RegisterFromFiles(tmpl, values, r.files...)
	case Production:
		return r.contracts.Register(tmpl, values...)
	default:
		return fmt.Errorf("register typed roots: unknown renderer mode %q", r.mode)
	}
}

// UseFuncs installs application-wide template functions on tmpl.
//
// It is a small wrapper around html/template.Funcs that returns panics as
// errors, matching the rest of the renderer API. Call it before parsing
// templates. Renderer.Register calls it automatically for Config.Funcs.
func UseFuncs(tmpl *template.Template, funcs template.FuncMap) (err error) {
	if tmpl == nil {
		return fmt.Errorf("register functions: nil template")
	}
	if len(funcs) == 0 {
		return nil
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("register functions: %v", recovered)
		}
	}()
	tmpl.Funcs(funcs)
	return nil
}

// Register installs typed-root accessors on an existing template.
//
// Call Register before parsing templates, just like html/template.Funcs:
//
//	tmpl := template.New("dashboard.gohtml")
//	err := renderer.Register(tmpl, renderer.Root("Page", page))
//	_, err = tmpl.ParseFiles("templates/dashboard.gohtml")
//
// Register mutates tmpl and only returns an error when a binding is invalid or
// html/template rejects the generated function map.
func Register(tmpl *template.Template, roots ...Binding) (err error) {
	if tmpl == nil {
		return fmt.Errorf("register typed roots: nil template")
	}
	funcs := make(template.FuncMap, len(roots))
	for _, root := range roots {
		name := strings.TrimSpace(root.Name)
		if !validRootName(name) {
			return fmt.Errorf("register typed roots: invalid root name %q", root.Name)
		}
		if _, exists := funcs[name]; exists {
			return fmt.Errorf("register typed roots: duplicate root name %q", name)
		}
		captured := root.Value
		funcs[name] = func() any {
			return captured
		}
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("register typed roots: %v", recovered)
		}
	}()
	tmpl.Funcs(funcs)
	return nil
}

// RegisterFromFiles scans typed-root declarations in template files and registers
// them by matching those declarations to the provided Go values.
//
// The template contract owns the public typed-root name:
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

// RegisterFromContracts registers typed-root accessors from a parsed contract map.
// It is useful when an application already has contract metadata.
func RegisterFromContracts(tmpl *template.Template, contracts map[string]string, values ...any) error {
	return Contracts{roots: maps.Clone(contracts)}.Register(tmpl, values...)
}

// Register registers typed-root accessors from a pre-scanned contract set.
func (contracts Contracts) Register(tmpl *template.Template, values ...any) error {
	lookup, err := NewRootLookup(values...)
	if err != nil {
		return err
	}
	return contracts.RegisterFromLookup(tmpl, lookup)
}

// RegisterFromLookup registers typed-root accessors from a pre-scanned contract set
// using a custom type lookup.
func (contracts Contracts) RegisterFromLookup(tmpl *template.Template, lookup RootLookupFunc) error {
	return RegisterFromLookup(tmpl, contracts.roots, lookup)
}

// Roots returns a copy of the scanned typed-root contract map.
func (contracts Contracts) Roots() map[string]string {
	return maps.Clone(contracts.roots)
}

// RegisterFromLookup registers typed-root accessors using a custom type lookup.
//
// The lookup receives each normalized typed-root type and should return the matching
// value. Return nil when the type cannot be resolved.
func RegisterFromLookup(tmpl *template.Template, contracts map[string]string, lookup RootLookupFunc) error {
	roots, err := bindingsFromLookup(contracts, lookup)
	if err != nil {
		return err
	}
	return Register(tmpl, roots...)
}

// NewRootLookup builds a type lookup from concrete Go values.
func NewRootLookup(values ...any) (RootLookupFunc, error) {
	roots := make([]rootValue, 0, len(values))
	for _, value := range values {
		root, ok := newRootValue(value)
		if !ok {
			return nil, fmt.Errorf("register typed roots: nil root value")
		}
		roots = append(roots, root)
	}

	return func(declaredType string) any {
		matches := matchingValues(normalizeType(declaredType), roots)
		if len(matches) != 1 {
			return nil
		}
		return matches[0]
	}, nil
}

func bindingsFromLookup(contracts map[string]string, lookup RootLookupFunc) ([]Binding, error) {
	if lookup == nil {
		return nil, fmt.Errorf("register typed roots: nil root lookup")
	}
	roots := make([]Binding, 0, len(contracts))
	for name, declaredType := range contracts {
		if !validRootName(name) {
			return nil, fmt.Errorf("register typed roots: invalid root name %q", name)
		}
		normalizedType := normalizeType(declaredType)
		value := lookup(normalizedType)
		if value == nil {
			return nil, fmt.Errorf("register typed roots: %s %s has no matching value", name, declaredType)
		}
		roots = append(roots, Root(name, value))
	}
	return roots, nil
}

type rootValue struct {
	fullName  string
	shortName string
	value     any
}

func newRootValue(value any) (rootValue, bool) {
	typeName, ok := valueTypeName(value)
	if !ok {
		return rootValue{}, false
	}
	return rootValue{
		fullName:  typeName,
		shortName: shortTypeName(typeName),
		value:     value,
	}, true
}

func matchingValues(declaredType string, values []rootValue) []any {
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

// LoadContracts reads template files and returns reusable typed-root contracts.
//
// Applications can call LoadContracts once at startup and reuse the returned
// value on each render. Changes to template declarations are picked up only
// after calling LoadContracts again.
func LoadContracts(files ...string) (Contracts, error) {
	roots, err := ScanContracts(files...)
	if err != nil {
		return Contracts{}, err
	}
	return Contracts{roots: roots}, nil
}

// ScanContracts reads template files and returns the declared typed-root contracts.
func ScanContracts(files ...string) (map[string]string, error) {
	contracts := make(map[string]string)
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("register typed roots: read %s: %w", file, err)
		}
		for _, match := range typedRootPattern.FindAllStringSubmatch(contractScanText(string(data)), -1) {
			annotation := strings.TrimSpace(match[1])
			if reservedContractAnnotation(annotation) {
				continue
			}
			name := strings.TrimSpace(match[2])
			typeName := normalizeType(match[3])
			if previous, exists := contracts[name]; exists && previous != typeName {
				return nil, fmt.Errorf("register typed roots: %s is declared as both %s and %s", name, previous, typeName)
			}
			contracts[name] = typeName
		}
	}
	return contracts, nil
}

func contractScanText(src string) string {
	matches := templateCommentPattern.FindAllStringSubmatch(src, -1)
	if len(matches) == 0 {
		return src
	}
	var out strings.Builder
	for _, match := range matches {
		body := strings.TrimSpace(match[1])
		if body == "" {
			continue
		}
		out.WriteString(body)
		out.WriteByte('\n')
	}
	if out.Len() == 0 {
		return src
	}
	return out.String()
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

func validRootName(name string) bool {
	if name == "" {
		return false
	}
	if godoccli.ReservedTemplateNames[name] {
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

var (
	typedRootPattern       = regexp.MustCompile(`(?m)^\s*@([A-Za-z_][A-Za-z0-9_]*)\s+([A-Za-z_][A-Za-z0-9_]*)\s+([A-Za-z_][A-Za-z0-9_./\[\]*-]*)\s*$`)
	templateCommentPattern = regexp.MustCompile(`(?s)\{\{/\*(.*?)\*/\}\}`)
)

func reservedContractAnnotation(name string) bool {
	switch name {
	case "dot", "func", "gen":
		return true
	default:
		return false
	}
}
