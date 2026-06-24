package godoccli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type lspServer struct {
	in         *bufio.Reader
	out        io.Writer
	root       string
	indexPath  string
	indexMTime time.Time
	idx        indexFile
	indexes    map[string]cachedLSPIndex
	docs       map[string]string
	nextID     int
	shutdown   bool
}

type cachedLSPIndex struct {
	idx   indexFile
	path  string
	root  string
	mtime time.Time
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type textDocumentIdentifier struct {
	URI string `json:"uri"`
}

type versionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version,omitempty"`
}

type textDocumentItem struct {
	URI  string `json:"uri"`
	Text string `json:"text"`
}

type position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type lspRange struct {
	Start position `json:"start"`
	End   position `json:"end"`
}

type location struct {
	URI   string   `json:"uri"`
	Range lspRange `json:"range"`
}

type diagnostic struct {
	Range    lspRange `json:"range"`
	Severity int      `json:"severity"`
	Source   string   `json:"source"`
	Message  string   `json:"message"`
}

type completionItem struct {
	Label         string `json:"label"`
	Kind          int    `json:"kind,omitempty"`
	Detail        string `json:"detail,omitempty"`
	Documentation string `json:"documentation,omitempty"`
	InsertText    string `json:"insertText,omitempty"`
}

type hover struct {
	Contents any      `json:"contents"`
	Range    lspRange `json:"range,omitempty"`
}

type documentSymbol struct {
	Name           string   `json:"name"`
	Kind           int      `json:"kind"`
	Range          lspRange `json:"range"`
	SelectionRange lspRange `json:"selectionRange"`
	Detail         string   `json:"detail,omitempty"`
	Children       []any    `json:"children,omitempty"`
}

type docRef struct {
	URI string `json:"uri"`
}

type didOpenParams struct {
	TextDocument textDocumentItem `json:"textDocument"`
}

type didChangeParams struct {
	TextDocument   versionedTextDocumentIdentifier `json:"textDocument"`
	ContentChanges []struct {
		Range       *lspRange `json:"range,omitempty"`
		RangeLength int       `json:"rangeLength,omitempty"`
		Text        string    `json:"text"`
	} `json:"contentChanges"`
}

type didCloseParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
}

type textDocumentPositionParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Position     position               `json:"position"`
}

type documentSymbolParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
}

type semanticTokensParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
}

type semanticTokens struct {
	Data []int `json:"data"`
}

type lspIndex struct {
	indexFile
	rootPath string
}

type scope struct {
	dotType string
	vars    map[string]string
}

type fieldRef struct {
	ownerType string
	fieldName string
	method    bool
	start     int
	end       int
}

type typeRef struct {
	typeName string
	start    int
	end      int
}

type semanticToken struct {
	start     int
	length    int
	tokenType int
}

const (
	semanticAccessor = iota
	semanticField
	semanticType
)

var (
	lspRangePattern       = regexp.MustCompile(`\{\{\s*-?\s*(range|with|end)\b([^}]*)\}\}`)
	lspScopeActionPattern = regexp.MustCompile(`\{\{\s*-?\s*([^}]*)\}\}`)
	lspAssignmentPattern  = regexp.MustCompile(`^\s*(\$[A-Za-z][A-Za-z0-9_]*)\s*:=\s*(.+?)\s*$`)
	lspActionPattern      = regexp.MustCompile(`\{\{[^}]*\}\}`)
	lspAccessorPattern    = regexp.MustCompile(`(?:[$_A-Za-z][$_A-Za-z0-9]*(?:\.[A-Za-z][A-Za-z0-9_]*)+|\.[A-Za-z][A-Za-z0-9_]*(?:\.[A-Za-z][A-Za-z0-9_]*)*)`)
	lspContractTypeRegexp = regexp.MustCompile(`@model\s+[A-Za-z][A-Za-z0-9_]*\s+([A-Za-z0-9_./-]+)`)
	lspModelPrefixRegexp  = regexp.MustCompile(`@model\s+[A-Za-z][A-Za-z0-9_]*\s+[A-Za-z0-9_./-]*$`)
)

func runLSP(input io.Reader, output io.Writer, root string) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	idx, indexPath, indexMTime, err := loadOrBuildIndex(absRoot)
	if err != nil {
		return err
	}
	server := &lspServer{
		in:         bufio.NewReader(input),
		out:        output,
		root:       absRoot,
		indexPath:  indexPath,
		indexMTime: indexMTime,
		idx:        idx,
		indexes:    make(map[string]cachedLSPIndex),
		docs:       make(map[string]string),
	}
	return server.serve()
}

func (s *lspServer) serve() error {
	for {
		msg, err := readRPCMessage(s.in)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if len(msg.ID) == 0 {
			if err := s.handleNotification(msg); err != nil {
				return err
			}
			continue
		}
		if msg.Method == "" {
			continue
		}
		result, rpcErr := s.handleRequest(msg)
		if err := writeRPCMessage(s.out, rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: result, Error: rpcErr}); err != nil {
			return err
		}
		if msg.Method == "shutdown" {
			s.shutdown = true
		}
	}
}

