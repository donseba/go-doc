package godoccli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

type (
	indexFile struct {
		Version       int                      `json:"version"`
		Module        string                   `json:"module"`
		Templates     map[string]templateIndex `json:"templates"`
		Types         map[string]goTypeIndex   `json:"types"`
		Funcs         map[string]goFuncIndex   `json:"funcs,omitempty"`
		Short         map[string][]string      `json:"short"`
		SymbolAliases map[string]string        `json:"symbolAliases,omitempty"`
		SymbolStrict  bool                     `json:"symbolStrictMode,omitempty"`
		Problems      []problem                `json:"problems,omitempty"`
	}

	templateIndex struct {
		Name   string            `json:"name,omitempty"`
		Roots  map[string]string `json:"roots,omitempty"`
		Dot    string            `json:"dot,omitempty"`
		Funcs  map[string]string `json:"funcs,omitempty"`
		Gens   map[string]string `json:"gens,omitempty"`
		Source string            `json:"source,omitempty"`
		Line   int               `json:"line,omitempty"`
		Column int               `json:"column,omitempty"`
	}

	goTypeIndex struct {
		Name    string                 `json:"name"`
		Package string                 `json:"package"`
		File    string                 `json:"file"`
		Line    int                    `json:"line,omitempty"`
		Column  int                    `json:"column,omitempty"`
		Doc     string                 `json:"doc,omitempty"`
		Fields  map[string]fieldIndex  `json:"fields"`
		Methods map[string]methodIndex `json:"methods,omitempty"`
	}

	fieldIndex struct {
		Type   string `json:"type"`
		Doc    string `json:"doc,omitempty"`
		File   string `json:"file,omitempty"`
		Line   int    `json:"line,omitempty"`
		Column int    `json:"column,omitempty"`
	}

	methodIndex struct {
		Type      string   `json:"type,omitempty"`
		Signature string   `json:"signature,omitempty"`
		Doc       string   `json:"doc,omitempty"`
		File      string   `json:"file,omitempty"`
		Line      int      `json:"line,omitempty"`
		Column    int      `json:"column,omitempty"`
		Params    []string `json:"params,omitempty"`
	}

	goFuncIndex struct {
		Name       string                 `json:"name"`
		Package    string                 `json:"package"`
		File       string                 `json:"file"`
		Line       int                    `json:"line,omitempty"`
		Column     int                    `json:"column,omitempty"`
		Doc        string                 `json:"doc,omitempty"`
		Result     string                 `json:"result,omitempty"`
		Results    []string               `json:"results,omitempty"`
		ReturnOK   bool                   `json:"returnOK,omitempty"`
		Signature  string                 `json:"signature,omitempty"`
		Params     []string               `json:"params,omitempty"`
		Signatures []goFuncSignatureIndex `json:"signatures,omitempty"`
	}

	goFuncSignatureIndex struct {
		Signature string   `json:"signature"`
		Params    []string `json:"params,omitempty"`
		Result    string   `json:"result,omitempty"`
		Results   []string `json:"results,omitempty"`
	}

	problem struct {
		File    string `json:"file,omitempty"`
		Message string `json:"message"`
	}

	indexConfig struct {
		Include           []string                 `json:"include"`
		Exclude           []string                 `json:"exclude"`
		Providers         []string                 `json:"providers"`
		Functions         map[string]string        `json:"functions"`
		FunctionMaps      []string                 `json:"functionMaps"`
		Discover          discoverConfig           `json:"discover"`
		TemplateFunctions []templateFunctionConfig `json:"templateFunctions"`
		SymbolAnnotations []symbolAnnotationConfig `json:"symbolAnnotations"`
		SymbolStrict      bool                     `json:"symbolStrictMode"`
		Enabled           *bool                    `json:"enabled"`
		WriteIndex        bool                     `json:"writeIndex"`
	}

	discoverConfig struct {
		FunctionMaps *bool `json:"functionMaps"`
		Providers    *bool `json:"providers"`
		Signatures   *bool `json:"signatures"`
	}

	templateFunctionConfig struct {
		Name       string   `json:"name"`
		Path       string   `json:"path,omitempty"`
		Signatures []string `json:"signatures,omitempty"`
	}

	symbolAnnotationConfig struct {
		Name string `json:"name"`
		Type string `json:"type,omitempty"`
	}

	typedRoot struct {
		Annotation string
		Name       string
		Type       string
	}

	templateFunctionSource struct {
		Name     string
		Target   string
		FuncMap  string
		File     string
		Priority int
	}

	templateFunctionRegistry struct {
		funcs        map[string]templateFunctionSource
		byPrio       map[int]map[string][]templateFunctionSource
		seenFuncMaps map[string]bool
		problem      []problem
	}
)

func (tmpl templateIndex) typedRootType(name string) (string, string, bool) {
	if typeName := tmpl.Roots[name]; typeName != "" {
		return typeName, "root", true
	}
	return "", "", false
}

