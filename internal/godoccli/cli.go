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
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
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
		Models map[string]string `json:"models"`
		Dot    string            `json:"dot,omitempty"`
		Funcs  map[string]string `json:"funcs,omitempty"`
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
		Name      string   `json:"name"`
		Package   string   `json:"package"`
		File      string   `json:"file"`
		Line      int      `json:"line,omitempty"`
		Column    int      `json:"column,omitempty"`
		Doc       string   `json:"doc,omitempty"`
		Result    string   `json:"result,omitempty"`
		Results   []string `json:"results,omitempty"`
		ReturnOK  bool     `json:"returnOK,omitempty"`
		Signature string   `json:"signature,omitempty"`
		Params    []string `json:"params,omitempty"`
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
	modelPattern = regexp.MustCompile(`(?m)^\s*@model\s+([A-Za-z_][A-Za-z0-9_]*)\s+([A-Za-z_][A-Za-z0-9_./\[\]*-]*)\s*$`)
	dotPattern   = regexp.MustCompile(`(?m)^\s*@dot\s+([A-Za-z_][A-Za-z0-9_./\[\]*-]*)\s*$`)
	funcPattern  = regexp.MustCompile(`(?m)^\s*@func\s+([A-Za-z_][A-Za-z0-9_]*)\s+([A-Za-z_][A-Za-z0-9_./-]*)\s*$`)
)

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
	if err := scanGoTypes(absRoot, cfg, &idx); err != nil {
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

func scanGoTypes(root string, cfg indexConfig, idx *indexFile) error {
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
		indexPackageTypes(root, fileSet, pkg, idx)
	}
	return nil
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

func indexPackageTypes(root string, fileSet *token.FileSet, pkg *packages.Package, idx *indexFile) {
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.GenDecl:
				if decl.Tok == token.TYPE {
					indexPackageTypeDecl(root, fileSet, pkg, decl, idx)
				}
			case *ast.FuncDecl:
				indexPackageFuncDecl(root, fileSet, pkg, decl, idx)
			}
		}
	}
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
		named, structType := namedStruct(obj.Type())
		if named == nil || structType == nil {
			continue
		}
		position := fileSet.Position(typeSpec.Name.Pos())
		fullName := qualifiedObjectName(obj)
		idx.Types[fullName] = goTypeIndex{
			Name:    obj.Name(),
			Package: obj.Pkg().Path(),
			File:    rel(root, position.Filename),
			Line:    position.Line,
			Column:  position.Column,
			Doc:     docText(firstDoc(typeSpec.Doc, decl.Doc)),
			Fields:  exportedTypedFields(root, fileSet, pkg, structType, typeSpec),
			Methods: make(map[string]methodIndex),
		}
		idx.Short[obj.Name()] = append(idx.Short[obj.Name()], fullName)
	}
}

func indexPackageFuncDecl(root string, fileSet *token.FileSet, pkg *packages.Package, fn *ast.FuncDecl, idx *indexFile) {
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
		typ.Methods[obj.Name()] = methodIndex{
			Type:      templateValueResultType(results),
			Signature: types.TypeString(sig, typeQualifier(pkg.Types)),
			Doc:       docText(fn.Doc),
			File:      rel(root, position.Filename),
			Line:      position.Line,
			Column:    position.Column,
		}
		idx.Types[fullTypeName] = typ
		return
	}
	results := signatureResults(sig, pkg.Types)
	idx.Funcs[qualifiedObjectName(obj)] = goFuncIndex{
		Name:      obj.Name(),
		Package:   obj.Pkg().Path(),
		File:      rel(root, position.Filename),
		Line:      position.Line,
		Column:    position.Column,
		Doc:       docText(fn.Doc),
		Result:    templateValueResultType(results),
		Results:   results,
		ReturnOK:  true,
		Signature: types.TypeString(sig, typeQualifier(pkg.Types)),
		Params:    signatureParams(sig, pkg.Types),
	}
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

func exportedTypedFields(root string, fileSet *token.FileSet, pkg *packages.Package, structType *types.Struct, typeSpec *ast.TypeSpec) map[string]fieldIndex {
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
		fields[field.Name()] = meta
	}
	return fields
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
		dot := parseDot(src)
		funcs := parseFuncs(src)
		if len(models) == 0 && dot == "" && len(funcs) == 0 {
			return nil
		}

		idx.Templates[rel(root, path)] = templateIndex{
			Models: models,
			Dot:    dot,
			Funcs:  funcs,
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

func parseDot(src string) string {
	match := dotPattern.FindStringSubmatch(src)
	if len(match) != 2 {
		return ""
	}
	return normalizeType(match[1])
}

func parseFuncs(src string) map[string]string {
	funcs := make(map[string]string)
	for _, match := range funcPattern.FindAllStringSubmatch(src, -1) {
		funcs[match[1]] = normalizeType(match[2])
	}
	return funcs
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
			if reservedTemplateNames[name] {
				idx.Problems = append(idx.Problems, problem{File: file, Message: fmt.Sprintf("@model %s uses a reserved template name", name)})
				continue
			}
			if _, ok := idx.Types[typ]; ok {
				continue
			}
			matches := idx.Short[typ]
			switch len(matches) {
			case 0:
				idx.Problems = append(idx.Problems, problem{File: file, Message: fmt.Sprintf("@model %s references unknown type %q", name, typ)})
			case 1:
				tmpl.Models[name] = matches[0]
				idx.Templates[file] = tmpl
			default:
				idx.Problems = append(idx.Problems, problem{File: file, Message: fmt.Sprintf("@model %s type %q is ambiguous: %s", name, typ, strings.Join(matches, ", "))})
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
			if reservedTemplateNames[name] {
				idx.Problems = append(idx.Problems, problem{File: file, Message: fmt.Sprintf("@func %s uses a reserved template name", name)})
				continue
			}
			if _, ok := idx.Funcs[fn]; !ok {
				idx.Problems = append(idx.Problems, problem{File: file, Message: fmt.Sprintf("@func %s references unknown function %q", name, fn)})
			}
		}
	}
}

func hasTemplateModels(idx indexFile) bool {
	for _, tmpl := range idx.Templates {
		if len(tmpl.Models) > 0 || tmpl.Dot != "" {
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