func (s *lspServer) handleRequest(msg rpcMessage) (any, *rpcError) {
	s.refreshWorkspaceState()
	switch msg.Method {
	case "initialize":
		return map[string]any{
			"capabilities": map[string]any{
				"textDocumentSync":       2,
				"completionProvider":     map[string]any{"triggerCharacters": []string{".", "_", "$", " "}},
				"hoverProvider":          true,
				"definitionProvider":     true,
				"documentSymbolProvider": true,
				"semanticTokensProvider": map[string]any{
					"legend": map[string]any{
						"tokenTypes":     []string{"variable", "property", "type"},
						"tokenModifiers": []string{},
					},
					"full": true,
				},
			},
			"serverInfo": map[string]string{"name": "go-doc", "version": "0.1.0"},
		}, nil
	case "shutdown":
		return nil, nil
	case "textDocument/completion":
		var params textDocumentPositionParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return nil, parseError(err)
		}
		return s.completions(params), nil
	case "textDocument/hover":
		var params textDocumentPositionParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return nil, parseError(err)
		}
		return s.hover(params), nil
	case "textDocument/definition":
		var params textDocumentPositionParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return nil, parseError(err)
		}
		return s.definition(params), nil
	case "textDocument/documentSymbol":
		var params documentSymbolParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return nil, parseError(err)
		}
		return s.documentSymbols(params), nil
	case "textDocument/semanticTokens/full":
		var params semanticTokensParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return nil, parseError(err)
		}
		return s.semanticTokens(params), nil
	default:
		return nil, &rpcError{Code: -32601, Message: "method not found"}
	}
}

func (s *lspServer) handleNotification(msg rpcMessage) error {
	s.refreshWorkspaceState()
	switch msg.Method {
	case "exit":
		return io.EOF
	case "initialized":
		return nil
	case "textDocument/didOpen":
		var params didOpenParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return err
		}
		s.docs[params.TextDocument.URI] = params.TextDocument.Text
		return s.publishDiagnostics(params.TextDocument.URI)
	case "textDocument/didChange":
		var params didChangeParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return err
		}
		if len(params.ContentChanges) > 0 {
			current, _ := s.documentText(params.TextDocument.URI)
			for _, change := range params.ContentChanges {
				current = applyTextChange(current, change.Range, change.Text)
			}
			s.docs[params.TextDocument.URI] = current
		}
		if err := s.publishDiagnostics(params.TextDocument.URI); err != nil {
			return err
		}
		return s.refreshSemanticTokens()
	case "textDocument/didClose":
		var params didCloseParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return err
		}
		delete(s.docs, params.TextDocument.URI)
		return writeRPCNotification(s.out, "textDocument/publishDiagnostics", map[string]any{"uri": params.TextDocument.URI, "diagnostics": []diagnostic{}})
	default:
		return nil
	}
}

func readRPCMessage(reader *bufio.Reader) (rpcMessage, error) {
	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return rpcMessage{}, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return rpcMessage{}, err
			}
			contentLength = n
		}
	}
	if contentLength < 0 {
		return rpcMessage{}, errors.New("missing Content-Length")
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, body); err != nil {
		return rpcMessage{}, err
	}
	var msg rpcMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		return rpcMessage{}, err
	}
	return msg, nil
}

func writeRPCMessage(writer io.Writer, msg rpcMessage) error {
	var payload any
	if msg.Error != nil {
		payload = struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Error   *rpcError       `json:"error"`
		}{
			JSONRPC: "2.0",
			ID:      msg.ID,
			Error:   msg.Error,
		}
	} else {
		payload = struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Result  any             `json:"result"`
		}{
			JSONRPC: "2.0",
			ID:      msg.ID,
			Result:  msg.Result,
		}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(writer, "Content-Length: %d\r\n\r\n%s", len(data), data)
	return err
}