func (tmpl templateIndex) typedRootNames() []string {
	names := make([]string, 0, len(tmpl.Roots))
	for name := range tmpl.Roots {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (tmpl templateIndex) hasContracts() bool {
	return len(tmpl.Roots) > 0 || tmpl.Dot != "" || len(tmpl.Funcs) > 0 || len(tmpl.Gens) > 0
}

const (
	goDocSigAnnotation = "go-doc:sig"
	funcMapAnnotation  = "go-doc:funcmap"
	providerAnnotation = "go-doc:provider"

	functionSourceAnnotatedFuncMap = 20
	functionSourceAnnotatedSig     = 20
	functionSourceConfigFuncMap    = 30
	functionSourceConfigFunction   = 40

	funcMapInvalidAnnotationMessage = "go-doc: //go-doc:funcmap must annotate a func or var returning template.FuncMap, map[string]any, or map[string]interface{}"
	funcMapDynamicMessage           = "go-doc: dynamic funcmap construction is not supported yet; use a direct composite literal"
	funcMapDynamicKeyMessage        = "go-doc: funcmap entry uses a dynamic key and was skipped"
	funcMapMissingMessageFormat     = "go-doc: functionMap %s not found"
	funcMapInvalidReturnFormat      = "go-doc: functionMap %s does not return template.FuncMap or map[string]any"
)

var (
	dotPattern                    = regexp.MustCompile(`(?m)^\s*@dot\s+([A-Za-z_][A-Za-z0-9_./\[\]*-]*)\s*$`)
	funcPattern                   = regexp.MustCompile(`(?m)^\s*@func\s+([A-Za-z_][A-Za-z0-9_]*)(?:\s+([A-Za-z_][A-Za-z0-9_./-]*))?\s*$`)
	goDocSigPattern               = regexp.MustCompile(`(?m)^\s*(?://\s*)?` + goDocSigAnnotation + `\s+(.+?)\s*$`)
	funcMapAnnotationPattern      = regexp.MustCompile(`(?m)^\s*(?://\s*)?` + funcMapAnnotation + `\s*$`)
	providerAnnotationPattern     = regexp.MustCompile(`(?m)^\s*(?://\s*)?` + providerAnnotation + `\s+"?([^"\s]+)"?\s*$`)
	genPattern                    = regexp.MustCompile(`(?m)^\s*@gen\s+([A-Za-z_][A-Za-z0-9_]*)\s+([A-Za-z_][A-Za-z0-9_./-]*)\s*$`)
	annotationPattern             = regexp.MustCompile(`(?m)^\s*@([A-Za-z_][A-Za-z0-9_]*)\s+([A-Za-z_][A-Za-z0-9_]*)(?:\s+([A-Za-z_][A-Za-z0-9_./\[\]*-]*))?\s*$`)
	templateCommentPattern        = regexp.MustCompile(`(?s)\{\{/\*(.*?)\*/\}\}`)
	definePattern                 = regexp.MustCompile(`(?s)\{\{\s*(?:-)?\s*define\s+"([^"]+)"\s*(?:-)?\s*\}\}(.*?)\{\{\s*(?:-)?\s*end\s*(?:-)?\s*\}\}`)
	leadingTemplateCommentPattern = regexp.MustCompile(`(?s)\{\{/\*.*?\*/\}\}\s*$`)
)

var ReservedTemplateNames = map[string]bool{
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

var defaultSkippedDirs = map[string]bool{
	".git":         true,
	".idea":        true,
	"node_modules": true,
	"references":   true,
	"dist":         true,
	"build":        true,
	"out":          true,
}

func Main() {
	if err := Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func Run(args []string) error {
	if len(args) == 0 {
		return usage()
	}

	switch args[0] {
	case "version", "--version", "-version":
		_, _ = fmt.Fprintln(os.Stdout, Version)
		return nil

	case "types":
		fs := flag.NewFlagSet("types", flag.ExitOnError)
		query := fs.String("query", "", "filter structs by name or fully qualified type")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		root := argRoot(fs.Args())
		idx, err := buildIndex(root)
		if err != nil {
			return err
		}
		if *query != "" {
			filterTypes(idx, *query)
		}
		return writeJSON(idx.Types, "")

	case "templates":
		fs := flag.NewFlagSet("templates", flag.ExitOnError)
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		root := argRoot(fs.Args())
		idx, err := buildIndex(root)
		if err != nil {
			return err
		}
		return writeJSON(idx.Templates, "")

	case "index":
		fs := flag.NewFlagSet("index", flag.ExitOnError)
		out := fs.String("o", "", "write JSON to this file instead of stdout")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		root := argRoot(fs.Args())
		idx, needed, err := buildTemplateIndex(root)
		if err != nil {
			return err
		}
		if !needed {
			if *out != "" {
				if err := os.Remove(*out); err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
			fmt.Fprintln(os.Stderr, "go-doc: no template contracts found; index not written")
			return nil
		}
		return writeJSON(idx, *out)

	case "lsp":
		fs := flag.NewFlagSet("lsp", flag.ExitOnError)
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		root := argRoot(fs.Args())
		return runLSP(os.Stdin, os.Stdout, root)

	default:
		return usage()
	}
}

func usage() error {
	return errors.New(`usage:
  go-doc types [-query Todo] [root]
  go-doc templates [root]
  go-doc index [-o .go-doc/index.json] [root]
  go-doc lsp [root]
  go-doc version`)
}

func argRoot(args []string) string {
	if len(args) == 0 {
		return "."
	}
	return args[0]
}

func buildIndex(root string) (indexFile, error) {
	idx, _, err := buildIndexWithMode(root, false)
	return idx, err
}

func buildTemplateIndex(root string) (indexFile, bool, error) {
	return buildIndexWithMode(root, true)
}

func buildIndexWithMode(root string, requireTemplateContracts bool) (indexFile, bool, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return indexFile{}, false, err
	}

	idx := indexFile{
		Version:   2,
		Templates: make(map[string]templateIndex),
		Types:     make(map[string]goTypeIndex),
		Funcs:     make(map[string]goFuncIndex),
		Short:     make(map[string][]string),
	}

	cfg := loadIndexConfig(absRoot)
	if !cfg.enabled() {
		return idx, false, nil
	}
	idx.SymbolAliases = symbolAliases(cfg)
	idx.SymbolStrict = cfg.SymbolStrict

	module, err := readModulePath(absRoot)
	if err != nil {
		return indexFile{}, false, err
	}
	idx.Module = module

	if requireTemplateContracts {
		if err := scanTemplates(absRoot, cfg, configuredFunctionMap(cfg), &idx); err != nil {
			return indexFile{}, false, err
		}
		needed := hasTemplateContracts(idx)
		if !needed && len(cfg.FunctionMaps) == 0 && len(providerPatterns(absRoot, cfg)) == 0 && !hasAnnotatedMetadataFiles(absRoot, cfg) {
			return idx, false, nil
		}
		idx.Templates = make(map[string]templateIndex)
	}

	funcs := newTemplateFunctionRegistry()
	if err := scanGoTypes(absRoot, cfg, &idx, funcs); err != nil {
		return indexFile{}, false, err
	}
	if err := scanConfiguredTemplateFunctionPackages(absRoot, cfg, &idx, funcs); err != nil {
		return indexFile{}, false, err
	}
	applyConfiguredTemplateFunctions(cfg, &idx)
	addConfiguredTemplateFunctionDefaults(cfg, funcs)
	idx.Problems = append(idx.Problems, funcs.problem...)

	if err := scanTemplates(absRoot, cfg, funcs.functionMap(), &idx); err != nil {
		return indexFile{}, false, err
	}
	needed := hasTemplateContracts(idx)
	if requireTemplateContracts && !needed {
		return idx, false, nil
	}
	sortShortNames(idx.Short)
	validateTemplateTypes(&idx)

	return idx, needed, nil
}

func readModulePath(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module ")), nil
		}
	}
	return "", errors.New("go.mod has no module line")
}

func scanGoTypes(root string, cfg indexConfig, idx *indexFile, funcs *templateFunctionRegistry) error {
	fileSet := token.NewFileSet()
	loaded, err := packages.Load(&packages.Config{
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedCompiledGoFiles |
			packages.NeedSyntax |
			packages.NeedTypes |
			packages.NeedTypesInfo |
			packages.NeedModule,
		Dir:  root,
		Fset: fileSet,
	}, "./...")
	if err != nil {
		return err
	}

	for _, pkg := range loaded {
		if len(pkg.Errors) > 0 {
			for _, pkgErr := range pkg.Errors {
				idx.Problems = append(idx.Problems, problem{File: pkgErr.Pos, Message: pkgErr.Msg})
			}
		}
		if pkg.Types == nil || !shouldIndexPackage(root, pkg, cfg) {
			continue
		}
		indexPackageTypes(root, fileSet, pkg, idx, cfg)
		discoverAnnotatedTemplateFunctionSignatures(root, fileSet, pkg, cfg, idx, funcs)
		discoverAnnotatedFuncMaps(root, fileSet, pkg, cfg, idx, funcs)
	}
	return nil
}

func scanConfiguredTemplateFunctionPackages(root string, cfg indexConfig, idx *indexFile, funcs *templateFunctionRegistry) error {
	packagesToLoad := configuredTemplateFunctionPackages(root, cfg)
	if len(packagesToLoad) == 0 {
		reportMissingConfiguredFuncMaps(cfg, funcs)
		return nil
	}
	fileSet := token.NewFileSet()
	loaded, err := packages.Load(&packages.Config{
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedCompiledGoFiles |
			packages.NeedSyntax |
			packages.NeedTypes |
			packages.NeedTypesInfo |
			packages.NeedModule,
		Dir:  root,
		Fset: fileSet,
	}, packagesToLoad...)
	if err != nil {
		return err
	}
	for _, pkg := range loaded {
		if len(pkg.Errors) > 0 {
			for _, pkgErr := range pkg.Errors {
				idx.Problems = append(idx.Problems, problem{File: pkgErr.Pos, Message: pkgErr.Msg})
			}
		}
		if pkg.Types == nil {
			continue
		}
		indexPackageTypes(root, fileSet, pkg, idx, cfg)
		discoverAnnotatedTemplateFunctionSignatures(root, fileSet, pkg, cfg, idx, funcs)
		discoverAnnotatedFuncMaps(root, fileSet, pkg, cfg, idx, funcs)
		extractConfiguredFuncMaps(root, fileSet, pkg, cfg, idx, funcs)
	}
	reportMissingConfiguredFuncMaps(cfg, funcs)
	return nil
}

func configuredTemplateFunctionPackages(root string, cfg indexConfig) []string {
	seen := make(map[string]bool)
	var packages []string
	add := func(pkgPath string) {
		pkgPath = strings.TrimSpace(pkgPath)
		if pkgPath == "" || seen[pkgPath] {
			return
		}
		seen[pkgPath] = true
		packages = append(packages, pkgPath)
	}
	for _, fn := range cfg.TemplateFunctions {
		if len(fn.Signatures) > 0 {
			continue
		}
		pkgPath := packagePathFromQualifiedName(normalizeType(strings.TrimSpace(fn.Path)))
		if pkgPath == "" || strings.HasPrefix(pkgPath, "$go-doc/") {
			continue
		}
		add(pkgPath)
	}
	for _, funcMap := range cfg.FunctionMaps {
		pkgPath := packagePathFromQualifiedName(normalizeType(strings.TrimSpace(funcMap)))
		if pkgPath == "" {
			continue
		}
		add(pkgPath)
	}
	for _, provider := range providerPatterns(root, cfg) {
		add(provider)
	}
	sort.Strings(packages)
	return packages
}

func providerPatterns(root string, cfg indexConfig) []string {
	seen := make(map[string]bool)
	var providers []string
	add := func(provider string) {
		provider = strings.TrimSpace(provider)
		if provider == "" || seen[provider] {
			return
		}
		seen[provider] = true
		providers = append(providers, provider)
	}
	for _, provider := range cfg.Providers {
		add(provider)
	}
	if root != "" && cfg.discoverProviders() {
		for _, provider := range annotatedProviderPatterns(root, cfg) {
			add(provider)
		}
	}
	return providers
}

func annotatedProviderPatterns(root string, cfg indexConfig) []string {
	var providers []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if shouldSkipDir(root, path, d.Name(), cfg) {
				return filepath.SkipDir
			}
			return nil
		}
		if !shouldIncludePath(root, path, cfg) || filepath.Ext(path) != ".go" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range providerAnnotationPattern.FindAllStringSubmatch(string(data), -1) {
			providers = append(providers, match[1])
		}
		return nil
	})
	return providers
}

