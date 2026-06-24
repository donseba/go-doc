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
		Params    map[string]string `json:"params"`
		Vars      map[string]string `json:"vars,omitempty"`
		Accessors map[string]string `json:"accessors"`
	}

	goTypeIndex struct {
		Name    string                `json:"name"`
		Package string                `json:"package"`
		File    string                `json:"file"`
		Line    int                   `json:"line,omitempty"`
		Column  int                   `json:"column,omitempty"`
		Doc     string                `json:"doc,omitempty"`
		Fields  map[string]fieldIndex `json:"fields"`
	}

	fieldIndex struct {
		Type   string `json:"type"`
		Doc    string `json:"doc,omitempty"`
		File   string `json:"file,omitempty"`
		Line   int    `json:"line,omitempty"`
		Column int    `json:"column,omitempty"`
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
)

var (
	paramPattern = regexp.MustCompile(`(?m)^\s*@param\s+([A-Za-z][A-Za-z0-9_]*)\s+([A-Za-z_][A-Za-z0-9_./\[\]*-]*)\s*$`)
	varPattern   = regexp.MustCompile(`(?m)^\s*@var\s+(\$?[A-Za-z][A-Za-z0-9_]*)\s+([A-Za-z_][A-Za-z0-9_./\[\]*-]*)\s*$`)
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
		idx, err := buildIndex(root)
		if err != nil {
			return err
		}
		return writeJSON(idx, *out)

	default:
		return usage()
	}
}

func usage() error {
	return errors.New(`usage:
  go-doc types [-query Todo] [root]
  go-doc templates [root]
  go-doc index [-o .go-doc/index.json] [root]`)
}

func argRoot(args []string) string {
	if len(args) == 0 {
		return "."
	}
	return args[0]
}

func buildIndex(root string) (indexFile, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return indexFile{}, err
	}

	module, err := readModulePath(absRoot)
	if err != nil {
		return indexFile{}, err
	}

	idx := indexFile{
		Version:   2,
		Module:    module,
		Templates: make(map[string]templateIndex),
		Types:     make(map[string]goTypeIndex),
		Funcs:     make(map[string]goFuncIndex),
		Short:     make(map[string][]string),
	}

	if err := scanGoTypes(absRoot, module, &idx); err != nil {
		return indexFile{}, err
	}
	if err := scanTemplates(absRoot, &idx); err != nil {
		return indexFile{}, err
	}
	sortShortNames(idx.Short)
	validateTemplateTypes(&idx)

	return idx, nil
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

func scanGoTypes(root, module string, idx *indexFile) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if shouldSkipDir(d.Name()) {
				return filepath.SkipDir
			}
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
				}
				idx.Short[typeSpec.Name.Name] = append(idx.Short[typeSpec.Name.Name], fullName)
			}
		}

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !fn.Name.IsExported() {
				continue
			}
			position := fileSet.Position(fn.Name.Pos())
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

func scanTemplates(root string, idx *indexFile) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if shouldSkipDir(d.Name()) {
				return filepath.SkipDir
			}
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
		params := parseParams(src)
		vars := parseVars(src)
		if len(params) == 0 && len(vars) == 0 {
			return nil
		}

		accessors := make(map[string]string, len(params)+len(vars))
		for name, typ := range params {
			accessors["_"+name] = typ
		}
		for name, typ := range vars {
			accessors[name] = typ
		}
		idx.Templates[rel(root, path)] = templateIndex{
			Params:    params,
			Vars:      vars,
			Accessors: accessors,
		}
		return nil
	})
}

func parseParams(src string) map[string]string {
	params := make(map[string]string)
	for _, match := range paramPattern.FindAllStringSubmatch(src, -1) {
		params[match[1]] = normalizeType(match[2])
	}
	return params
}

func parseVars(src string) map[string]string {
	vars := make(map[string]string)
	for _, match := range varPattern.FindAllStringSubmatch(src, -1) {
		name := match[1]
		if !strings.HasPrefix(name, "$") {
			name = "$" + name
		}
		vars[name] = normalizeType(match[2])
	}
	return vars
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
		for name, typ := range tmpl.Params {
			if _, ok := idx.Types[typ]; ok {
				continue
			}
			matches := idx.Short[typ]
			switch len(matches) {
			case 0:
				idx.Problems = append(idx.Problems, problem{File: file, Message: fmt.Sprintf("@param %s references unknown type %q", name, typ)})
			case 1:
				tmpl.Params[name] = matches[0]
				tmpl.Accessors["_"+name] = matches[0]
				idx.Templates[file] = tmpl
			default:
				idx.Problems = append(idx.Problems, problem{File: file, Message: fmt.Sprintf("@param %s type %q is ambiguous: %s", name, typ, strings.Join(matches, ", "))})
			}
		}
		for name, typ := range tmpl.Vars {
			if _, ok := idx.Types[typ]; ok {
				continue
			}
			matches := idx.Short[typ]
			switch len(matches) {
			case 0:
				idx.Problems = append(idx.Problems, problem{File: file, Message: fmt.Sprintf("@var %s references unknown type %q", name, typ)})
			case 1:
				tmpl.Vars[name] = matches[0]
				tmpl.Accessors[name] = matches[0]
				idx.Templates[file] = tmpl
			default:
				idx.Problems = append(idx.Problems, problem{File: file, Message: fmt.Sprintf("@var %s type %q is ambiguous: %s", name, typ, strings.Join(matches, ", "))})
			}
		}
	}
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

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", ".idea", "node_modules", "vendor", "references":
		return true
	default:
		return false
	}
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