func writeRPCNotification(writer io.Writer, method string, params any) error {
	data, err := json.Marshal(struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(writer, "Content-Length: %d\r\n\r\n%s", len(data), data)
	return err
}

func writeRPCRequest(writer io.Writer, id int, method string, params any) error {
	data, err := json.Marshal(struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(writer, "Content-Length: %d\r\n\r\n%s", len(data), data)
	return err
}

func parseError(err error) *rpcError {
	return &rpcError{Code: -32700, Message: err.Error()}
}

func applyTextChange(current string, rng *lspRange, replacement string) string {
	if rng == nil {
		return replacement
	}
	start := offsetAt(current, rng.Start)
	end := offsetAt(current, rng.End)
	if start > end {
		start, end = end, start
	}
	return current[:start] + replacement + current[end:]
}

func loadOrBuildIndex(root string) (indexFile, string, time.Time, error) {
	path := filepath.Join(root, ".go-doc", "index.json")
	data, err := os.ReadFile(path)
	if err == nil {
		var idx indexFile
		if err := json.Unmarshal(data, &idx); err != nil {
			return indexFile{}, path, time.Time{}, err
		}
		stat, _ := os.Stat(path)
		var mtime time.Time
		if stat != nil {
			mtime = stat.ModTime()
		}
		return idx, path, mtime, nil
	}
	idx, _, err := buildTemplateIndex(root)
	return idx, path, time.Time{}, err
}

func (s *lspServer) refreshWorkspaceState() {
	if !s.refreshIndex() {
		return
	}
	_ = s.publishOpenDiagnostics()
	_ = s.refreshSemanticTokens()
}

func (s *lspServer) refreshIndex() bool {
	stat, err := os.Stat(s.indexPath)
	if err != nil {
		return false
	}
	if !s.indexMTime.IsZero() && !stat.ModTime().After(s.indexMTime) {
		return false
	}
	data, err := os.ReadFile(s.indexPath)
	if err != nil {
		return false
	}
	var idx indexFile
	if err := json.Unmarshal(data, &idx); err != nil {
		return false
	}
	s.idx = idx
	s.indexMTime = stat.ModTime()
	return true
}

func (s *lspServer) publishOpenDiagnostics() error {
	for uri := range s.docs {
		if err := s.publishDiagnostics(uri); err != nil {
			return err
		}
	}
	return nil
}

func (s *lspServer) refreshSemanticTokens() error {
	s.nextID++
	return writeRPCRequest(s.out, s.nextID, "workspace/semanticTokens/refresh", nil)
}

func (s *lspServer) index() lspIndex {
	return lspIndex{indexFile: s.idx, rootPath: s.root}
}

func (s *lspServer) indexForURI(uri string) lspIndex {
	path, err := pathFromURI(uri)
	if err != nil {
		return s.index()
	}
	root := nearestIndexRoot(path)
	if root == "" {
		root = nearestModuleRoot(path)
	}
	if root == "" {
		return s.index()
	}
	idx, ok := s.loadIndexForRoot(root)
	if !ok {
		return s.index()
	}
	return idx
}

func (s *lspServer) loadIndexForRoot(root string) (lspIndex, bool) {
	if s.indexes == nil {
		s.indexes = make(map[string]cachedLSPIndex)
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return lspIndex{}, false
	}
	indexPath := filepath.Join(root, ".go-doc", "index.json")
	stat, statErr := os.Stat(indexPath)
	if statErr == nil {
		if cached, ok := s.indexes[root]; ok && cached.path == indexPath && !stat.ModTime().After(cached.mtime) {
			return lspIndex{indexFile: cached.idx, rootPath: cached.root}, true
		}
		data, err := os.ReadFile(indexPath)
		if err != nil {
			return lspIndex{}, false
		}
		var idx indexFile
		if err := json.Unmarshal(data, &idx); err != nil {
			return lspIndex{}, false
		}
		s.indexes[root] = cachedLSPIndex{idx: idx, path: indexPath, root: root, mtime: stat.ModTime()}
		return lspIndex{indexFile: idx, rootPath: root}, true
	}

	if cached, ok := s.indexes[root]; ok && cached.path == "" {
		return lspIndex{indexFile: cached.idx, rootPath: cached.root}, true
	}
	idx, _, err := buildTemplateIndex(root)
	if err != nil {
		return lspIndex{}, false
	}
	s.indexes[root] = cachedLSPIndex{idx: idx, root: root}
	return lspIndex{indexFile: idx, rootPath: root}, true
}

func nearestIndexRoot(path string) string {
	dir := fileDir(path)
	for dir != "" {
		if filepath.Base(dir) == ".go-doc" {
			dir = filepath.Dir(dir)
			continue
		}
		if fileExists(filepath.Join(dir, ".go-doc", "index.json")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func nearestModuleRoot(path string) string {
	dir := fileDir(path)
	for dir != "" {
		if fileExists(filepath.Join(dir, "go.mod")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func fileDir(path string) string {
	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		return path
	}
	return filepath.Dir(path)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func (s *lspServer) documentText(uri string) (string, bool) {
	if text, ok := s.docs[uri]; ok {
		return text, true
	}
	path, err := pathFromURI(uri)
	if err != nil {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(data), true
}

func (s *lspServer) contractForURI(uri string, idx lspIndex) (templateIndex, bool) {
	path, err := pathFromURI(uri)
	if err != nil {
		return templateIndex{}, false
	}
	relative := rel(idx.rootPath, path)
	text, _ := s.documentText(uri)
	if contract, ok := idx.Templates[relative]; ok {
		return mergeInlineContract(text, idx, contract), true
	}
	for key, contract := range idx.Templates {
		if strings.HasSuffix(relative, key) {
			return mergeInlineContract(text, idx, contract), true
		}
	}
	contract := mergeInlineContract(text, idx, templateIndex{})
	return contract, len(contract.Models) > 0
}

func mergeInlineContract(text string, idx lspIndex, base templateIndex) templateIndex {
	models := make(map[string]string, len(base.Models))
	for key, value := range base.Models {
		models[key] = value
	}
	accessors := make(map[string]string, len(base.Accessors))
	for key, value := range base.Accessors {
		accessors[key] = value
	}
	for _, match := range modelPattern.FindAllStringSubmatch(text, -1) {
		name := match[1]
		typeName := normalizeType(match[2])
		if resolved := resolveGoType(idx, typeName); resolved != "" {
			typeName = resolved
		}
		models[name] = typeName
		accessors["_"+name] = typeName
	}
	return templateIndex{Models: models, Accessors: accessors}
}

func (s *lspServer) publishDiagnostics(uri string) error {
	text, ok := s.documentText(uri)
	if !ok {
		return nil
	}
	idx := s.indexForURI(uri)
	contract, ok := s.contractForURI(uri, idx)
	if !ok {
		return writeRPCNotification(s.out, "textDocument/publishDiagnostics", map[string]any{"uri": uri, "diagnostics": []diagnostic{}})
	}
	items := diagnosticsForText(text, idx, contract)
	return writeRPCNotification(s.out, "textDocument/publishDiagnostics", map[string]any{"uri": uri, "diagnostics": items})
}

func diagnosticsForText(text string, idx lspIndex, contract templateIndex) []diagnostic {
	var items []diagnostic
	for _, match := range lspContractTypeRegexp.FindAllStringSubmatchIndex(text, -1) {
		start, end := match[2], match[3]
		raw := text[start:end]
		typeName := normalizeType(raw)
		if resolveGoType(idx, typeName) == "" {
			if _, ok := idx.Types[typeName]; !ok {
				items = append(items, diagnostic{
					Range:    rangeFromOffsets(text, start, end),
					Severity: 2,
					Source:   "go-doc",
					Message:  fmt.Sprintf("Unknown go-doc model type '%s'", raw),
				})
			}
		}
	}
	for _, match := range lspRangePattern.FindAllStringSubmatchIndex(text, -1) {
		if text[match[2]:match[3]] != "range" {
			continue
		}
		expression := strings.TrimSpace(text[match[4]:match[5]])
		source := sourceExpression(expression)
		sourceType := resolveExpressionValueType(idx, contract, source, "")
		if sourceType != "" && !isRangeable(sourceType) {
			items = append(items, diagnostic{
				Range:    rangeFromOffsets(text, match[4], match[5]),
				Severity: 2,
				Source:   "go-doc",
				Message:  fmt.Sprintf("Cannot range over '%s' because it is %s", source, sourceType),
			})
		}
	}
	for _, action := range lspActionPattern.FindAllStringIndex(text, -1) {
		actionText := text[action[0]:action[1]]
		for _, match := range lspAccessorPattern.FindAllStringIndex(actionText, -1) {
			start := action[0] + match[0]
			end := action[0] + match[1]
			token := actionText[match[0]:match[1]]
			root := tokenRoot(token)
			if strings.HasPrefix(root, "_") && contract.Accessors[root] == "" {
				items = append(items, diagnostic{
					Range:    rangeFromOffsets(text, start, start+len(root)),
					Severity: 1,
					Source:   "go-doc",
					Message:  fmt.Sprintf("Unknown go-doc accessor '%s'", root),
				})
				continue
			}
			ref, ok := fieldReferenceAt(text, start+(end-start)/2, idx, contract)
			if !ok {
				continue
			}
			owner := idx.Types[ref.ownerType]
			if hasMember(owner, ref.fieldName) {
				continue
			}
			if !strings.HasSuffix(token, "."+ref.fieldName) {
				continue
			}
			items = append(items, diagnostic{
				Range:    rangeFromOffsets(text, ref.start, ref.end),
				Severity: 1,
				Source:   "go-doc",
				Message:  fmt.Sprintf("Unknown field '%s' on %s", ref.fieldName, owner.Name),
			})
		}
	}
	return items
}

func tokenRoot(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	trimmed := strings.TrimPrefix(token, ".")
	root, _, _ := strings.Cut(trimmed, ".")
	if strings.HasPrefix(token, ".") {
		return "." + root
	}
	return root
}

func (s *lspServer) completions(params textDocumentPositionParams) []completionItem {
	text, ok := s.documentText(params.TextDocument.URI)
	if !ok {
		return nil
	}
	idx := s.indexForURI(params.TextDocument.URI)
	offset := offsetAt(text, params.Position)
	if inModelTypePosition(text, offset) {
		return typeCompletionItems(idx)
	}
	contract, ok := s.contractForURI(params.TextDocument.URI, idx)
	if !ok {
		return nil
	}
	if prefix, ok := accessorPrefixBeforeCaret(text, offset); ok {
		return accessorCompletionItems(idx, contract, prefix)
	}
	targetType, ok := fieldTargetBeforeCaret(text, offset, idx, contract)
	if !ok {
		return nil
	}
	typ := idx.Types[targetType]
	names := make([]string, 0, len(typ.Fields)+len(typ.Methods))
	for name := range typ.Fields {
		names = append(names, name)
	}
	for name := range typ.Methods {
		names = append(names, name)
	}
	sort.Strings(names)
	items := make([]completionItem, 0, len(names))
	for _, name := range names {
		if field, ok := typ.Fields[name]; ok {
			items = append(items, completionItem{
				Label:         name,
				Kind:          5,
				Detail:        field.Type,
				Documentation: field.Doc,
			})
			continue
		}
		method := typ.Methods[name]
		items = append(items, completionItem{
			Label:         name,
			Kind:          2,
			Detail:        method.Signature,
			Documentation: method.Doc,
		})
	}
	return items
}

func accessorPrefixBeforeCaret(text string, offset int) (string, bool) {
	before := strings.TrimRight(text[:max(0, min(offset, len(text)))], " \t\r\n")
	token := trailingToken(before)
	if token == "" {
		return "", inTemplateActionBeforeCaret(text, offset)
	}
	if strings.Contains(token, ".") {
		return "", false
	}
	if strings.HasPrefix(token, "_") || strings.HasPrefix(token, "$") {
		return token, true
	}
	return "", false
}

func inTemplateActionBeforeCaret(text string, offset int) bool {
	offset = max(0, min(offset, len(text)))
	open := strings.LastIndex(text[:offset], "{{")
	if open < 0 {
		return false
	}
	close := strings.LastIndex(text[:offset], "}}")
	if close > open {
		return false
	}
	nextClose := strings.Index(text[offset:], "}}")
	return nextClose >= 0
}

func accessorCompletionItems(idx lspIndex, contract templateIndex, prefix string) []completionItem {
	names := make([]string, 0, len(contract.Accessors))
	for name := range contract.Accessors {
		if strings.HasPrefix(name, prefix) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	items := make([]completionItem, 0, len(names))
	for _, name := range names {
		typeName := contract.Accessors[name]
		typ := idx.Types[typeName]
		detail := typeName
		doc := ""
		if typ.Name != "" {
			detail = typ.Name
			doc = typ.Doc
		}
		items = append(items, completionItem{
			Label:         name,
			Kind:          6,
			Detail:        detail,
			Documentation: doc,
			InsertText:    name + ".",
		})
	}
	return items
}

func inModelTypePosition(text string, offset int) bool {
	before := text[:max(0, min(offset, len(text)))]
	if len(before) > 300 {
		before = before[len(before)-300:]
	}
	return lspModelPrefixRegexp.MatchString(before)
}

func typeCompletionItems(idx lspIndex) []completionItem {
	names := make([]string, 0, len(idx.Types))
	for name := range idx.Types {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		left, right := idx.Types[names[i]], idx.Types[names[j]]
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		return names[i] < names[j]
	})
	items := make([]completionItem, 0, len(names))
	for _, fullName := range names {
		typ := idx.Types[fullName]
		items = append(items, completionItem{
			Label:         fullName,
			Kind:          7,
			Detail:        typ.Package,
			Documentation: typ.Doc,
		})
	}
	return items
}

func (s *lspServer) hover(params textDocumentPositionParams) any {
	text, ok := s.documentText(params.TextDocument.URI)
	if !ok {
		return nil
	}
	idx := s.indexForURI(params.TextDocument.URI)
	offset := offsetAt(text, params.Position)
	if ref, ok := typeReferenceAt(text, offset, idx); ok {
		typ := idx.Types[ref.typeName]
		return hover{
			Contents: markdown(fmt.Sprintf("```go\ntype %s struct\n```\n%s", typ.Name, typ.Doc)),
			Range:    rangeFromOffsets(text, ref.start, ref.end),
		}
	}
	contract, ok := s.contractForURI(params.TextDocument.URI, idx)
	if !ok {
		return nil
	}
	ref, ok := fieldReferenceAt(text, offset, idx, contract)
	if !ok {
		return nil
	}
	owner := idx.Types[ref.ownerType]
	if field, ok := owner.Fields[ref.fieldName]; ok {
		return hover{
			Contents: markdown(fmt.Sprintf("```go\n%s.%s %s\n```\n%s", owner.Name, ref.fieldName, field.Type, field.Doc)),
			Range:    rangeFromOffsets(text, ref.start, ref.end),
		}
	}
	if method, ok := owner.Methods[ref.fieldName]; ok {
		signature := method.Signature
		if signature == "" {
			signature = "func()"
		}
		return hover{
			Contents: markdown(fmt.Sprintf("```go\nfunc (%s) %s%s\n```\n%s", owner.Name, ref.fieldName, strings.TrimPrefix(signature, "func"), method.Doc)),
			Range:    rangeFromOffsets(text, ref.start, ref.end),
		}
	}
	return nil
}

func (s *lspServer) definition(params textDocumentPositionParams) any {
	text, ok := s.documentText(params.TextDocument.URI)
	if !ok {
		return nil
	}
	idx := s.indexForURI(params.TextDocument.URI)
	offset := offsetAt(text, params.Position)
	if ref, ok := typeReferenceAt(text, offset, idx); ok {
		typ := idx.Types[ref.typeName]
		return locationForTarget(idx.rootPath, typ.File, typ.Line, typ.Column)
	}
	contract, ok := s.contractForURI(params.TextDocument.URI, idx)
	if !ok {
		return nil
	}
	ref, ok := fieldReferenceAt(text, offset, idx, contract)
	if !ok {
		return nil
	}
	owner := idx.Types[ref.ownerType]
	if field, ok := owner.Fields[ref.fieldName]; ok {
		file := field.File
		if file == "" {
			file = owner.File
		}
		return locationForTarget(idx.rootPath, file, field.Line, field.Column)
	}
	if method, ok := owner.Methods[ref.fieldName]; ok {
		file := method.File
		if file == "" {
			file = owner.File
		}
		return locationForTarget(idx.rootPath, file, method.Line, method.Column)
	}
	return nil
}

func (s *lspServer) documentSymbols(params documentSymbolParams) []documentSymbol {
	idx := s.indexForURI(params.TextDocument.URI)
	contract, ok := s.contractForURI(params.TextDocument.URI, idx)
	if !ok {
		return nil
	}
	var symbols []documentSymbol
	for name, typeName := range contract.Accessors {
		rng := lspRange{Start: position{}, End: position{}}
		symbols = append(symbols, documentSymbol{Name: name, Detail: typeName, Kind: 13, Range: rng, SelectionRange: rng})
	}
	sort.Slice(symbols, func(i, j int) bool { return symbols[i].Name < symbols[j].Name })
	return symbols
}

func (s *lspServer) semanticTokens(params semanticTokensParams) semanticTokens {
	text, ok := s.documentText(params.TextDocument.URI)
	if !ok {
		return semanticTokens{}
	}
	idx := s.indexForURI(params.TextDocument.URI)
	contract, _ := s.contractForURI(params.TextDocument.URI, idx)
	tokens := semanticTokensForText(text, idx, contract)
	return semanticTokens{Data: encodeSemanticTokens(text, tokens)}
}

func semanticTokensForText(text string, idx lspIndex, contract templateIndex) []semanticToken {
	var tokens []semanticToken
	for _, match := range lspContractTypeRegexp.FindAllStringSubmatchIndex(text, -1) {
		start, end := match[2], match[3]
		raw := text[start:end]
		typeName := normalizeType(raw)
		if resolveGoType(idx, typeName) == "" {
			if _, ok := idx.Types[typeName]; !ok {
				continue
			}
		}
		shortStart := start + len(raw) - len(shortTypeName(raw))
		tokens = append(tokens, semanticToken{start: shortStart, length: end - shortStart, tokenType: semanticType})
	}
	for _, action := range lspActionPattern.FindAllStringIndex(text, -1) {
		actionText := text[action[0]:action[1]]
		for _, match := range lspAccessorPattern.FindAllStringIndex(actionText, -1) {
			start := action[0] + match[0]
			end := action[0] + match[1]
			token := actionText[match[0]:match[1]]
			root := tokenRoot(token)
			if (strings.HasPrefix(root, "_") || strings.HasPrefix(root, "$")) && contract.Accessors[root] != "" {
				tokens = append(tokens, semanticToken{start: start, length: len(root), tokenType: semanticAccessor})
			}
			ref, ok := fieldReferenceAt(text, start+(end-start)/2, idx, contract)
			if !ok {
				continue
			}
			owner := idx.Types[ref.ownerType]
			if hasMember(owner, ref.fieldName) {
				tokens = append(tokens, semanticToken{start: ref.start, length: ref.end - ref.start, tokenType: semanticField})
			}
		}
	}
	sort.Slice(tokens, func(i, j int) bool {
		if tokens[i].start == tokens[j].start {
			return tokens[i].length < tokens[j].length
		}
		return tokens[i].start < tokens[j].start
	})
	return compactSemanticTokens(tokens)
}

func shortTypeName(typeName string) string {
	lastSlash := strings.LastIndex(typeName, "/")
	lastDot := strings.LastIndex(typeName, ".")
	separator := max(lastSlash, lastDot)
	if separator < 0 || separator == len(typeName)-1 {
		return typeName
	}
	return typeName[separator+1:]
}

func compactSemanticTokens(tokens []semanticToken) []semanticToken {
	if len(tokens) == 0 {
		return nil
	}
	compacted := tokens[:0]
	lastStart, lastEnd := -1, -1
	for _, token := range tokens {
		if token.length <= 0 {
			continue
		}
		if token.start == lastStart && token.start+token.length == lastEnd {
			continue
		}
		compacted = append(compacted, token)
		lastStart, lastEnd = token.start, token.start+token.length
	}
	return compacted
}

func encodeSemanticTokens(text string, tokens []semanticToken) []int {
	data := make([]int, 0, len(tokens)*5)
	prevLine, prevChar := 0, 0
	for _, token := range tokens {
		pos := positionAt(text, token.start)
		deltaLine := pos.Line - prevLine
		deltaStart := pos.Character
		if deltaLine == 0 {
			deltaStart -= prevChar
		}
		data = append(data, deltaLine, deltaStart, token.length, token.tokenType, 0)
		prevLine, prevChar = pos.Line, pos.Character
	}
	return data
}

func markdown(value string) map[string]string {
	return map[string]string{"kind": "markdown", "value": strings.TrimSpace(value)}
}

func resolveExpressionValueType(idx lspIndex, contract templateIndex, expression, dotType string) string {
	clean := strings.TrimSpace(expression)
	if clean == "" {
		return ""
	}
	if clean == "." {
		return dotType
	}
	parts := strings.FieldsFunc(clean, func(r rune) bool { return r == '.' })
	if len(parts) == 0 {
		return ""
	}
	root := parts[0]
	var rootType string
	switch {
	case strings.HasPrefix(root, "_"):
		rootType = contract.Accessors[root]
	case strings.HasPrefix(root, "$"):
		rootType = contract.Accessors[root]
	case strings.HasPrefix(clean, "."):
		rootType = dotType
	}
	if rootType == "" {
		return ""
	}
	return resolveFieldValuePath(idx, rootType, parts[1:])
}

func resolveExpressionType(idx lspIndex, contract templateIndex, expression, dotType string) string {
	return resolveGoType(idx, resolveExpressionValueType(idx, contract, expression, dotType))
}

func resolveFieldPath(idx lspIndex, rootType string, fields []string) string {
	return resolveGoType(idx, resolveFieldValuePath(idx, rootType, fields))
}

func resolveFieldValuePath(idx lspIndex, rootType string, fields []string) string {
	current := rootType
	for i, name := range fields {
		typ, ok := idx.Types[current]
		if !ok {
			return ""
		}
		valueType, ok := memberValueType(typ, name)
		if !ok {
			return ""
		}
		if i == len(fields)-1 {
			return valueType
		}
		current = resolveGoType(idx, valueType)
		if current == "" {
			return ""
		}
	}
	return current
}

func hasMember(typ goTypeIndex, name string) bool {
	if _, ok := typ.Fields[name]; ok {
		return true
	}
	_, ok := typ.Methods[name]
	return ok
}

func memberValueType(typ goTypeIndex, name string) (string, bool) {
	if field, ok := typ.Fields[name]; ok {
		return field.Type, true
	}
	if method, ok := typ.Methods[name]; ok {
		return method.Type, true
	}
	return "", false
}

func resolveGoType(idx lspIndex, typeExpr string) string {
	normalized := normalizeValueType(typeExpr)
	if _, ok := idx.Types[normalized]; ok {
		return normalized
	}
	if idx.Module != "" && strings.Contains(normalized, ".") && !strings.Contains(normalized, "/") {
		importPath, typeName, ok := strings.Cut(normalized, ".")
		if ok {
			moduleScoped := idx.Module + "/" + importPath + "." + typeName
			if _, ok := idx.Types[moduleScoped]; ok {
				return moduleScoped
			}
		}
	}
	matches := idx.Short[normalized]
	if len(matches) == 1 {
		return matches[0]
	}
	return ""
}

func rangeElementType(idx lspIndex, typeExpr string) string {
	normalized := strings.TrimSpace(typeExpr)
	for {
		normalized = stripPointer(normalized)
		switch {
		case strings.HasPrefix(normalized, "[]"):
			normalized = strings.TrimPrefix(normalized, "[]")
		case strings.HasPrefix(normalized, "["):
			end := strings.Index(normalized, "]")
			if end < 0 {
				return ""
			}
			normalized = normalized[end+1:]
		default:
			return mapElementType(idx, normalized)
		}
	}
}

func mapElementType(idx lspIndex, typeExpr string) string {
	normalized := stripPointer(strings.TrimSpace(typeExpr))
	if strings.HasPrefix(normalized, "map[") {
		end := strings.Index(normalized, "]")
		if end < 0 {
			return ""
		}
		return resolveGoType(idx, normalized[end+1:])
	}
	return resolveGoType(idx, normalized)
}

func isRangeable(typeExpr string) bool {
	normalized := stripPointer(strings.TrimSpace(typeExpr))
	return strings.HasPrefix(normalized, "[]") || strings.HasPrefix(normalized, "[") || strings.HasPrefix(normalized, "map[")
}

func normalizeValueType(typeExpr string) string {
	normalized := strings.TrimSpace(typeExpr)
	for {
		normalized = stripPointer(normalized)
		switch {
		case strings.HasPrefix(normalized, "[]"):
			normalized = strings.TrimPrefix(normalized, "[]")
		case strings.HasPrefix(normalized, "["):
			end := strings.Index(normalized, "]")
			if end < 0 {
				return normalized
			}
			normalized = normalized[end+1:]
		default:
			return strings.TrimSpace(normalized)
		}
	}
}

func stripPointer(typeExpr string) string {
	typeExpr = strings.TrimSpace(typeExpr)
	for strings.HasPrefix(typeExpr, "*") {
		typeExpr = strings.TrimSpace(strings.TrimPrefix(typeExpr, "*"))
	}
	return typeExpr
}

func scopeAt(text string, offset int, idx lspIndex, contract templateIndex) scope {
	stack := []scope{{vars: map[string]string{}}}
	before := text[:max(0, min(offset, len(text)))]
	for _, match := range lspScopeActionPattern.FindAllStringSubmatchIndex(before, -1) {
		action := strings.TrimSpace(strings.Trim(before[match[2]:match[3]], "- "))
		keyword, expression, _ := strings.Cut(action, " ")
		expression = strings.TrimSpace(expression)
		parent := stack[len(stack)-1]
		switch keyword {
		case "range":
			sourceType := resolveExpressionValueType(idx, contract, sourceExpression(expression), parent.dotType)
			itemType := rangeElementType(idx, sourceType)
			stack = append(stack, scope{dotType: itemType, vars: mergeVars(parent.vars, rangeVariables(expression, itemType))})
		case "with":
			valueType := resolveExpressionType(idx, contract, sourceExpression(expression), parent.dotType)
			stack = append(stack, scope{dotType: valueType, vars: mergeVars(parent.vars, assignedVariable(expression, idx, contract, parent.dotType))})
		case "end":
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		default:
			stack[len(stack)-1] = scope{dotType: parent.dotType, vars: mergeVars(parent.vars, assignedVariable(action, idx, contract, parent.dotType))}
		}
	}
	return stack[len(stack)-1]
}

func sourceExpression(expression string) string {
	if _, right, ok := strings.Cut(expression, ":="); ok {
		return strings.TrimSpace(right)
	}
	if _, right, ok := strings.Cut(expression, "="); ok {
		return strings.TrimSpace(right)
	}
	return strings.TrimSpace(expression)
}

func rangeVariables(expression, elementType string) map[string]string {
	if elementType == "" || !strings.Contains(expression, ":=") {
		return nil
	}
	left, _, _ := strings.Cut(expression, ":=")
	names := strings.Split(left, ",")
	for i := len(names) - 1; i >= 0; i-- {
		name := strings.TrimSpace(names[i])
		if strings.HasPrefix(name, "$") {
			return map[string]string{name: elementType}
		}
	}
	return nil
}

func assignedVariable(expression string, idx lspIndex, contract templateIndex, dotType string) map[string]string {
	match := lspAssignmentPattern.FindStringSubmatch(expression)
	if len(match) != 3 {
		return nil
	}
	typeName := resolveExpressionType(idx, contract, match[2], dotType)
	if typeName == "" {
		return nil
	}
	return map[string]string{match[1]: typeName}
}

func mergeVars(left, right map[string]string) map[string]string {
	merged := make(map[string]string, len(left)+len(right))
	for key, value := range left {
		merged[key] = value
	}
	for key, value := range right {
		merged[key] = value
	}
	return merged
}

func fieldTargetBeforeCaret(text string, offset int, idx lspIndex, contract templateIndex) (string, bool) {
	before := strings.TrimRight(text[:max(0, min(offset, len(text)))], " \t\r\n")
	if len(before) > 500 {
		before = before[len(before)-500:]
	}
	token := trailingToken(before)
	if token == "" || !strings.Contains(token, ".") {
		return "", false
	}
	lastDot := strings.LastIndex(token, ".")
	chain := token[:lastDot+1]
	parts := strings.FieldsFunc(chain, func(r rune) bool { return r == '.' })
	if strings.HasPrefix(chain, "_") || strings.HasPrefix(chain, "$") {
		vars := scopeAt(text, offset, idx, contract).vars
		rootType := contract.Accessors[parts[0]]
		if rootType == "" {
			rootType = vars[parts[0]]
		}
		typeName := resolveFieldPath(idx, rootType, parts[1:])
		return typeName, typeName != ""
	}
	if strings.HasPrefix(chain, ".") {
		dotType := scopeAt(text, offset, idx, contract).dotType
		typeName := resolveFieldPath(idx, dotType, parts)
		return typeName, typeName != ""
	}
	return "", false
}

func fieldReferenceAt(text string, offset int, idx lspIndex, contract templateIndex) (fieldRef, bool) {
	token, _, end := tokenAt(text, offset)
	if token == "" {
		return fieldRef{}, false
	}
	parts := strings.FieldsFunc(token, func(r rune) bool { return r == '.' })
	if len(parts) == 0 {
		return fieldRef{}, false
	}
	if strings.HasPrefix(token, "_") || strings.HasPrefix(token, "$") {
		vars := scopeAt(text, offset, idx, contract).vars
		rootType := contract.Accessors[parts[0]]
		if rootType == "" {
			rootType = vars[parts[0]]
		}
		ownerType := resolveFieldPath(idx, rootType, parts[1:len(parts)-1])
		if ownerType == "" {
			return fieldRef{}, false
		}
		fieldStart := end - len(parts[len(parts)-1])
		return fieldRef{ownerType: ownerType, fieldName: parts[len(parts)-1], start: fieldStart, end: end}, true
	}
	if strings.HasPrefix(token, ".") {
		dotType := scopeAt(text, offset, idx, contract).dotType
		ownerType := resolveFieldPath(idx, dotType, parts[:len(parts)-1])
		if ownerType == "" {
			return fieldRef{}, false
		}
		fieldStart := end - len(parts[len(parts)-1])
		return fieldRef{ownerType: ownerType, fieldName: parts[len(parts)-1], start: fieldStart, end: end}, true
	}
	return fieldRef{}, false
}

func typeReferenceAt(text string, offset int, idx lspIndex) (typeRef, bool) {
	for _, match := range lspContractTypeRegexp.FindAllStringSubmatchIndex(text, -1) {
		start, end := match[2], match[3]
		if offset < start || offset > end {
			continue
		}
		raw := text[start:end]
		typeName := resolveGoType(idx, raw)
		if typeName == "" {
			if _, ok := idx.Types[raw]; ok {
				typeName = raw
			}
		}
		if typeName == "" {
			return typeRef{}, false
		}
		shortStart := start + len(raw) - len(shortTypeName(raw))
		if offset >= shortStart {
			start = shortStart
		}
		return typeRef{typeName: typeName, start: start, end: end}, true
	}
	return typeRef{}, false
}

func trailingToken(text string) string {
	end := len(text)
	start := end
	for start > 0 && isTokenChar(text[start-1]) {
		start--
	}
	return text[start:end]
}

func tokenAt(text string, offset int) (string, int, int) {
	start := max(0, min(offset, len(text)))
	end := start
	for start > 0 && isTokenChar(text[start-1]) {
		start--
	}
	for end < len(text) && isTokenChar(text[end]) {
		end++
	}
	token := text[start:end]
	if strings.Contains(token, ".") {
		return token, start, end
	}
	return "", 0, 0
}

func isTokenChar(b byte) bool {
	return b == '$' || b == '_' || b == '.' || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

func locationForTarget(root, file string, line, column int) any {
	if file == "" {
		return nil
	}
	if line <= 0 {
		line = 1
	}
	if column <= 0 {
		column = 1
	}
	pos := position{Line: line - 1, Character: column - 1}
	return location{URI: uriFromPath(filepath.Join(root, filepath.FromSlash(file))), Range: lspRange{Start: pos, End: pos}}
}

func pathFromURI(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "file" {
		return "", fmt.Errorf("unsupported URI scheme %q", parsed.Scheme)
	}
	path, err := url.PathUnescape(parsed.Path)
	if err != nil {
		return "", err
	}
	if parsed.Host != "" {
		path = "//" + parsed.Host + path
	}
	if len(path) >= 3 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	return filepath.FromSlash(path), nil
}

func uriFromPath(path string) string {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	if len(u.Path) >= 2 && u.Path[1] == ':' {
		u.Path = "/" + u.Path
	}
	return u.String()
}

func offsetAt(text string, pos position) int {
	line, character := 0, 0
	for i, r := range text {
		if line == pos.Line && character == pos.Character {
			return i
		}
		if r == '\n' {
			line++
			character = 0
			continue
		}
		character++
	}
	return len(text)
}

func positionAt(text string, offset int) position {
	offset = max(0, min(offset, len(text)))
	line, character := 0, 0
	for i, r := range text {
		if i >= offset {
			break
		}
		if r == '\n' {
			line++
			character = 0
			continue
		}
		character++
	}
	return position{Line: line, Character: character}
}

func rangeFromOffsets(text string, start, end int) lspRange {
	return lspRange{Start: positionAt(text, start), End: positionAt(text, end)}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func testRPCConversation(input string) (string, error) {
	var out bytes.Buffer
	err := runLSP(strings.NewReader(input), &out, ".")
	return out.String(), err
}