func shouldIndexPackage(root string, pkg *packages.Package, cfg indexConfig) bool {
	for _, file := range pkg.GoFiles {
		if shouldIncludePath(root, file, cfg) {
			return true
		}
	}
	for _, file := range pkg.CompiledGoFiles {
		if shouldIncludePath(root, file, cfg) {
			return true
		}
	}
	return false
}

func indexPackageTypes(root string, fileSet *token.FileSet, pkg *packages.Package, idx *indexFile, cfg indexConfig) {
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.GenDecl:
				if decl.Tok == token.TYPE {
					indexPackageTypeDecl(root, fileSet, pkg, decl, idx)
				}
			case *ast.FuncDecl:
				indexPackageFuncDecl(root, fileSet, pkg, decl, idx, cfg)
			}
		}
	}
}

func discoverAnnotatedFuncMaps(root string, fileSet *token.FileSet, pkg *packages.Package, cfg indexConfig, idx *indexFile, funcs *templateFunctionRegistry) {
	if !cfg.discoverFunctionMaps() {
		return
	}
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				if !hasFuncMapAnnotation(decl.Doc) {
					continue
				}
				extractFuncMapFromFuncDecl(root, fileSet, pkg, idx, funcs, decl, functionSourceAnnotatedFuncMap)
			case *ast.GenDecl:
				declAnnotated := hasFuncMapAnnotation(decl.Doc)
				if declAnnotated && decl.Tok != token.VAR {
					pos := fileSet.Position(decl.Pos())
					funcs.problem = append(funcs.problem, problem{
						File:    rel(root, pos.Filename),
						Message: funcMapInvalidAnnotationMessage,
					})
					continue
				}
				if decl.Tok != token.VAR {
					continue
				}
				for _, spec := range decl.Specs {
					valueSpec, ok := spec.(*ast.ValueSpec)
					if !ok || (!declAnnotated && !hasFuncMapAnnotation(valueSpec.Doc)) {
						continue
					}
					extractFuncMapFromValueSpec(root, fileSet, pkg, idx, funcs, valueSpec, functionSourceAnnotatedFuncMap, "")
				}
			}
		}
	}
}

func discoverAnnotatedTemplateFunctionSignatures(root string, fileSet *token.FileSet, pkg *packages.Package, cfg indexConfig, idx *indexFile, funcs *templateFunctionRegistry) {
	if !cfg.discoverSignatures() {
		return
	}
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			decl, ok := decl.(*ast.FuncDecl)
			if !ok || decl.Body == nil {
				continue
			}
			for _, stmt := range decl.Body.List {
				signatures := parseGoDocSignatures(statementLeadingComment(fileSet, file, stmt))
				if len(signatures) == 0 {
					continue
				}
				for _, name := range assignedTemplateFunctionNames(stmt) {
					indexAnnotatedTemplateFunctionSignature(root, fileSet, pkg, idx, funcs, stmt, name, signatures)
				}
			}
		}
	}
}

func indexAnnotatedTemplateFunctionSignature(root string, fileSet *token.FileSet, pkg *packages.Package, idx *indexFile, funcs *templateFunctionRegistry, stmt ast.Stmt, name string, signatures []goFuncSignatureIndex) {
	target := virtualPackageTemplateFunctionPath(pkg.PkgPath, name)
	pos := fileSet.Position(stmt.Pos())
	fn := idx.Funcs[target]
	if fn.Name == "" {
		fn.Name = name
	}
	if fn.Package == "" {
		fn.Package = pkg.PkgPath
	}
	fn.File = rel(root, pos.Filename)
	fn.Line = pos.Line
	fn.Column = pos.Column
	fn.Signatures = signatures
	fn.Signature = signatures[0].Signature
	fn.Params = signatures[0].Params
	fn.Result = signatures[0].Result
	fn.Results = signatures[0].Results
	fn.ReturnOK = len(fn.Results) > 0 || fn.Result != ""
	idx.Funcs[target] = fn
	funcs.add(templateFunctionSource{
		Name:     name,
		Target:   target,
		File:     rel(root, pos.Filename),
		Priority: functionSourceAnnotatedSig,
	})
}

func assignedTemplateFunctionNames(stmt ast.Stmt) []string {
	assign, ok := stmt.(*ast.AssignStmt)
	if !ok {
		return nil
	}
	var names []string
	for _, lhs := range assign.Lhs {
		index, ok := lhs.(*ast.IndexExpr)
		if !ok {
			continue
		}
		name, ok := staticStringKey(index.Index)
		if ok {
			names = append(names, name)
		}
	}
	return names
}

func statementLeadingComment(fileSet *token.FileSet, file *ast.File, stmt ast.Stmt) string {
	stmtPos := fileSet.Position(stmt.Pos())
	var selected *ast.CommentGroup
	for _, group := range file.Comments {
		end := fileSet.Position(group.End())
		if end.Filename != stmtPos.Filename || end.Line >= stmtPos.Line {
			continue
		}
		if selected == nil || group.End() > selected.End() {
			selected = group
		}
	}
	if selected == nil {
		return ""
	}
	end := fileSet.Position(selected.End())
	if end.Line+1 < stmtPos.Line {
		return ""
	}
	return selected.Text()
}

func extractConfiguredFuncMaps(root string, fileSet *token.FileSet, pkg *packages.Package, cfg indexConfig, idx *indexFile, funcs *templateFunctionRegistry) {
	for _, raw := range cfg.FunctionMaps {
		funcMap := normalizeType(strings.TrimSpace(raw))
		if funcMap == "" || packagePathFromQualifiedName(funcMap) != pkg.PkgPath {
			continue
		}
		symbol := symbolNameFromQualifiedName(funcMap)
		found := false
		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				switch decl := decl.(type) {
				case *ast.FuncDecl:
					if decl.Name.Name != symbol {
						continue
					}
					found = true
					extractFuncMapFromFuncDecl(root, fileSet, pkg, idx, funcs, decl, functionSourceConfigFuncMap)
				case *ast.GenDecl:
					if decl.Tok != token.VAR {
						continue
					}
					for _, spec := range decl.Specs {
						valueSpec, ok := spec.(*ast.ValueSpec)
						if !ok || !valueSpecHasName(valueSpec, symbol) {
							continue
						}
						found = true
						extractFuncMapFromValueSpec(root, fileSet, pkg, idx, funcs, valueSpec, functionSourceConfigFuncMap, symbol)
					}
				}
			}
		}
		if found {
			funcs.seenFuncMaps[funcMap] = true
		}
	}
}

func reportMissingConfiguredFuncMaps(cfg indexConfig, funcs *templateFunctionRegistry) {
	for _, raw := range cfg.FunctionMaps {
		funcMap := normalizeType(strings.TrimSpace(raw))
		if funcMap == "" || funcs.seenFuncMaps[funcMap] {
			continue
		}
		funcs.problem = append(funcs.problem, problem{Message: fmt.Sprintf(funcMapMissingMessageFormat, funcMap)})
	}
}

func extractFuncMapFromFuncDecl(root string, fileSet *token.FileSet, pkg *packages.Package, idx *indexFile, funcs *templateFunctionRegistry, decl *ast.FuncDecl, priority int) {
	obj, _ := pkg.TypesInfo.Defs[decl.Name].(*types.Func)
	funcMapName := qualifiedObjectName(obj)
	if funcMapName == "" && pkg.Types != nil {
		funcMapName = pkg.Types.Path() + "." + decl.Name.Name
	}
	pos := fileSet.Position(decl.Name.Pos())
	if obj == nil || !funcMapLikeFunc(obj.Type()) {
		funcs.problem = append(funcs.problem, problem{
			File:    rel(root, pos.Filename),
			Message: fmt.Sprintf(funcMapInvalidReturnFormat, funcMapName),
		})
		return
	}
	lit := directFuncMapReturnLiteral(decl)
	if lit == nil {
		funcs.problem = append(funcs.problem, problem{
			File:    rel(root, pos.Filename),
			Message: funcMapDynamicMessage,
		})
		return
	}
	extractFuncMapLiteral(root, fileSet, pkg, idx, funcs, funcMapName, lit, priority)
	funcs.seenFuncMaps[funcMapName] = true
}

