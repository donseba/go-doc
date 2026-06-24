package godoccli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type (
	indexFile struct {
		Version   int                      `json:"version"`
		Module    string                   `json:"module"`
		Templates map[string]templateIndex `json:"templates"`
		Types     map[string]goTypeIndex   `json:"types"`
		Funcs     map[string]goFuncIndex   `json:"funcs,omitempty"`
		Short     map[string][]string      `json:"short"`
		Problems  []problem                `json:"problems,omitempty"`
	}

	templateIndex struct {
		Models    map[string]string `json:"models"`
		Accessors map[string]string `json:"accessors"`
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
		Type      string `json:"type,omitempty"`
		Signature string `json:"signature,omitempty"`
		Doc       string `json:"doc,omitempty"`
		File      string `json:"file,omitempty"`
		Line      int    `json:"line,omitempty"`
		Column    int    `json:"column,omitempty"`
	}

	goFuncIndex struct {
		Name    string `json:"name"`
		Package string `json:"package"`
		File    string `json:"file"`
		Line    int    `json:"line,omitempty"`
		Column  int    `json:"column,omitempty"`
		Doc     string `json:"doc,omitempty"`
	}

	problem struct {
		File    string `json:"file,omitempty"`
		Message string `json:"message"`
	}

	indexConfig struct {
		Include []string `json:"include"`
		Exclude []string `json:"exclude"`
	}
)