func extractFuncMapFromValueSpec(root string, fileSet *token.FileSet, pkg *packages.Package, idx *indexFile, funcs *templateFunctionRegistry, spec *ast.ValueSpec, priority int, onlyName string) {
	for i, name := range spec.Names {
		if onlyName != "" && name.Name != onlyName {
			continue
		}
		obj := pkg.TypesInfo.Defs[name]
		funcMapName := qualifiedObjectName(obj)
		if funcMapName == "" && pkg.Types != nil {
			funcMapName = pkg.Types.Path() + "." + name.Name
		}
		pos := fileSet.Position(name.Pos())
		var value ast.Expr
		if i < len(spec.Values) {
			value = spec.Values[i]
		} else if len(spec.Values) == 1 {
			value = spec.Values[0]
		}
		lit, ok := value.(*ast.CompositeLit)
		if !ok || lit == nil {
			funcs.problem = append(funcs.problem, problem{
				File:    rel(root, pos.Filename),
				Message: funcMapDynamicMessage,
			})
			continue
		}
		if !funcMapLikeType(pkg.TypesInfo.TypeOf(lit)) && !funcMapLikeType(pkg.TypesInfo.TypeOf(spec.Type)) {
			funcs.problem = append(funcs.problem, problem{
				File:    rel(root, pos.Filename),
				Message: funcMapInvalidAnnotationMessage,
			})
			continue
		}
		extractFuncMapLiteral(root, fileSet, pkg, idx, funcs, funcMapName, lit, priority)
		funcs.seenFuncMaps[funcMapName] = true
	}
}

func extractFuncMapLiteral(root string, fileSet *token.FileSet, pkg *packages.Package, idx *indexFile, funcs *templateFunctionRegistry, funcMapName string, lit *ast.CompositeLit, priority int) {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := staticStringKey(kv.Key)
		if !ok {
			pos := fileSet.Position(kv.Key.Pos())
			funcs.problem = append(funcs.problem, problem{
				File:    rel(root, pos.Filename),
				Message: funcMapDynamicKeyMessage,
			})
			continue
		}
		targetObj := functionObjectForExpr(pkg.TypesInfo, kv.Value)
		target := qualifiedObjectName(targetObj)
		if target == "" {
			continue
		}
		ensureFunctionTargetIndexed(root, fileSet, pkg, idx, targetObj)
		pos := fileSet.Position(kv.Pos())
		funcs.add(templateFunctionSource{
			Name:     key,
			Target:   target,
			FuncMap:  funcMapName,
			File:     rel(root, pos.Filename),
			Priority: priority,
		})
	}
}

func directFuncMapReturnLiteral(decl *ast.FuncDecl) *ast.CompositeLit {
	if decl.Body == nil {
		return nil
	}
	for _, stmt := range decl.Body.List {
		ret, ok := stmt.(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			continue
		}
		lit, ok := ret.Results[0].(*ast.CompositeLit)
		if ok {
			return lit
		}
	}
	return nil
}

func hasFuncMapAnnotation(group *ast.CommentGroup) bool {
	if group == nil {
		return false
	}
	for _, comment := range group.List {
		if funcMapAnnotationPattern.MatchString(comment.Text) {
			return true
		}
	}
	return false
}

func funcMapLikeFunc(typ types.Type) bool {
	sig, ok := typ.(*types.Signature)
	if !ok || sig.Results().Len() != 1 {
		return false
	}
	return funcMapLikeType(sig.Results().At(0).Type())
}

func funcMapLikeType(typ types.Type) bool {
	if typ == nil {
		return false
	}
	if named, ok := types.Unalias(typ).(*types.Named); ok {
		obj := named.Obj()
		if obj != nil && obj.Pkg() != nil && obj.Name() == "FuncMap" {
			switch obj.Pkg().Path() {
			case "html/template", "text/template":
				return true
			}
		}
	}
	m, ok := types.Unalias(typ).Underlying().(*types.Map)
	if !ok {
		return false
	}
	key, ok := types.Unalias(m.Key()).(*types.Basic)
	if !ok || key.Kind() != types.String {
		return false
	}
	iface, ok := types.Unalias(m.Elem()).Underlying().(*types.Interface)
	return ok && iface.NumMethods() == 0
}

func staticStringKey(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	return value, err == nil
}

func ensureFunctionTargetIndexed(root string, fileSet *token.FileSet, pkg *packages.Package, idx *indexFile, obj types.Object) {
	fn, ok := obj.(*types.Func)
	if !ok || qualifiedObjectName(fn) == "" {
		return
	}
	fullName := qualifiedObjectName(fn)
	if _, ok := idx.Funcs[fullName]; ok {
		return
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return
	}
	results := signatureResults(sig, pkg.Types)
	position := fileSet.Position(fn.Pos())
	idx.Funcs[fullName] = goFuncIndex{
		Name:      fn.Name(),
		Package:   fn.Pkg().Path(),
		File:      rel(root, position.Filename),
		Line:      position.Line,
		Column:    position.Column,
		Result:    templateValueResultType(results),
		Results:   results,
		ReturnOK:  true,
		Signature: types.TypeString(sig, typeQualifier(pkg.Types)),
		Params:    signatureParams(sig, pkg.Types),
	}
}

func functionObjectForExpr(info *types.Info, expr ast.Expr) types.Object {
	switch expr := expr.(type) {
	case *ast.Ident:
		return info.Uses[expr]
	case *ast.SelectorExpr:
		return info.Uses[expr.Sel]
	default:
		return nil
	}
}

func valueSpecHasName(spec *ast.ValueSpec, name string) bool {
	for _, ident := range spec.Names {
		if ident.Name == name {
			return true
		}
	}
	return false
}

func symbolNameFromQualifiedName(name string) string {
	if dot := strings.LastIndex(name, "."); dot >= 0 {
		return name[dot+1:]
	}
	return name
}

func indexPackageTypeDecl(root string, fileSet *token.FileSet, pkg *packages.Package, decl *ast.GenDecl, idx *indexFile) {
	for _, spec := range decl.Specs {
		typeSpec, ok := spec.(*ast.TypeSpec)
		if !ok || !typeSpec.Name.IsExported() {
			continue
		}
		obj, ok := pkg.TypesInfo.Defs[typeSpec.Name].(*types.TypeName)
		if !ok || obj.Pkg() == nil {
			continue
		}
		position := fileSet.Position(typeSpec.Name.Pos())
		fullName := qualifiedObjectName(obj)
		indexed := goTypeIndex{
			Name:    obj.Name(),
			Package: obj.Pkg().Path(),
			File:    rel(root, position.Filename),
			Line:    position.Line,
			Column:  position.Column,
			Doc:     docText(firstDoc(typeSpec.Doc, decl.Doc)),
			Methods: make(map[string]methodIndex),
		}
		if named, structType := namedStruct(obj.Type()); named != nil && structType != nil {
			indexed.Fields = exportedTypedFields(root, fileSet, pkg, idx, structType, typeSpec)
		} else if iface := namedInterface(obj.Type()); iface != nil {
			indexed.Fields = map[string]fieldIndex{}
			addMethodSet(root, fileSet, idx, pkg.Types, indexed.Methods, types.NewMethodSet(obj.Type()), nil)
		} else {
			continue
		}
		idx.Types[fullName] = indexed
		idx.Short[obj.Name()] = append(idx.Short[obj.Name()], fullName)
	}
}

func indexPackageFuncDecl(root string, fileSet *token.FileSet, pkg *packages.Package, fn *ast.FuncDecl, idx *indexFile, cfg indexConfig) {
	if !fn.Name.IsExported() {
		return
	}
	obj, ok := pkg.TypesInfo.Defs[fn.Name].(*types.Func)
	if !ok || obj.Pkg() == nil {
		return
	}
	sig, ok := obj.Type().(*types.Signature)
	if !ok {
		return
	}
	position := fileSet.Position(fn.Name.Pos())
	if sig.Recv() != nil {
		receiver := receiverNamed(sig.Recv().Type())
		if receiver == nil || receiver.Obj().Pkg() == nil {
			return
		}
		fullTypeName := qualifiedObjectName(receiver.Obj())
		typ, ok := idx.Types[fullTypeName]
		if !ok {
			return
		}
		if typ.Methods == nil {
			typ.Methods = make(map[string]methodIndex)
		}
		results := signatureResults(sig, pkg.Types)
		methodDoc := docText(fn.Doc)
		typ.Methods[obj.Name()] = methodIndex{
			Type:      templateValueResultType(results),
			Signature: types.TypeString(sig, typeQualifier(pkg.Types)),
			Doc:       stripGoDocSignatureDocs(methodDoc),
			File:      rel(root, position.Filename),
			Line:      position.Line,
			Column:    position.Column,
			Params:    signatureParams(sig, pkg.Types),
		}
		idx.Types[fullTypeName] = typ
		return
	}
	results := signatureResults(sig, pkg.Types)
	funcDoc := docText(fn.Doc)
	var signatures []goFuncSignatureIndex
	if cfg.discoverSignatures() {
		signatures = parseGoDocSignatures(funcDoc)
	}
	indexReachableTypes(root, fileSet, idx, pkg.Types, sig, nil)
	idx.Funcs[qualifiedObjectName(obj)] = goFuncIndex{
		Name:       obj.Name(),
		Package:    obj.Pkg().Path(),
		File:       rel(root, position.Filename),
		Line:       position.Line,
		Column:     position.Column,
		Doc:        stripGoDocSignatureDocs(funcDoc),
		Result:     templateValueResultType(results),
		Results:    results,
		ReturnOK:   true,
		Signature:  types.TypeString(sig, typeQualifier(pkg.Types)),
		Params:     signatureParams(sig, pkg.Types),
		Signatures: signatures,
	}
}

func applyConfiguredTemplateFunctions(cfg indexConfig, idx *indexFile) {
	for _, fn := range cfg.TemplateFunctions {
		name := strings.TrimSpace(fn.Name)
		if name == "" {
			continue
		}
		path := normalizeType(strings.TrimSpace(fn.Path))
		if path == "" {
			path = virtualTemplateFunctionPath(name)
		}
		existing := idx.Funcs[path]
		if existing.Name == "" {
			existing.Name = name
		}
		if existing.Package == "" && !strings.HasPrefix(path, "$go-doc/") {
			existing.Package = packagePathFromQualifiedName(path)
		}
		if len(fn.Signatures) > 0 {
			existing.Signatures = parseSignatureList(fn.Signatures)
			if len(existing.Signatures) > 0 {
				existing.Signature = existing.Signatures[0].Signature
				existing.Params = existing.Signatures[0].Params
				existing.Result = existing.Signatures[0].Result
				existing.Results = existing.Signatures[0].Results
				existing.ReturnOK = len(existing.Results) > 0 || existing.Result != ""
			}
		}
		idx.Funcs[path] = existing
	}
}

func newTemplateFunctionRegistry() *templateFunctionRegistry {
	return &templateFunctionRegistry{
		funcs:        make(map[string]templateFunctionSource),
		byPrio:       make(map[int]map[string][]templateFunctionSource),
		seenFuncMaps: make(map[string]bool),
	}
}

func (r *templateFunctionRegistry) add(fn templateFunctionSource) {
	fn.Name = strings.TrimSpace(fn.Name)
	fn.Target = normalizeType(strings.TrimSpace(fn.Target))
	if fn.Name == "" || fn.Target == "" {
		return
	}
	if r.byPrio[fn.Priority] == nil {
		r.byPrio[fn.Priority] = make(map[string][]templateFunctionSource)
	}
	peers := r.byPrio[fn.Priority][fn.Name]
	for _, peer := range peers {
		if peer.Target == fn.Target && peer.FuncMap == fn.FuncMap {
			return
		}
	}
	if len(peers) > 0 {
		r.problem = append(r.problem, duplicateFunctionProblem(fn.Name, append(peers, fn)))
	}
	r.byPrio[fn.Priority][fn.Name] = append(peers, fn)
	existing, ok := r.funcs[fn.Name]
	if !ok || fn.Priority >= existing.Priority {
		r.funcs[fn.Name] = fn
	}
}

func (r *templateFunctionRegistry) functionMap() map[string]string {
	if len(r.funcs) == 0 {
		return nil
	}
	out := make(map[string]string, len(r.funcs))
	for name, fn := range r.funcs {
		out[name] = fn.Target
	}
	return out
}

func duplicateFunctionProblem(name string, funcs []templateFunctionSource) problem {
	var labels []string
	file := funcs[len(funcs)-1].File
	for _, fn := range funcs {
		label := fn.FuncMap
		if label == "" {
			label = fn.Target
		}
		if label != "" && !slices.Contains(labels, label) {
			labels = append(labels, label)
		}
	}
	sort.Strings(labels)
	message := fmt.Sprintf("function %q is declared by multiple funcmaps: %s", name, strings.Join(labels, ", "))
	return problem{File: file, Message: message}
}

func addConfiguredTemplateFunctionDefaults(cfg indexConfig, funcs *templateFunctionRegistry) {
	for name, path := range cfg.Functions {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(path) == "" {
			continue
		}
		funcs.add(templateFunctionSource{
			Name:     name,
			Target:   path,
			Priority: functionSourceConfigFunction,
		})
	}
	for _, fn := range cfg.TemplateFunctions {
		name := strings.TrimSpace(fn.Name)
		if name == "" {
			continue
		}
		path := strings.TrimSpace(fn.Path)
		if path == "" {
			path = virtualTemplateFunctionPath(name)
		}
		funcs.add(templateFunctionSource{
			Name:     name,
			Target:   path,
			Priority: functionSourceConfigFunction,
		})
	}
}

func packagePathFromQualifiedName(name string) string {
	if dot := strings.LastIndex(name, "."); dot >= 0 {
		return name[:dot]
	}
	return ""
}

func parseGoDocSignatures(doc string) []goFuncSignatureIndex {
	var signatures []string
	for _, match := range goDocSigPattern.FindAllStringSubmatch(doc, -1) {
		signatures = append(signatures, match[1])
	}
	return parseSignatureList(signatures)
}