var (
	modelPattern = regexp.MustCompile(`(?m)^\s*@model\s+([A-Za-z][A-Za-z0-9_]*)\s+([A-Za-z_][A-Za-z0-9_./\[\]*-]*)\s*$`)
)

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
			fmt.Fprintln(os.Stderr, "go-doc: no @model annotations found; index not written")
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
  go-doc lsp [root]`)
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

func buildIndexWithMode(root string, requireTemplateModels bool) (indexFile, bool, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return indexFile{}, false, err
	}

	module, err := readModulePath(absRoot)
	if err != nil {
		return indexFile{}, false, err
	}

	idx := indexFile{
		Version:   2,
		Module:    module,
		Templates: make(map[string]templateIndex),
		Types:     make(map[string]goTypeIndex),
		Funcs:     make(map[string]goFuncIndex),
		Short:     make(map[string][]string),
	}

	cfg := loadIndexConfig(absRoot)
	if err := scanTemplates(absRoot, cfg, &idx); err != nil {
		return indexFile{}, false, err
	}
	needed := hasTemplateModels(idx)
	if requireTemplateModels && !needed {
		return idx, false, nil
	}
	if err := scanGoTypes(absRoot, module, cfg, &idx); err != nil {
		return indexFile{}, false, err
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

func scanGoTypes(root, module string, cfg indexConfig, idx *indexFile) error {
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
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		fileSet := token.NewFileSet()
		file, err := parser.ParseFile(fileSet, path, nil, parser.ParseComments)
		if err != nil {
			idx.Problems = append(idx.Problems, problem{File: rel(root, path), Message: err.Error()})
			return nil
		}

		importPath := moduleImportPath(root, module, filepath.Dir(path))
		for _, decl := range file.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.TYPE {
				continue
			}
			for _, spec := range genDecl.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if !typeSpec.Name.IsExported() {
					continue
				}
				structType, ok := typeSpec.Type.(*ast.StructType)
				if !ok {
					continue
				}

				fullName := importPath + "." + typeSpec.Name.Name
				position := fileSet.Position(typeSpec.Name.Pos())
				idx.Types[fullName] = goTypeIndex{
					Name:    typeSpec.Name.Name,
					Package: importPath,
					File:    rel(root, path),
					Line:    position.Line,
					Column:  position.Column,
					Doc:     docText(firstDoc(typeSpec.Doc, genDecl.Doc)),
					Fields:  exportedFields(root, path, fileSet, structType),
					Methods: make(map[string]methodIndex),
				}
				idx.Short[typeSpec.Name.Name] = append(idx.Short[typeSpec.Name.Name], fullName)
			}
		}

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !fn.Name.IsExported() {
				continue
			}
			position := fileSet.Position(fn.Name.Pos())
			if fn.Recv != nil {
				typeName := receiverTypeName(fileSet, fn.Recv)
				if typeName == "" {
					continue
				}
				fullTypeName := importPath + "." + typeName
				typ := idx.Types[fullTypeName]
				if typ.Methods == nil {
					typ.Methods = make(map[string]methodIndex)
				}
				typ.Methods[fn.Name.Name] = methodIndex{
					Type:      resultType(fileSet, fn.Type),
					Signature: funcSignature(fileSet, fn.Type),
					Doc:       docText(fn.Doc),
					File:      rel(root, path),
					Line:      position.Line,
					Column:    position.Column,
				}
				idx.Types[fullTypeName] = typ
				continue
			}
			fullName := importPath + "." + fn.Name.Name
			idx.Funcs[fullName] = goFuncIndex{
				Name:    fn.Name.Name,
				Package: importPath,
				File:    rel(root, path),
				Line:    position.Line,
				Column:  position.Column,
				Doc:     docText(fn.Doc),
			}
		}

		return nil
	})
}

func scanTemplates(root string, cfg indexConfig, idx *indexFile) error {
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
		models := parseModels(src)
		if len(models) == 0 {
			return nil
		}

		accessors := make(map[string]string, len(models))
		for name, typ := range models {
			accessors["_"+name] = typ
		}
		idx.Templates[rel(root, path)] = templateIndex{
			Models:    models,
			Accessors: accessors,
		}
		return nil
	})
}

func parseModels(src string) map[string]string {
	models := make(map[string]string)
	for _, match := range modelPattern.FindAllStringSubmatch(src, -1) {
		models[match[1]] = normalizeType(match[2])
	}
	return models
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
		for name, typ := range tmpl.Models {
			if _, ok := idx.Types[typ]; ok {
				continue
			}
			matches := idx.Short[typ]
			switch len(matches) {
			case 0:
				idx.Problems = append(idx.Problems, problem{File: file, Message: fmt.Sprintf("@model %s references unknown type %q", name, typ)})
			case 1:
				tmpl.Models[name] = matches[0]
				tmpl.Accessors["_"+name] = matches[0]
				idx.Templates[file] = tmpl
			default:
				idx.Problems = append(idx.Problems, problem{File: file, Message: fmt.Sprintf("@model %s type %q is ambiguous: %s", name, typ, strings.Join(matches, ", "))})
			}
		}
	}
}

func hasTemplateModels(idx indexFile) bool {
	for _, tmpl := range idx.Templates {
		if len(tmpl.Models) > 0 {
			return true
		}
	}
	return false
}

func exportedFields(root, path string, fileSet *token.FileSet, structType *ast.StructType) map[string]fieldIndex {
	fields := make(map[string]fieldIndex)
	for _, field := range structType.Fields.List {
		typeName := exprString(fileSet, field.Type)
		fieldDoc := docText(field.Doc)
		if fieldDoc == "" {
			fieldDoc = docText(field.Comment)
		}
		for _, name := range field.Names {
			if name.IsExported() {
				position := fileSet.Position(name.Pos())
				fields[name.Name] = fieldIndex{
					Type:   typeName,
					Doc:    fieldDoc,
					File:   rel(root, path),
					Line:   position.Line,
					Column: position.Column,
				}
			}
		}
	}
	return fields
}

func receiverTypeName(fileSet *token.FileSet, recv *ast.FieldList) string {
	if recv == nil || len(recv.List) == 0 {
		return ""
	}
	expr := recv.List[0].Type
	for {
		pointer, ok := expr.(*ast.StarExpr)
		if !ok {
			break
		}
		expr = pointer.X
	}
	switch typ := expr.(type) {
	case *ast.Ident:
		if typ.IsExported() {
			return typ.Name
		}
	case *ast.IndexExpr:
		return receiverTypeName(fileSet, &ast.FieldList{List: []*ast.Field{{Type: typ.X}}})
	case *ast.IndexListExpr:
		return receiverTypeName(fileSet, &ast.FieldList{List: []*ast.Field{{Type: typ.X}}})
	}
	return ""
}

func resultType(fileSet *token.FileSet, fn *ast.FuncType) string {
	if fn.Results == nil || len(fn.Results.List) == 0 {
		return ""
	}
	return exprString(fileSet, fn.Results.List[0].Type)
}

func funcSignature(fileSet *token.FileSet, fn *ast.FuncType) string {
	return exprString(fileSet, fn)
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

func exprString(fileSet *token.FileSet, expr ast.Expr) string {
	var builder strings.Builder
	if err := formatNode(&builder, fileSet, expr); err != nil {
		return ""
	}
	return builder.String()
}

func formatNode(builder *strings.Builder, fileSet *token.FileSet, node any) error {
	return printer.Fprint(builder, fileSet, node)
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

func moduleImportPath(root, module, dir string) string {
	relative := rel(root, dir)
	if relative == "." {
		return module
	}
	return module + "/" + filepath.ToSlash(relative)
}

func rel(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(relative)
}

func loadIndexConfig(root string) indexConfig {
	cfg := indexConfig{
		Include: []string{"/"},
		Exclude: []string{"vendor"},
	}
	data, err := os.ReadFile(filepath.Join(root, ".go-doc", "config.json"))
	if err != nil {
		return cfg
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return indexConfig{Include: []string{"/"}, Exclude: []string{"vendor"}}
	}
	if len(cfg.Include) == 0 {
		cfg.Include = []string{"/"}
	}
	return cfg
}

func shouldSkipDir(root, path, name string, cfg indexConfig) bool {
	switch name {
	case ".git", ".idea", "node_modules", "references":
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