func stripGoDocSignatureDocs(doc string) string {
	if strings.TrimSpace(doc) == "" {
		return ""
	}
	var lines []string
	for _, line := range strings.Split(doc, "\n") {
		if goDocSigPattern.MatchString(line) {
			continue
		}
		lines = append(lines, line)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func parseSignatureList(values []string) []goFuncSignatureIndex {
	var out []goFuncSignatureIndex
	for _, value := range values {
		if signature, ok := parseTemplateFunctionSignature(value); ok {
			out = append(out, signature)
		}
	}
	return out
}

func parseTemplateFunctionSignature(value string) (goFuncSignatureIndex, bool) {
	signature := strings.TrimSpace(value)
	if !strings.HasPrefix(signature, "func") {
		return goFuncSignatureIndex{}, false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(signature, "func"))
	if !strings.HasPrefix(rest, "(") {
		return goFuncSignatureIndex{}, false
	}
	close := matchingParenInString(rest, 0)
	if close < 0 {
		return goFuncSignatureIndex{}, false
	}
	params := parseSignatureParams(rest[1:close])
	results := parseSignatureResults(strings.TrimSpace(rest[close+1:]))
	return goFuncSignatureIndex{
		Signature: signature,
		Params:    params,
		Result:    templateValueResultType(results),
		Results:   results,
	}, true
}

func matchingParenInString(value string, open int) int {
	depth := 0
	for i := open; i < len(value); i++ {
		switch value[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func parseSignatureParams(value string) []string {
	parts := splitTopLevelComma(value)
	params := make([]string, 0, len(parts))
	for _, part := range parts {
		typ := signaturePartType(part)
		if typ != "" {
			params = append(params, normalizeType(typ))
		}
	}
	return params
}

func parseSignatureResults(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if strings.HasPrefix(value, "(") {
		if close := matchingParenInString(value, 0); close == len(value)-1 {
			value = value[1:close]
		}
	}
	parts := splitTopLevelComma(value)
	results := make([]string, 0, len(parts))
	for _, part := range parts {
		typ := signaturePartType(part)
		if typ != "" {
			results = append(results, normalizeType(typ))
		}
	}
	return results
}

func signaturePartType(part string) string {
	fields := strings.Fields(strings.TrimSpace(part))
	if len(fields) == 0 {
		return ""
	}
	if len(fields) == 1 {
		return fields[0]
	}
	return fields[len(fields)-1]
}

func splitTopLevelComma(value string) []string {
	var parts []string
	start := 0
	depth := 0
	for i, r := range value {
		switch r {
		case '[', '(', '{':
			depth++
		case ']', ')', '}':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				if part := strings.TrimSpace(value[start:i]); part != "" {
					parts = append(parts, part)
				}
				start = i + len(string(r))
			}
		}
	}
	if part := strings.TrimSpace(value[start:]); part != "" {
		parts = append(parts, part)
	}
	return parts
}

func namedStruct(typ types.Type) (*types.Named, *types.Struct) {
	typ = types.Unalias(typ)
	named, ok := typ.(*types.Named)
	if !ok {
		return nil, nil
	}
	structType, ok := named.Underlying().(*types.Struct)
	if !ok {
		return nil, nil
	}
	return named, structType
}

func namedInterface(typ types.Type) *types.Interface {
	typ = types.Unalias(typ)
	named, ok := typ.(*types.Named)
	if !ok {
		return nil
	}
	iface, ok := named.Underlying().(*types.Interface)
	if !ok {
		return nil
	}
	return iface
}

func receiverNamed(typ types.Type) *types.Named {
	for {
		switch t := types.Unalias(typ).(type) {
		case *types.Pointer:
			typ = t.Elem()
		case *types.Named:
			return t
		default:
			return nil
		}
	}
}

func exportedTypedFields(root string, fileSet *token.FileSet, pkg *packages.Package, idx *indexFile, structType *types.Struct, typeSpec *ast.TypeSpec) map[string]fieldIndex {
	metadata := astFieldMetadata(root, fileSet, typeSpec)
	fields := make(map[string]fieldIndex)
	for i := range structType.NumFields() {
		field := structType.Field(i)
		if !field.Exported() {
			continue
		}
		meta := metadata[field.Name()]
		if meta.File == "" {
			position := fileSet.Position(field.Pos())
			meta.File = rel(root, position.Filename)
			meta.Line = position.Line
			meta.Column = position.Column
		}
		meta.Type = typeString(field.Type(), pkg.Types)
		indexReachableTypes(root, fileSet, idx, pkg.Types, field.Type(), nil)
		fields[field.Name()] = meta
	}
	return fields
}

func indexReachableTypes(root string, fileSet *token.FileSet, idx *indexFile, current *types.Package, typ types.Type, seen map[string]bool) {
	if typ == nil {
		return
	}
	if seen == nil {
		seen = make(map[string]bool)
	}
	switch t := types.Unalias(typ).(type) {
	case *types.Basic:
		return
	case *types.Pointer:
		indexReachableTypes(root, fileSet, idx, current, t.Elem(), seen)
	case *types.Slice:
		indexReachableTypes(root, fileSet, idx, current, t.Elem(), seen)
	case *types.Array:
		indexReachableTypes(root, fileSet, idx, current, t.Elem(), seen)
	case *types.Map:
		indexReachableTypes(root, fileSet, idx, current, t.Key(), seen)
		indexReachableTypes(root, fileSet, idx, current, t.Elem(), seen)
	case *types.Chan:
		indexReachableTypes(root, fileSet, idx, current, t.Elem(), seen)
	case *types.Signature:
		indexReachableTuple(root, fileSet, idx, current, t.Params(), seen)
		indexReachableTuple(root, fileSet, idx, current, t.Results(), seen)
	case *types.Named:
		key := namedTypeSeenKey(t)
		if key != "" {
			if seen[key] {
				return
			}
			seen[key] = true
		}
		indexReachableNamedType(root, fileSet, idx, current, t, seen)
		for i := range t.TypeArgs().Len() {
			indexReachableTypes(root, fileSet, idx, current, t.TypeArgs().At(i), seen)
		}
		indexReachableTypes(root, fileSet, idx, current, t.Underlying(), seen)
	case *types.Struct:
		for i := range t.NumFields() {
			field := t.Field(i)
			if field.Exported() {
				indexReachableTypes(root, fileSet, idx, current, field.Type(), seen)
			}
		}
	case *types.Interface:
		for i := range t.NumExplicitMethods() {
			indexReachableTypes(root, fileSet, idx, current, t.ExplicitMethod(i).Type(), seen)
		}
	}
}

func namedTypeSeenKey(named *types.Named) string {
	if named == nil || named.Obj() == nil {
		return ""
	}
	obj := named.Obj()
	if obj.Pkg() == nil {
		return obj.Name()
	}
	return obj.Pkg().Path() + "." + obj.Name()
}

func indexReachableTuple(root string, fileSet *token.FileSet, idx *indexFile, current *types.Package, tuple *types.Tuple, seen map[string]bool) {
	if tuple == nil {
		return
	}
	for i := range tuple.Len() {
		indexReachableTypes(root, fileSet, idx, current, tuple.At(i).Type(), seen)
	}
}

func indexReachableNamedType(root string, fileSet *token.FileSet, idx *indexFile, current *types.Package, named *types.Named, seen map[string]bool) {
	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil || !obj.Exported() {
		return
	}
	if current != nil && obj.Pkg().Path() == current.Path() {
		return
	}
	key := typeString(named, current)
	if key == "" {
		return
	}

	typ, ok := idx.Types[key]
	if !ok {
		typ = goTypeIndex{
			Name:    obj.Name(),
			Package: obj.Pkg().Path(),
			Fields:  exportedExternalFields(root, fileSet, idx, current, named, seen),
			Methods: make(map[string]methodIndex),
		}
		if typ.Fields == nil {
			typ.Fields = make(map[string]fieldIndex)
		}
	} else if typ.Methods == nil {
		typ.Methods = make(map[string]methodIndex)
	}

	addMethodSet(root, fileSet, idx, current, typ.Methods, types.NewMethodSet(named), seen)
	addMethodSet(root, fileSet, idx, current, typ.Methods, types.NewMethodSet(types.NewPointer(named)), seen)
	idx.Types[key] = typ
	if !slices.Contains(idx.Short[obj.Name()], key) {
		idx.Short[obj.Name()] = append(idx.Short[obj.Name()], key)
	}
}

func exportedExternalFields(root string, fileSet *token.FileSet, idx *indexFile, current *types.Package, named *types.Named, seen map[string]bool) map[string]fieldIndex {
	structType, ok := named.Underlying().(*types.Struct)
	if !ok {
		return nil
	}
	fields := make(map[string]fieldIndex)
	for i := range structType.NumFields() {
		field := structType.Field(i)
		if !field.Exported() {
			continue
		}
		fieldType := typeString(field.Type(), current)
		indexed := fieldIndex{Type: fieldType}
		if position := fileSet.Position(field.Pos()); position.IsValid() && position.Filename != "" {
			indexed.File = rel(root, position.Filename)
			indexed.Line = position.Line
			indexed.Column = position.Column
		}
		fields[field.Name()] = indexed
		indexReachableTypes(root, fileSet, idx, current, field.Type(), seen)
	}
	return fields
}

func addMethodSet(root string, fileSet *token.FileSet, idx *indexFile, current *types.Package, methods map[string]methodIndex, methodSet *types.MethodSet, seen map[string]bool) {
	for i := range methodSet.Len() {
		method, ok := methodSet.At(i).Obj().(*types.Func)
		if !ok || !method.Exported() {
			continue
		}
		if _, exists := methods[method.Name()]; exists {
			continue
		}
		sig, ok := method.Type().(*types.Signature)
		if !ok {
			continue
		}
		results := signatureResults(sig, current)
		indexed := methodIndex{
			Type:      templateValueResultType(results),
			Signature: types.TypeString(sig, typeQualifier(current)),
			Params:    signatureParams(sig, current),
		}
		if position := fileSet.Position(method.Pos()); position.IsValid() && position.Filename != "" {
			indexed.File = rel(root, position.Filename)
			indexed.Line = position.Line
			indexed.Column = position.Column
		}
		methods[method.Name()] = indexed
		indexReachableTypes(root, fileSet, idx, current, sig, seen)
	}
}

func astFieldMetadata(root string, fileSet *token.FileSet, typeSpec *ast.TypeSpec) map[string]fieldIndex {
	structType, ok := typeSpec.Type.(*ast.StructType)
	if !ok {
		return nil
	}
	fields := make(map[string]fieldIndex)
	for _, field := range structType.Fields.List {
		fieldDoc := docText(field.Doc)
		if fieldDoc == "" {
			fieldDoc = docText(field.Comment)
		}
		for _, name := range field.Names {
			if !name.IsExported() {
				continue
			}
			position := fileSet.Position(name.Pos())
			fields[name.Name] = fieldIndex{
				Doc:    fieldDoc,
				File:   rel(root, position.Filename),
				Line:   position.Line,
				Column: position.Column,
			}
		}
	}
	return fields
}

func signatureParams(sig *types.Signature, current *types.Package) []string {
	params := make([]string, 0, sig.Params().Len())
	for i := range sig.Params().Len() {
		paramType := sig.Params().At(i).Type()
		if sig.Variadic() && i == sig.Params().Len()-1 {
			if slice, ok := paramType.(*types.Slice); ok {
				params = append(params, "..."+typeString(slice.Elem(), current))
				continue
			}
		}
		params = append(params, typeString(paramType, current))
	}
	return params
}

func signatureResults(sig *types.Signature, current *types.Package) []string {
	results := make([]string, 0, sig.Results().Len())
	for i := range sig.Results().Len() {
		results = append(results, typeString(sig.Results().At(i).Type(), current))
	}
	return results
}

func typeString(typ types.Type, current *types.Package) string {
	return types.TypeString(types.Unalias(typ), typeQualifier(current))
}

func typeQualifier(current *types.Package) types.Qualifier {
	return func(pkg *types.Package) string {
		if pkg == nil || current != nil && pkg.Path() == current.Path() {
			return ""
		}
		return pkg.Path()
	}
}

func qualifiedObjectName(obj types.Object) string {
	if obj == nil || obj.Pkg() == nil {
		return ""
	}
	return obj.Pkg().Path() + "." + obj.Name()
}

func scanTemplates(root string, cfg indexConfig, defaultFuncs map[string]string, idx *indexFile) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if shouldSkipDir(root, path, d.Name(), cfg) {
				return filepath.SkipDir
			}
			return nil
		}
		if !shouldIncludePath(root, path, cfg) {
			return nil
		}
		if !isTemplateFile(path) {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		src := string(data)
		roots := parseTypedRootMap(src, cfg)
		dot := parseDot(src)
		funcs := contractFuncs(defaultFuncs, parseFuncs(src))
		gens := parseGens(src)
		if len(roots) == 0 && dot == "" && len(funcs) == 0 && len(gens) == 0 {
			scanTemplateDefines(root, path, src, cfg, defaultFuncs, idx)
			return nil
		}

		idx.Templates[rel(root, path)] = templateIndex{
			Roots: roots,
			Dot:   dot,
			Funcs: funcs,
			Gens:  gens,
		}
		scanTemplateDefines(root, path, src, cfg, defaultFuncs, idx)
		return nil
	})
}

func hasAnnotatedMetadataFiles(root string, cfg indexConfig) bool {
	found := false
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || found {
			return err
		}
		if d.IsDir() {
			if shouldSkipDir(root, path, d.Name(), cfg) {
				return filepath.SkipDir
			}
			return nil
		}
		if !shouldIncludePath(root, path, cfg) || filepath.Ext(path) != ".go" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		switch {
		case cfg.discoverFunctionMaps() && funcMapAnnotationPattern.Match(data):
			found = true
		case cfg.discoverProviders() && providerAnnotationPattern.Match(data):
			found = true
		case cfg.discoverSignatures() && goDocSigPattern.Match(data):
			found = true
		}
		return nil
	})
	return found
}

func scanTemplateDefines(root, path, src string, cfg indexConfig, defaultFuncs map[string]string, idx *indexFile) {
	for _, match := range definePattern.FindAllStringSubmatchIndex(src, -1) {
		name := src[match[2]:match[3]]
		body := defineContractText(src, match[0], match[4], match[5])
		roots := parseTypedRootMap(body, cfg)
		dot := parseDot(body)
		funcs := contractFuncs(defaultFuncs, parseFuncs(body))
		gens := parseGens(body)
		if len(roots) == 0 && dot == "" && len(funcs) == 0 && len(gens) == 0 {
			continue
		}
		line, column := lineColumn(src, match[0])
		source := rel(root, path)
		idx.Templates[source+"#"+name] = templateIndex{
			Name:   name,
			Roots:  roots,
			Dot:    dot,
			Funcs:  funcs,
			Gens:   gens,
			Source: source,
			Line:   line,
			Column: column,
		}
	}
}

func defineContractText(src string, defineStart, bodyStart, bodyEnd int) string {
	body := src[bodyStart:bodyEnd]
	if hasContractAnnotations(body) {
		return body
	}
	prefix := src[:defineStart]
	if match := leadingTemplateCommentPattern.FindString(prefix); match != "" {
		return match + "\n" + body
	}
	return body
}

func hasContractAnnotations(src string) bool {
	src = contractScanText(src)
	return len(parseTypedRoots(src, symbolParseConfig{})) > 0 || dotPattern.MatchString(src) || funcPattern.MatchString(src) || genPattern.MatchString(src)
}

func lineColumn(src string, offset int) (int, int) {
	line := 1
	column := 1
	for i, r := range src {
		if i >= offset {
			break
		}
		if r == '\n' {
			line++
			column = 1
			continue
		}
		column++
	}
	return line, column
}

func parseDot(src string) string {
	src = contractScanText(src)
	match := dotPattern.FindStringSubmatch(src)
	if len(match) != 2 {
		return ""
	}
	return normalizeType(match[1])
}

func parseFuncs(src string) map[string]string {
	src = contractScanText(src)
	funcs := make(map[string]string)
	for _, match := range funcPattern.FindAllStringSubmatch(src, -1) {
		funcs[match[1]] = normalizeType(match[2])
	}
	return funcs
}

func parseGens(src string) map[string]string {
	src = contractScanText(src)
	gens := make(map[string]string)
	for _, match := range genPattern.FindAllStringSubmatch(src, -1) {
		gens[match[1]] = strings.TrimSpace(match[2])
	}
	return gens
}

func parseTypedRootMap(src string, cfg indexConfig) map[string]string {
	roots := make(map[string]string)
	for _, root := range parseTypedRoots(src, symbolParseConfig{Aliases: symbolAliases(cfg), Strict: cfg.SymbolStrict}) {
		roots[root.Name] = root.Type
	}
	return roots
}

type symbolParseConfig struct {
	Aliases map[string]string
	Strict  bool
}

func parseTypedRoots(src string, symbols symbolParseConfig) []typedRoot {
	src = contractScanText(src)
	var roots []typedRoot
	for _, match := range annotationPattern.FindAllStringSubmatch(src, -1) {
		annotation, name, explicitType := match[1], match[2], match[3]
		typeName := explicitType
		switch annotation {
		default:
			if reservedContractAnnotation(annotation) {
				continue
			}
			defaultType, known := symbols.Aliases[annotation]
			if !known && symbols.Strict {
				continue
			}
			if typeName == "" && known {
				typeName = defaultType
			}
			if typeName == "" {
				continue
			}
		}
		roots = append(roots, typedRoot{
			Annotation: annotation,
			Name:       name,
			Type:       normalizeType(typeName),
		})
	}
	return roots
}

func symbolAliases(cfg indexConfig) map[string]string {
	if len(cfg.SymbolAnnotations) == 0 {
		return nil
	}
	aliases := make(map[string]string, len(cfg.SymbolAnnotations))
	for _, annotation := range cfg.SymbolAnnotations {
		name := strings.TrimSpace(annotation.Name)
		if name == "" || reservedContractAnnotation(name) {
			continue
		}
		aliases[name] = normalizeType(strings.TrimSpace(annotation.Type))
	}
	return aliases
}

func reservedContractAnnotation(name string) bool {
	switch name {
	case "dot", "func", "gen":
		return true
	default:
		return false
	}
}

func contractFuncs(defaults, local map[string]string) map[string]string {
	if len(defaults) == 0 && len(local) == 0 {
		return nil
	}
	funcs := make(map[string]string, len(defaults)+len(local))
	for name, fn := range defaults {
		funcs[name] = normalizeType(fn)
	}
	for name, fn := range local {
		if fn == "" {
			if defaults[name] != "" {
				funcs[name] = normalizeType(defaults[name])
			}
			continue
		}
		funcs[name] = normalizeType(fn)
	}
	return funcs
}

func configuredFunctionMap(cfg indexConfig) map[string]string {
	if len(cfg.Functions) == 0 && len(cfg.TemplateFunctions) == 0 {
		return nil
	}
	funcs := make(map[string]string, len(cfg.Functions)+len(cfg.TemplateFunctions))
	for name, path := range cfg.Functions {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(path) == "" {
			continue
		}
		funcs[name] = normalizeType(path)
	}
	for _, fn := range cfg.TemplateFunctions {
		name := strings.TrimSpace(fn.Name)
		if name == "" {
			continue
		}
		path := strings.TrimSpace(fn.Path)
		if path == "" {
			path = virtualTemplateFunctionPath(name)
		}
		funcs[name] = normalizeType(path)
	}
	return funcs
}

func virtualTemplateFunctionPath(name string) string {
	return "$go-doc/templatefunc." + name
}

func virtualPackageTemplateFunctionPath(pkgPath, name string) string {
	return "$go-doc/templatefunc." + pkgPath + "." + name
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

func normalizeType(typ string) string {
	lastSlash := strings.LastIndex(typ, "/")
	lastDot := strings.LastIndex(typ, ".")
	if lastSlash > lastDot {
		return typ[:lastSlash] + "." + typ[lastSlash+1:]
	}
	return typ
}

func validateTemplateTypes(idx *indexFile) {
	for file, tmpl := range idx.Templates {
		validateTemplateGens(idx, file, &tmpl)
		for name, typ := range tmpl.Roots {
			if ReservedTemplateNames[name] {
				idx.Problems = append(idx.Problems, problem{File: file, Message: fmt.Sprintf("typed root %s uses a reserved template name", name)})
				continue
			}
			if _, ok := tmpl.Funcs[name]; ok {
				idx.Problems = append(idx.Problems, problem{File: file, Message: fmt.Sprintf("typed root %s collides with @func %s", name, name)})
				continue
			}
			if _, ok := tmpl.Gens[name]; ok && !strings.HasPrefix(typ, genTypePrefix) {
				idx.Problems = append(idx.Problems, problem{File: file, Message: fmt.Sprintf("typed root %s collides with @gen %s", name, name)})
				continue
			}
			if _, ok := idx.Types[typ]; ok {
				continue
			}
			matches := idx.Short[typ]
			switch len(matches) {
			case 0:
				idx.Problems = append(idx.Problems, problem{File: file, Message: fmt.Sprintf("typed root %s references unknown type %q", name, typ)})
			case 1:
				tmpl.Roots[name] = matches[0]
				idx.Templates[file] = tmpl
			default:
				idx.Problems = append(idx.Problems, problem{File: file, Message: fmt.Sprintf("typed root %s type %q is ambiguous: %s", name, typ, strings.Join(matches, ", "))})
			}
		}
		if tmpl.Dot != "" {
			if _, ok := idx.Types[tmpl.Dot]; !ok {
				matches := idx.Short[tmpl.Dot]
				switch len(matches) {
				case 0:
					idx.Problems = append(idx.Problems, problem{File: file, Message: fmt.Sprintf("@dot references unknown type %q", tmpl.Dot)})
				case 1:
					tmpl.Dot = matches[0]
					idx.Templates[file] = tmpl
				default:
					idx.Problems = append(idx.Problems, problem{File: file, Message: fmt.Sprintf("@dot type %q is ambiguous: %s", tmpl.Dot, strings.Join(matches, ", "))})
				}
			}
		}
		for name, fn := range tmpl.Funcs {
			if ReservedTemplateNames[name] {
				idx.Problems = append(idx.Problems, problem{File: file, Message: fmt.Sprintf("@func %s uses a reserved template name", name)})
				continue
			}
			if _, ok := idx.Funcs[fn]; !ok {
				idx.Problems = append(idx.Problems, problem{File: file, Message: fmt.Sprintf("@func %s references unknown function %q", name, fn)})
			}
		}
		idx.Templates[file] = tmpl
	}
}

const genTypePrefix = "$go-doc/gen."

func validateTemplateGens(idx *indexFile, file string, tmpl *templateIndex) {
	if len(tmpl.Gens) == 0 {
		return
	}
	if tmpl.Roots == nil {
		tmpl.Roots = make(map[string]string)
	}
	for name, pkg := range tmpl.Gens {
		if ReservedTemplateNames[name] {
			idx.Problems = append(idx.Problems, problem{File: file, Message: fmt.Sprintf("@gen %s uses a reserved template name", name)})
			continue
		}
		if rootType := tmpl.Roots[name]; rootType != "" && !strings.HasPrefix(rootType, genTypePrefix) {
			idx.Problems = append(idx.Problems, problem{File: file, Message: fmt.Sprintf("@gen %s collides with typed root %s", name, name)})
			continue
		}
		if _, ok := tmpl.Funcs[name]; ok {
			idx.Problems = append(idx.Problems, problem{File: file, Message: fmt.Sprintf("@gen %s collides with @func %s", name, name)})
			continue
		}
		typeName, ok := ensureGeneratedNamespaceType(idx, name, pkg)
		if !ok {
			idx.Problems = append(idx.Problems, problem{File: file, Message: fmt.Sprintf("@gen %s references package %q with no exported functions", name, pkg)})
			continue
		}
		tmpl.Roots[name] = typeName
	}
}

func ensureGeneratedNamespaceType(idx *indexFile, name, pkg string) (string, bool) {
	typeName := genTypePrefix + name
	if typ, ok := idx.Types[typeName]; ok && len(typ.Methods) > 0 {
		return typeName, true
	}

	methods := make(map[string]methodIndex)
	var file string
	for _, fn := range idx.Funcs {
		if fn.Package != pkg {
			continue
		}
		if file == "" || fn.File < file {
			file = fn.File
		}
		methods[fn.Name] = methodIndex{
			Type:      fn.Result,
			Signature: fn.Signature,
			Doc:       fn.Doc,
			File:      fn.File,
			Line:      fn.Line,
			Column:    fn.Column,
			Params:    fn.Params,
		}
	}
	if len(methods) == 0 {
		return "", false
	}

	idx.Types[typeName] = goTypeIndex{
		Name:    name,
		Package: pkg,
		File:    file,
		Line:    1,
		Column:  1,
		Fields:  map[string]fieldIndex{},
		Methods: methods,
	}
	return typeName, true
}

func hasTemplateContracts(idx indexFile) bool {
	for _, tmpl := range idx.Templates {
		if len(tmpl.Roots) > 0 || tmpl.Dot != "" || len(tmpl.Funcs) > 0 || len(tmpl.Gens) > 0 {
			return true
		}
	}
	return false
}

func templateValueResultType(results []string) string {
	switch {
	case len(results) == 1:
		return results[0]
	case len(results) == 2 && results[1] == "error":
		return results[0]
	default:
		return ""
	}
}

func docText(group *ast.CommentGroup) string {
	if group == nil {
		return ""
	}
	return strings.TrimSpace(group.Text())
}

func firstDoc(groups ...*ast.CommentGroup) *ast.CommentGroup {
	for _, group := range groups {
		if group != nil {
			return group
		}
	}
	return nil
}

func filterTypes(idx indexFile, query string) {
	query = strings.ToLower(query)
	for name := range idx.Types {
		if strings.Contains(strings.ToLower(name), query) || strings.Contains(strings.ToLower(idx.Types[name].Name), query) {
			continue
		}
		delete(idx.Types, name)
	}
}

func sortShortNames(short map[string][]string) {
	for name := range short {
		sort.Strings(short[name])
	}
}

func rel(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(relative)
}

func loadIndexConfig(root string) indexConfig {
	cfg := defaultIndexConfig()
	data, err := os.ReadFile(filepath.Join(root, ".go-doc", "config.json"))
	if err != nil {
		return cfg
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return defaultIndexConfig()
	}
	if len(cfg.Include) == 0 {
		cfg.Include = defaultIndexConfig().Include
	}
	return cfg
}

func defaultIndexConfig() indexConfig {
	return indexConfig{
		Include: []string{"/"},
		Exclude: []string{"vendor"},
	}
}

func (cfg indexConfig) enabled() bool {
	return cfg.Enabled == nil || *cfg.Enabled
}

func (cfg indexConfig) discoverFunctionMaps() bool {
	return cfg.Discover.FunctionMaps == nil || *cfg.Discover.FunctionMaps
}

func (cfg indexConfig) discoverProviders() bool {
	return cfg.Discover.Providers == nil || *cfg.Discover.Providers
}

func (cfg indexConfig) discoverSignatures() bool {
	return cfg.Discover.Signatures == nil || *cfg.Discover.Signatures
}

func shouldSkipDir(root, path, name string, cfg indexConfig) bool {
	if defaultSkippedDirs[name] {
		return true
	}
	if path != root && hasGoMod(path) {
		return true
	}
	return !shouldIncludePath(root, path, cfg)
}

func hasGoMod(path string) bool {
	info, err := os.Stat(filepath.Join(path, "go.mod"))
	return err == nil && !info.IsDir()
}

func shouldIncludePath(root, path string, cfg indexConfig) bool {
	relative := filepath.ToSlash(rel(root, path))
	if relative == "." {
		relative = "/"
	}
	for _, excluded := range cfg.Exclude {
		if pathMatches(relative, excluded) {
			return false
		}
	}
	for _, included := range cfg.Include {
		if pathMatches(relative, included) {
			return true
		}
	}
	return false
}

func pathMatches(relative, pattern string) bool {
	pattern = normalizeScanPath(pattern)
	if pattern == "" {
		return false
	}
	if pattern == "/" || pattern == "." {
		return true
	}
	pattern = strings.Trim(pattern, "/")
	relative = strings.Trim(normalizeScanPath(relative), "/")
	return relative == pattern || strings.HasPrefix(relative, pattern+"/")
}

func normalizeScanPath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.ReplaceAll(path, "\\", "/")
	path = filepath.ToSlash(path)
	return path
}

func isTemplateFile(path string) bool {
	switch filepath.Ext(path) {
	case ".gohtml", ".tmpl", ".html":
		return true
	default:
		return false
	}
}

func writeJSON(value any, out string) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if out == "" {
		_, err = os.Stdout.Write(data)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	return os.WriteFile(out, data, 0o644)
}
