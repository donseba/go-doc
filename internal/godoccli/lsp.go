package godoccli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/url"
	"os"
	"os/exec"
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
	idx         indexFile
	path        string
	root        string
	mtime       time.Time
	sourceMTime time.Time
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

type renameParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Position     position               `json:"position"`
	NewName      string                 `json:"newName"`
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

type codeActionParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Range        lspRange               `json:"range"`
	Context      codeActionContext      `json:"context"`
}

type codeActionContext struct {
	Diagnostics []diagnostic `json:"diagnostics,omitempty"`
}

type codeAction struct {
	Title       string         `json:"title"`
	Kind        string         `json:"kind,omitempty"`
	Diagnostics []diagnostic   `json:"diagnostics,omitempty"`
	Edit        *workspaceEdit `json:"edit,omitempty"`
	Command     *command       `json:"command,omitempty"`
}

type command struct {
	Title     string `json:"title"`
	Command   string `json:"command"`
	Arguments []any  `json:"arguments,omitempty"`
}

type executeCommandParams struct {
	Command   string          `json:"command"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type missingFieldAppliedCommand struct {
	Root      string   `json:"root"`
	OwnerType string   `json:"ownerType"`
	FieldName string   `json:"fieldName"`
	FieldType string   `json:"fieldType"`
	TargetURI string   `json:"targetUri"`
	Range     lspRange `json:"range"`
	NewText   string   `json:"newText"`
}

type workspaceEdit struct {
	Changes map[string][]textEdit `json:"changes"`
}

type textEdit struct {
	Range   lspRange `json:"range"`
	NewText string   `json:"newText"`
}

type lspIndex struct {
	indexFile
	rootPath string
}

func (idx lspIndex) symbolParseConfig() symbolParseConfig {
	return symbolParseConfig{Aliases: idx.SymbolAliases, Strict: idx.SymbolStrict}
}

type scope struct {
	dotType string
	vars    map[string]string
}

type fieldRef struct {
	ownerType string
	fieldName string
	start     int
	end       int
}

type typeRef struct {
	typeName string
	start    int
	end      int
}

type funcRef struct {
	funcName string
	start    int
	end      int
}

type modelNameRef struct {
	name  string
	start int
	end   int
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
	semanticFunction
)

const (
	lspServerName = "go-doc"
	lspSource     = "go-doc"

	goDocDir      = ".go-doc"
	indexFileName = "index.json"

	lspCommandMissingFieldApplied  = "goDoc.missingFieldApplied"
	lspMethodPublishDiagnostics    = "textDocument/publishDiagnostics"
	lspMethodSemanticTokensRefresh = "workspace/semanticTokens/refresh"
	lspMethodWorkspaceApplyEdit    = "workspace/applyEdit"

	lspDiagnosticsKey = "diagnostics"
	lspQuickFixKind   = "quickfix"
	lspShutdownMethod = "shutdown"

	rpcContentFormat = "Content-Length: %d\r\n\r\n%s"

	goRootMarker = "$GOROOT"
	goRootEnv    = "GOROOT"

	unknownFieldDiagnosticPrefix       = "Unknown field "
	unknownTypedRootTypePrefix         = "Unknown go-doc typed root type "
	unknownTypedRootTypeFormat         = unknownTypedRootTypePrefix + "'%s'"
	moveDefineAnnotationsPrefix        = "Move go-doc annotations inside define "
	moveDefineAnnotationsFormat        = moveDefineAnnotationsPrefix + "%q"
	moveDefineAnnotationsCodeAction    = "Move go-doc annotations inside define"
	missingFieldCodeActionTitleFormat  = "Add field %s %s to %s"
	missingFieldAppliedCodeActionTitle = "Add missing field"
)

var (
	lspRangePattern                = regexp.MustCompile(`\{\{\s*-?\s*(range|with|end)\b([^}]*)\}\}`)
	lspScopeActionPattern          = regexp.MustCompile(`\{\{\s*-?\s*([^}]*)\}\}`)
	lspAssignmentPattern           = regexp.MustCompile(`^\s*(\$[A-Za-z][A-Za-z0-9_]*)\s*:=\s*(.+?)\s*$`)
	lspActionPattern               = regexp.MustCompile(`\{\{[^}]*\}\}`)
	lspAccessorPattern             = regexp.MustCompile(`(?:[$_A-Za-z][$_A-Za-z0-9]*(?:\.[A-Za-z][A-Za-z0-9_]*)+|\.[A-Za-z][A-Za-z0-9_]*(?:\.[A-Za-z][A-Za-z0-9_]*)*)`)
	lspTemplateCallRegexp          = regexp.MustCompile(`^\s*(?:template|block)\s+"([^"]+)"(?:\s+(.+?))?\s*-?\s*$`)
	lspDefineActionRegexp          = regexp.MustCompile(`^\s*define\s+"([^"]+)"\s*$`)
	lspLeadingDefineContractRegexp = regexp.MustCompile(`(?s)(\{\{/\*.*?@(model|dot|func|gen|symbol).*?\*/\}\})([ \t\r\n]*)(\{\{\s*(?:-)?\s*define\s+"([^"]+)"\s*(?:-)?\s*\}\})`)
	lspTypePrefixRegexp            = regexp.MustCompile(`@[A-Za-z_][A-Za-z0-9_]*\s+[A-Za-z_][A-Za-z0-9_]*\s+[A-Za-z0-9_./\[\]*-]*$`)
	lspFuncPrefixRegexp            = regexp.MustCompile(`@func\s+[A-Za-z_][A-Za-z0-9_]*\s+[A-Za-z0-9_./-]*$`)
	lspAnnotationPrefixRegexp      = regexp.MustCompile(`@\w*$`)
	lspDotTypeRegexp               = regexp.MustCompile(`@dot\s+([A-Za-z0-9_./-]+)`)
	lspFuncTypeRegexp              = regexp.MustCompile(`@func\s+[A-Za-z_][A-Za-z0-9_]*\s+([A-Za-z0-9_./-]+)`)
	lspFuncDeclarationRegexp       = regexp.MustCompile(`@func\s+([A-Za-z_][A-Za-z0-9_]*)\s+([A-Za-z0-9_./-]+)`)
	lspUnknownShorthandRegexp      = regexp.MustCompile(`(?m)^\s*@([A-Za-z_][A-Za-z0-9_]*)\s+([A-Za-z_][A-Za-z0-9_./\[\]*-]*)\s*$`)
)

type templateFuncInfo struct {
	Signature string
	Doc       string
}

var builtInTemplateFuncs = map[string]templateFuncInfo{
	"and":      {Signature: "and x y ... any", Doc: "Returns the boolean AND of its arguments."},
	"call":     {Signature: "call function arg ... any", Doc: "Calls the first argument as a function with the remaining arguments."},
	"html":     {Signature: "html value any", Doc: "Marks text as HTML. Available in html/template."},
	"index":    {Signature: "index x index ... any", Doc: "Indexes into a map, slice, or array."},
	"slice":    {Signature: "slice x start [end] any", Doc: "Slices a slice, array, or string."},
	"js":       {Signature: "js value any", Doc: "Marks text as JavaScript. Available in html/template."},
	"len":      {Signature: "len x int", Doc: "Returns the length of a string, array, slice, map, or channel."},
	"not":      {Signature: "not x bool", Doc: "Returns the boolean negation of its argument."},
	"or":       {Signature: "or x y ... any", Doc: "Returns the boolean OR of its arguments."},
	"print":    {Signature: "print arg ... string", Doc: "Formats using fmt.Sprint."},
	"printf":   {Signature: "printf format arg ... string", Doc: "Formats using fmt.Sprintf."},
	"println":  {Signature: "println arg ... string", Doc: "Formats using fmt.Sprintln."},
	"urlquery": {Signature: "urlquery value any", Doc: "Escapes text for use in URL query context."},
	"eq":       {Signature: "eq x y ... bool", Doc: "Reports whether values are equal."},
	"ne":       {Signature: "ne x y bool", Doc: "Reports whether values are not equal."},
	"lt":       {Signature: "lt x y bool", Doc: "Reports whether x is less than y."},
	"le":       {Signature: "le x y bool", Doc: "Reports whether x is less than or equal to y."},
	"gt":       {Signature: "gt x y bool", Doc: "Reports whether x is greater than y."},
	"ge":       {Signature: "ge x y bool", Doc: "Reports whether x is greater than or equal to y."},
}

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
		if msg.Method == lspShutdownMethod {
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
				"completionProvider":     map[string]any{"triggerCharacters": []string{".", "_", "$", " ", "@"}},
				"hoverProvider":          true,
				"definitionProvider":     true,
				"renameProvider":         map[string]any{"prepareProvider": true},
				"codeActionProvider":     true,
				"documentSymbolProvider": true,
				"executeCommandProvider": map[string]any{"commands": []string{lspCommandMissingFieldApplied}},
				"semanticTokensProvider": map[string]any{
					"legend": map[string]any{
						"tokenTypes":     []string{"variable", "property", "type", "function"},
						"tokenModifiers": []string{},
					},
					"full": true,
				},
			},
			"serverInfo": map[string]string{"name": lspServerName, "version": Version},
		}, nil
	case lspShutdownMethod:
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
	case "textDocument/prepareRename":
		var params textDocumentPositionParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return nil, parseError(err)
		}
		return s.prepareRename(params), nil
	case "textDocument/rename":
		var params renameParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return nil, parseError(err)
		}
		return s.rename(params), nil
	case "textDocument/codeAction":
		var params codeActionParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return nil, parseError(err)
		}
		return s.codeActions(params), nil
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
	case "workspace/executeCommand":
		var params executeCommandParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return nil, parseError(err)
		}
		return nil, s.executeCommand(params)
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
		if err := s.clearDiagnostics(params.TextDocument.URI); err != nil {
			return err
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
		return s.clearDiagnostics(params.TextDocument.URI)
	default:
		return nil
	}
}

func (s *lspServer) executeCommand(params executeCommandParams) *rpcError {
	switch params.Command {
	case lspCommandMissingFieldApplied:
		var args []missingFieldAppliedCommand
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return parseError(err)
		}
		if len(args) == 0 {
			return nil
		}
		if err := s.applyWorkspaceEdit(args[0]); err != nil {
			return &rpcError{Code: -32603, Message: err.Error()}
		}
		s.applyMissingField(args[0])
		if err := s.refreshOpenDiagnostics(); err != nil {
			return &rpcError{Code: -32603, Message: err.Error()}
		}
		_ = s.refreshSemanticTokens()
		return nil
	default:
		return &rpcError{Code: -32601, Message: "command not found"}
	}
}

func (s *lspServer) applyWorkspaceEdit(applied missingFieldAppliedCommand) error {
	if applied.TargetURI == "" || applied.NewText == "" {
		return nil
	}
	s.nextID++
	return writeRPCRequest(s.out, s.nextID, lspMethodWorkspaceApplyEdit, map[string]any{
		"label": fmt.Sprintf("Add field %s to %s", applied.FieldName, shortTypeName(applied.OwnerType)),
		"edit": workspaceEdit{Changes: map[string][]textEdit{
			applied.TargetURI: {{
				Range:   applied.Range,
				NewText: applied.NewText,
			}},
		}},
	})
}

func (s *lspServer) applyMissingField(applied missingFieldAppliedCommand) {
	root, err := filepath.Abs(applied.Root)
	if err != nil {
		root = applied.Root
	}
	apply := func(idx *indexFile) {
		if idx == nil || idx.Types == nil {
			return
		}
		owner := idx.Types[applied.OwnerType]
		if owner.Name == "" {
			return
		}
		if owner.Fields == nil {
			owner.Fields = map[string]fieldIndex{}
		}
		if _, exists := owner.Fields[applied.FieldName]; !exists {
			owner.Fields[applied.FieldName] = fieldIndex{Type: applied.FieldType, File: owner.File}
			idx.Types[applied.OwnerType] = owner
		}
	}
	if filepath.Clean(root) == filepath.Clean(s.root) {
		apply(&s.idx)
	}
	for key, cached := range s.indexes {
		if filepath.Clean(key) != filepath.Clean(root) {
			continue
		}
		apply(&cached.idx)
		s.indexes[key] = cached
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
	_, err = fmt.Fprintf(writer, rpcContentFormat, len(data), data)
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
	_, err = fmt.Fprintf(writer, rpcContentFormat, len(data), data)
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
	_, err = fmt.Fprintf(writer, rpcContentFormat, len(data), data)
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
	cfg := loadIndexConfig(root)
	if !cfg.enabled() {
		return indexFile{Version: 2, Templates: map[string]templateIndex{}, Types: map[string]goTypeIndex{}, Funcs: map[string]goFuncIndex{}, Short: map[string][]string{}}, "", time.Time{}, nil
	}
	path := filepath.Join(root, goDocDir, indexFileName)
	if cfg.WriteIndex {
		data, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return indexFile{}, path, time.Time{}, err
		}
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
	}
	idx, _, err := buildTemplateIndex(root)
	return idx, "", time.Time{}, err
}

func (s *lspServer) refreshWorkspaceState() {
	if !s.refreshIndex() {
		return
	}
	_ = s.publishOpenDiagnostics()
	_ = s.refreshSemanticTokens()
}

func (s *lspServer) refreshIndex() bool {
	if s.indexPath == "" {
		return false
	}
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

func (s *lspServer) refreshOpenDiagnostics() error {
	for uri := range s.docs {
		if err := s.clearDiagnostics(uri); err != nil {
			return err
		}
	}
	return s.publishOpenDiagnostics()
}

func (s *lspServer) clearDiagnostics(uri string) error {
	return writeRPCNotification(s.out, lspMethodPublishDiagnostics, map[string]any{"uri": uri, lspDiagnosticsKey: []diagnostic{}})
}

func (s *lspServer) refreshSemanticTokens() error {
	s.nextID++
	return writeRPCRequest(s.out, s.nextID, lspMethodSemanticTokensRefresh, nil)
}

func (s *lspServer) index() lspIndex {
	return lspIndex{indexFile: s.idx, rootPath: s.root}
}

func (s *lspServer) indexForURI(uri string) lspIndex {
	path, err := pathFromURI(uri)
	if err != nil {
		return s.index()
	}
	root := nearestModuleRoot(path)
	if root == "" {
		root = nearestIndexRoot(path)
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
	cfg := loadIndexConfig(root)
	if !cfg.enabled() {
		return lspIndex{indexFile: indexFile{Version: 2, Templates: map[string]templateIndex{}, Types: map[string]goTypeIndex{}, Funcs: map[string]goFuncIndex{}, Short: map[string][]string{}}, rootPath: root}, true
	}
	indexPath := filepath.Join(root, goDocDir, indexFileName)
	sourceMTime := latestSourceModTime(root)
	stat, statErr := os.Stat(indexPath)
	if cfg.WriteIndex && statErr == nil {
		if cached, ok := s.indexes[root]; ok &&
			cached.path == indexPath &&
			!stat.ModTime().After(cached.mtime) &&
			!sourceMTime.After(cached.sourceMTime) {
			return lspIndex{indexFile: cached.idx, rootPath: cached.root}, true
		}
		if sourceMTime.After(stat.ModTime()) {
			idx, _, err := buildTemplateIndex(root)
			if err == nil {
				s.indexes[root] = cachedLSPIndex{
					idx:         idx,
					path:        indexPath,
					root:        root,
					mtime:       stat.ModTime(),
					sourceMTime: sourceMTime,
				}
				return lspIndex{indexFile: idx, rootPath: root}, true
			}
		}
		data, err := os.ReadFile(indexPath)
		if err != nil {
			return lspIndex{}, false
		}
		var idx indexFile
		if err := json.Unmarshal(data, &idx); err != nil {
			return lspIndex{}, false
		}
		s.indexes[root] = cachedLSPIndex{idx: idx, path: indexPath, root: root, mtime: stat.ModTime(), sourceMTime: sourceMTime}
		return lspIndex{indexFile: idx, rootPath: root}, true
	}

	if cached, ok := s.indexes[root]; ok && cached.path == "" && !sourceMTime.After(cached.sourceMTime) {
		return lspIndex{indexFile: cached.idx, rootPath: cached.root}, true
	}
	idx, _, err := buildTemplateIndex(root)
	if err != nil {
		return lspIndex{}, false
	}
	s.indexes[root] = cachedLSPIndex{idx: idx, root: root, sourceMTime: sourceMTime}
	return lspIndex{indexFile: idx, rootPath: root}, true
}

func latestSourceModTime(root string) time.Time {
	cfg := loadIndexConfig(root)
	var latest time.Time
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if shouldSkipDir(root, path, d.Name(), cfg) {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".go" && !isTemplateFile(path) {
			return nil
		}
		if !shouldIncludePath(root, path, cfg) {
			return nil
		}
		info, err := d.Info()
		if err == nil && info.ModTime().After(latest) {
			latest = info.ModTime()
		}
		return nil
	})
	return latest
}

func nearestIndexRoot(path string) string {
	dir := fileDir(path)
	for dir != "" {
		if filepath.Base(dir) == goDocDir {
			dir = filepath.Dir(dir)
			continue
		}
		if fileExists(filepath.Join(dir, goDocDir, indexFileName)) {
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
	return s.contractForURIAt(uri, idx, -1)
}

func (s *lspServer) contractForURIAt(uri string, idx lspIndex, offset int) (templateIndex, bool) {
	path, err := pathFromURI(uri)
	if err != nil {
		return templateIndex{}, false
	}
	relative := rel(idx.rootPath, path)
	text, _ := s.documentText(uri)
	if contract, ok := idx.Templates[relative]; ok {
		contract = mergeInlineContract(text, idx, contract)
		return activeContractAt(text, idx, relative, contract, offset), true
	}
	for key, contract := range idx.Templates {
		if strings.HasSuffix(relative, key) {
			contract = mergeInlineContract(text, idx, contract)
			return activeContractAt(text, idx, relative, contract, offset), true
		}
	}
	contract := mergeInlineContract(text, idx, templateIndex{})
	contract = activeContractAt(text, idx, relative, contract, offset)
	return contract, contract.hasContracts()
}

func activeContractAt(text string, idx lspIndex, relative string, base templateIndex, offset int) templateIndex {
	if offset < 0 {
		return base
	}
	name := activeDefineNameAt(text, offset)
	if name == "" {
		return base
	}
	if contract, ok := templateContractForDefine(idx, relative, name); ok {
		return mergeInlineContract(text, idx, contract)
	}
	return base
}

func templateContractForDefine(idx lspIndex, relative, name string) (templateIndex, bool) {
	normalized := filepath.ToSlash(relative)
	for _, contract := range idx.Templates {
		if contract.Name == name && filepath.ToSlash(contract.Source) == normalized {
			return contract, true
		}
	}
	contract, _, ok := templateContractByName(idx, name)
	return contract, ok
}

func activeDefineNameAt(text string, offset int) string {
	offset = max(0, min(offset, len(text)))
	var stack []string
	for _, action := range lspActionPattern.FindAllStringIndex(text[:offset], -1) {
		actionText := text[action[0]:action[1]]
		if isTemplateCommentAction(actionText) {
			continue
		}
		content, _, ok := actionContent(actionText)
		if !ok {
			continue
		}
		content = strings.TrimSpace(content)
		if match := lspDefineActionRegexp.FindStringSubmatch(content); len(match) == 2 {
			stack = append(stack, match[1])
			continue
		}
		if content == "end" && len(stack) > 0 {
			stack = stack[:len(stack)-1]
		}
	}
	if len(stack) == 0 {
		return ""
	}
	return stack[len(stack)-1]
}

func mergeInlineContract(text string, idx lspIndex, base templateIndex) templateIndex {
	text = contractScanText(contractAnnotationText(text, base))
	inlineRoots := parseTypedRootMap(text, indexConfig{SymbolAnnotations: symbolAnnotationsFromAliases(idx.SymbolAliases), SymbolStrict: idx.SymbolStrict})
	roots := make(map[string]string, len(base.Roots)+len(inlineRoots))
	funcs := make(map[string]string, len(base.Funcs))
	gens := make(map[string]string, len(base.Gens))
	dot := base.Dot
	if len(inlineRoots) == 0 {
		for key, value := range base.Roots {
			roots[key] = value
		}
	}
	for key, value := range base.Funcs {
		funcs[key] = value
	}
	for key, value := range base.Gens {
		gens[key] = value
	}
	for name, typeName := range inlineRoots {
		if resolved := resolveGoType(idx, typeName); resolved != "" {
			typeName = resolved
		}
		roots[name] = typeName
	}
	if match := dotPattern.FindStringSubmatch(text); len(match) == 2 {
		dot = normalizeType(match[1])
		if resolved := resolveGoType(idx, dot); resolved != "" {
			dot = resolved
		}
	}
	for _, match := range funcPattern.FindAllStringSubmatch(text, -1) {
		if match[2] == "" {
			continue
		}
		funcs[match[1]] = normalizeType(match[2])
	}
	for _, match := range genPattern.FindAllStringSubmatch(text, -1) {
		name := match[1]
		pkg := strings.TrimSpace(match[2])
		gens[name] = pkg
		if typeName, ok := ensureGeneratedNamespaceType(&idx.indexFile, name, pkg); ok {
			roots[name] = typeName
		}
	}
	return templateIndex{Roots: roots, Dot: dot, Funcs: funcs, Gens: gens}
}

func symbolAnnotationsFromAliases(aliases map[string]string) []symbolAnnotationConfig {
	if len(aliases) == 0 {
		return nil
	}
	annotations := make([]symbolAnnotationConfig, 0, len(aliases))
	for name, typ := range aliases {
		annotations = append(annotations, symbolAnnotationConfig{Name: name, Type: typ})
	}
	return annotations
}

func contractAnnotationText(text string, base templateIndex) string {
	if base.Name != "" {
		if body, ok := defineBodyText(text, base.Name); ok {
			return body
		}
		return text
	}
	return topLevelTemplateText(text)
}

func topLevelTemplateText(text string) string {
	var out strings.Builder
	cursor := 0
	for _, block := range defineBlockRanges(text) {
		if block[0] > cursor {
			out.WriteString(text[cursor:block[0]])
		}
		cursor = max(cursor, block[1])
	}
	if cursor < len(text) {
		out.WriteString(text[cursor:])
	}
	return out.String()
}

func defineBodyText(text, name string) (string, bool) {
	for _, block := range defineBlocks(text) {
		if block.name == name {
			return text[block.bodyStart:block.bodyEnd], true
		}
	}
	return "", false
}

type defineBlock struct {
	name               string
	start, end         int
	bodyStart, bodyEnd int
}

func defineBlockRanges(text string) [][2]int {
	blocks := defineBlocks(text)
	ranges := make([][2]int, 0, len(blocks))
	for _, block := range blocks {
		ranges = append(ranges, [2]int{block.start, block.end})
	}
	return ranges
}

func defineBlocks(text string) []defineBlock {
	type openDefine struct {
		name      string
		start     int
		bodyStart int
	}
	var stack []openDefine
	var blocks []defineBlock
	for _, action := range lspActionPattern.FindAllStringIndex(text, -1) {
		actionText := text[action[0]:action[1]]
		if isTemplateCommentAction(actionText) {
			continue
		}
		content, _, ok := actionContent(actionText)
		if !ok {
			continue
		}
		content = strings.TrimSpace(content)
		if match := lspDefineActionRegexp.FindStringSubmatch(content); len(match) == 2 {
			stack = append(stack, openDefine{name: match[1], start: action[0], bodyStart: action[1]})
			continue
		}
		if content == "end" && len(stack) > 0 {
			open := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			blocks = append(blocks, defineBlock{
				name:      open.name,
				start:     open.start,
				end:       action[1],
				bodyStart: open.bodyStart,
				bodyEnd:   action[0],
			})
		}
	}
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].start < blocks[j].start })
	return blocks
}

func (s *lspServer) publishDiagnostics(uri string) error {
	text, ok := s.documentText(uri)
	if !ok {
		return nil
	}
	idx := s.indexForURI(uri)
	contract, ok := s.contractForURI(uri, idx)
	if !ok && !hasGoDocAnnotation(text) {
		return s.clearDiagnostics(uri)
	}
	relative := ""
	if path, err := pathFromURI(uri); err == nil {
		relative = rel(idx.rootPath, path)
	}
	items := diagnosticsForTextScoped(text, idx, contract, relative)
	if items == nil {
		items = []diagnostic{}
	}
	return writeRPCNotification(s.out, lspMethodPublishDiagnostics, map[string]any{"uri": uri, lspDiagnosticsKey: items})
}

func hasGoDocAnnotation(text string) bool {
	text = contractScanText(text)
	return annotationPattern.MatchString(text) ||
		dotPattern.MatchString(text) ||
		funcPattern.MatchString(text) ||
		genPattern.MatchString(text)
}

func diagnosticsForText(text string, idx lspIndex, contract templateIndex) []diagnostic {
	return diagnosticsForTextScoped(text, idx, contract, "")
}

func diagnosticsForTextScoped(text string, idx lspIndex, contract templateIndex, relative string) []diagnostic {
	var items []diagnostic
	items = append(items, leadingDefineContractDiagnostics(text)...)
	items = append(items, unknownShorthandDiagnostics(text, idx.symbolParseConfig())...)
	items = append(items, typedRootCollisionDiagnostics(text, contract, idx.symbolParseConfig())...)
	items = append(items, unusedNamedDeclarationDiagnostics(text, idx, contract, relative)...)
	for _, match := range lspDotTypeRegexp.FindAllStringSubmatchIndex(text, -1) {
		start, end := match[2], match[3]
		raw := text[start:end]
		typeName := normalizeType(raw)
		if _, ok := idx.Types[typeName]; ok {
			continue
		}
		if resolved := resolveGoType(idx, typeName); resolved != "" {
			continue
		}
		items = append(items, diagnostic{
			Range:    rangeFromOffsets(text, start, end),
			Severity: 2,
			Source:   lspSource,
			Message:  fmt.Sprintf("Unknown go-doc dot type '%s'", raw),
		})
	}
	for _, match := range lspFuncTypeRegexp.FindAllStringSubmatchIndex(text, -1) {
		start, end := match[2], match[3]
		raw := text[start:end]
		funcName := normalizeType(raw)
		if _, ok := idx.Funcs[funcName]; !ok {
			items = append(items, diagnostic{
				Range:    rangeFromOffsets(text, start, end),
				Severity: 2,
				Source:   lspSource,
				Message:  fmt.Sprintf("Unknown go-doc function '%s'", raw),
			})
		}
	}
	for _, ref := range typedRootDeclarationRefs(text, idx.symbolParseConfig()) {
		if ref.unknown {
			items = append(items, diagnostic{
				Range:    rangeFromOffsets(text, ref.annotationStart, ref.annotationEnd),
				Severity: 2,
				Source:   lspSource,
				Message:  fmt.Sprintf("Unknown go-doc annotation '@%s'", ref.annotation),
			})
			continue
		}
		if ref.typeName == "" {
			items = append(items, diagnostic{
				Range:    rangeFromOffsets(text, ref.nameStart, ref.nameEnd),
				Severity: 2,
				Source:   lspSource,
				Message:  fmt.Sprintf("@%s %s needs a type or a configured default type", ref.annotation, ref.name),
			})
			continue
		}
		if ref.typeStart >= 0 {
			if _, ok := idx.Types[ref.typeName]; ok {
				continue
			}
			if resolved := resolveGoType(idx, ref.typeName); resolved != "" {
				continue
			}
			items = append(items, diagnostic{
				Range:    rangeFromOffsets(text, ref.typeStart, ref.typeEnd),
				Severity: 2,
				Source:   lspSource,
				Message:  fmt.Sprintf(unknownTypedRootTypeFormat, ref.typeName),
			})
		}
	}
	for _, match := range lspRangePattern.FindAllStringSubmatchIndex(text, -1) {
		if text[match[2]:match[3]] != "range" {
			continue
		}
		actionContract := activeContractAt(text, idx, relative, contract, match[0])
		expression := strings.TrimSpace(text[match[4]:match[5]])
		source := sourceExpression(expression)
		sourceType := resolveExpressionValueType(idx, actionContract, source, "")
		if sourceType != "" && !isRangeable(sourceType) {
			items = append(items, diagnostic{
				Range:    rangeFromOffsets(text, match[4], match[5]),
				Severity: 2,
				Source:   lspSource,
				Message:  fmt.Sprintf("Cannot range over '%s' because it is %s", source, sourceType),
			})
		}
	}
	for _, action := range lspActionPattern.FindAllStringIndex(text, -1) {
		actionText := text[action[0]:action[1]]
		if isTemplateCommentAction(actionText) {
			continue
		}
		actionContract := activeContractAt(text, idx, relative, contract, action[0])
		currentScope := scopeAt(text, action[0], idx, actionContract)
		if item, ok := lenDiagnosticForAction(text, action[0], actionText, idx, actionContract, currentScope.dotType); ok {
			items = append(items, item)
		}
		if item, ok := functionReturnDiagnosticForAction(text, action[0], actionText, idx, actionContract); ok {
			items = append(items, item)
		}
		if item, ok := functionArgumentDiagnosticForAction(text, action[0], actionText, idx, actionContract, currentScope.dotType); ok {
			items = append(items, item)
		}
		if item, ok := nestedFunctionArgumentDiagnosticForAction(text, action[0], actionText, idx, actionContract, currentScope.dotType); ok {
			items = append(items, item)
		}
		if item, ok := methodArgumentDiagnosticForAction(text, action[0], actionText, idx, actionContract, currentScope.dotType); ok {
			items = append(items, item)
		}
		if item, ok := pipelineFunctionDiagnosticForAction(text, action[0], actionText, idx, actionContract, currentScope.dotType); ok {
			items = append(items, item)
		}
		if item, ok := templateIncludeDiagnosticForAction(text, action[0], actionText, idx, relative, actionContract); ok {
			items = append(items, item)
		}
		for _, match := range lspAccessorPattern.FindAllStringIndex(actionText, -1) {
			if inQuotedString(actionText, match[0]) {
				continue
			}
			start := action[0] + match[0]
			end := action[0] + match[1]
			token := actionText[match[0]:match[1]]
			root := tokenRoot(token)
			if looksLikeModelAccessor(root) && actionContract.Roots[root] == "" && actionContract.Funcs[root] == "" {
				items = append(items, diagnostic{
					Range:    rangeFromOffsets(text, start, start+len(root)),
					Severity: 1,
					Source:   lspSource,
					Message:  fmt.Sprintf("Unknown go-doc accessor '%s'", root),
				})
				continue
			}
			ref, ok := lastFieldReferenceForToken(text, start, end, idx, actionContract)
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
				Source:   lspSource,
				Message:  fmt.Sprintf("Unknown field '%s' on %s", ref.fieldName, owner.Name),
			})
		}
	}
	return items
}

func typedRootCollisionDiagnostics(text string, contract templateIndex, symbols symbolParseConfig) []diagnostic {
	var items []diagnostic
	for _, ref := range typedRootDeclarationRefs(text, symbols) {
		if ref.unknown {
			continue
		}
		switch {
		case builtInTemplateFuncs[ref.name].Signature != "":
			items = append(items, diagnostic{
				Range:    rangeFromOffsets(text, ref.nameStart, ref.nameEnd),
				Severity: 1,
				Source:   lspSource,
				Message:  fmt.Sprintf("Typed root name '%s' collides with built-in template function '%s'", ref.name, ref.name),
			})
		case contract.Funcs[ref.name] != "":
			items = append(items, diagnostic{
				Range:    rangeFromOffsets(text, ref.nameStart, ref.nameEnd),
				Severity: 1,
				Source:   lspSource,
				Message:  fmt.Sprintf("Typed root name '%s' collides with template function '%s'", ref.name, ref.name),
			})
		}
	}
	return items
}

func unusedNamedDeclarationDiagnostics(text string, idx lspIndex, contract templateIndex, relative string) []diagnostic {
	usedRoots, usedFuncs := usedNamedDeclarations(text, idx, contract, relative)
	var items []diagnostic

	for _, ref := range typedRootDeclarationRefs(text, idx.symbolParseConfig()) {
		if ref.unknown || ref.name == "" || contract.Roots[ref.name] == "" {
			continue
		}
		if usedRoots[ref.name] {
			continue
		}
		items = append(items, diagnostic{
			Range:    rangeFromOffsets(text, ref.nameStart, ref.nameEnd),
			Severity: 2,
			Source:   lspSource,
			Message:  fmt.Sprintf("Typed root '%s' is declared but not used", ref.name),
		})
	}

	for _, ref := range funcDeclarationRefs(text) {
		if ref.name == "" || contract.Funcs[ref.name] == "" {
			continue
		}
		if usedFuncs[ref.name] {
			continue
		}
		items = append(items, diagnostic{
			Range:    rangeFromOffsets(text, ref.nameStart, ref.nameEnd),
			Severity: 2,
			Source:   lspSource,
			Message:  fmt.Sprintf("Function '%s' is declared but not used", ref.name),
		})
	}

	return items
}

func usedNamedDeclarations(text string, idx lspIndex, contract templateIndex, relative string) (map[string]bool, map[string]bool) {
	usedRoots := make(map[string]bool)
	usedFuncs := make(map[string]bool)
	for _, action := range lspActionPattern.FindAllStringIndex(text, -1) {
		actionText := text[action[0]:action[1]]
		if isTemplateCommentAction(actionText) {
			continue
		}
		actionContract := activeContractAt(text, idx, relative, contract, action[0])
		for _, token := range templateFunctionTokensInAction(actionText, idx, actionContract) {
			switch {
			case actionContract.Roots[token.name] != "":
				usedRoots[token.name] = true
			case actionContract.Funcs[token.name] != "":
				usedFuncs[token.name] = true
			}
		}
		for _, match := range lspAccessorPattern.FindAllStringIndex(actionText, -1) {
			if inQuotedString(actionText, match[0]) {
				continue
			}
			root := tokenRoot(actionText[match[0]:match[1]])
			if actionContract.Roots[root] != "" {
				usedRoots[root] = true
			}
		}
	}
	return usedRoots, usedFuncs
}

type funcDeclRef struct {
	name      string
	nameStart int
	nameEnd   int
}

func funcDeclarationRefs(text string) []funcDeclRef {
	var refs []funcDeclRef
	for _, match := range lspFuncDeclarationRegexp.FindAllStringSubmatchIndex(text, -1) {
		refs = append(refs, funcDeclRef{
			name:      text[match[2]:match[3]],
			nameStart: match[2],
			nameEnd:   match[3],
		})
	}
	return refs
}

func unknownShorthandDiagnostics(text string, symbols symbolParseConfig) []diagnostic {
	var items []diagnostic
	for _, match := range lspUnknownShorthandRegexp.FindAllStringSubmatchIndex(text, -1) {
		annotation := text[match[2]:match[3]]
		value := text[match[4]:match[5]]
		if reservedContractAnnotation(annotation) {
			continue
		}
		if _, known := symbols.Aliases[annotation]; known {
			continue
		}
		if !looksLikeTypePath(value) {
			continue
		}
		items = append(items, diagnostic{
			Range:    rangeFromOffsets(text, match[2], match[3]),
			Severity: 2,
			Source:   lspSource,
			Message:  fmt.Sprintf("Unknown go-doc annotation '@%s'; configure it in symbolAnnotations or use a named typed root", annotation),
		})
	}
	return items
}

func looksLikeTypePath(value string) bool {
	return strings.Contains(value, ".") || strings.Contains(value, "/")
}

func leadingDefineContractDiagnostics(text string) []diagnostic {
	var items []diagnostic
	for _, match := range lspLeadingDefineContractRegexp.FindAllStringSubmatchIndex(text, -1) {
		commentStart, commentEnd := match[2], match[3]
		name := text[match[10]:match[11]]
		items = append(items, diagnostic{
			Range:    rangeFromOffsets(text, commentStart, commentEnd),
			Severity: 3,
			Source:   lspSource,
			Message:  fmt.Sprintf(moveDefineAnnotationsFormat, name),
		})
	}
	return items
}

func templateIncludeDiagnosticForAction(text string, actionStart int, actionText string, idx lspIndex, relative string, contract templateIndex) (diagnostic, bool) {
	content, contentOffset, ok := actionContent(actionText)
	if !ok {
		return diagnostic{}, false
	}
	match := lspTemplateCallRegexp.FindStringSubmatchIndex(content)
	if len(match) == 0 {
		return diagnostic{}, false
	}
	name := content[match[2]:match[3]]
	child, _, ok := templateContractByNameScopedText(idx, name, relative, text)
	if !ok || child.Dot == "" {
		return diagnostic{}, false
	}
	if match[4] < 0 || match[5] < 0 {
		nameStart := actionStart + contentOffset + match[2]
		nameEnd := actionStart + contentOffset + match[3]
		expected := resolveGoType(idx, child.Dot)
		if expected == "" {
			expected = child.Dot
		}
		if expected == "" {
			return diagnostic{}, false
		}
		return diagnostic{
			Range:    rangeFromOffsets(text, nameStart, nameEnd),
			Severity: 2,
			Source:   lspSource,
			Message:  fmt.Sprintf("Template %s expects %s, got no data", name, shortTypeName(expected)),
		}, true
	}
	rawExpression := content[match[4]:match[5]]
	trimmed := strings.TrimRight(strings.TrimSpace(rawExpression), "- ")
	if trimmed == "" {
		return diagnostic{}, false
	}
	expressionLeading := strings.Index(rawExpression, strings.TrimLeft(rawExpression, " \t\r\n"))
	if expressionLeading < 0 {
		expressionLeading = 0
	}
	expressionStart := actionStart + contentOffset + match[4] + expressionLeading
	currentScope := scopeAt(text, actionStart, idx, contract)
	actual := resolveExpressionValueType(idx, contract, trimmed, currentScope.dotType)
	expected := resolveGoType(idx, child.Dot)
	if expected == "" {
		expected = child.Dot
	}
	if actual == "" || expected == "" || sameTemplateDotType(idx, actual, expected) {
		return diagnostic{}, false
	}
	return diagnostic{
		Range:    rangeFromOffsets(text, expressionStart, expressionStart+len(trimmed)),
		Severity: 2,
		Source:   lspSource,
		Message:  fmt.Sprintf("Template %s expects %s, got %s", name, shortTypeName(expected), shortTypeName(actual)),
	}, true
}

func templateContractByName(idx lspIndex, name string) (templateIndex, string, bool) {
	normalized := filepath.ToSlash(strings.TrimPrefix(name, "/"))
	for path, contract := range idx.Templates {
		templatePath := filepath.ToSlash(path)
		if contract.Name == normalized ||
			templatePath == normalized ||
			pathBase(templatePath) == normalized ||
			strings.HasSuffix(templatePath, "/"+normalized) {
			return contract, path, true
		}
	}
	return templateIndex{}, "", false
}

func templateContractByNameScoped(idx lspIndex, name, relative string) (templateIndex, string, bool) {
	normalized := filepath.ToSlash(strings.TrimPrefix(name, "/"))
	source := filepath.ToSlash(relative)
	if source != "" {
		for path, contract := range idx.Templates {
			if contract.Name == normalized && filepath.ToSlash(contract.Source) == source {
				return contract, path, true
			}
		}
	}
	return templateContractByName(idx, name)
}

func templateContractByNameScopedText(idx lspIndex, name, relative, text string) (templateIndex, string, bool) {
	normalized := filepath.ToSlash(strings.TrimPrefix(name, "/"))
	source := filepath.ToSlash(relative)
	if source != "" {
		if contract, ok := inlineDefineContractByName(text, idx, normalized, source); ok {
			return contract, source + "#" + normalized, true
		}
	}
	return templateContractByNameScoped(idx, name, relative)
}

func inlineDefineContractByName(text string, idx lspIndex, name, source string) (templateIndex, bool) {
	for _, block := range defineBlocks(text) {
		if block.name != name {
			continue
		}
		body := text[block.bodyStart:block.bodyEnd]
		roots := parseTypedRootMap(body, indexConfig{SymbolAnnotations: symbolAnnotationsFromAliases(idx.SymbolAliases), SymbolStrict: idx.SymbolStrict})
		dot := parseDot(body)
		funcs := parseFuncs(body)
		gens := parseGens(body)
		if len(roots) == 0 && dot == "" && len(funcs) == 0 && len(gens) == 0 {
			return templateIndex{}, false
		}
		if dot != "" {
			if resolved := resolveGoType(idx, dot); resolved != "" {
				dot = resolved
			}
		}
		for name, typeName := range roots {
			if resolved := resolveGoType(idx, typeName); resolved != "" {
				roots[name] = resolved
			}
		}
		for name, pkg := range gens {
			if typeName, ok := ensureGeneratedNamespaceType(&idx.indexFile, name, pkg); ok {
				roots[name] = typeName
			}
		}
		line, column := lineColumn(text, block.start)
		return templateIndex{
			Name:   name,
			Roots:  roots,
			Dot:    dot,
			Funcs:  funcs,
			Gens:   gens,
			Source: source,
			Line:   line,
			Column: column,
		}, true
	}
	return templateIndex{}, false
}

func pathBase(path string) string {
	clean := strings.TrimRight(filepath.ToSlash(path), "/")
	if clean == "" {
		return ""
	}
	if slash := strings.LastIndex(clean, "/"); slash >= 0 {
		return clean[slash+1:]
	}
	return clean
}

func sameTemplateDotType(idx lspIndex, left, right string) bool {
	left = comparableDotType(idx, left)
	right = comparableDotType(idx, right)
	return left != "" && right != "" && left == right
}

func comparableDotType(idx lspIndex, typeExpr string) string {
	normalized := stripPointer(strings.TrimSpace(typeExpr))
	if isCompositeValueType(normalized) {
		return normalized
	}
	if resolved := resolveGoType(idx, normalized); resolved != "" {
		return resolved
	}
	return normalized
}

func actionContent(actionText string) (string, int, bool) {
	start := strings.Index(actionText, "{{")
	end := strings.LastIndex(actionText, "}}")
	if start < 0 || end < start {
		return "", 0, false
	}
	contentStart := start + len("{{")
	for contentStart < end && (actionText[contentStart] == '-' || isSpaceByte(actionText[contentStart])) {
		contentStart++
	}
	contentEnd := end
	for contentEnd > contentStart && (actionText[contentEnd-1] == '-' || isSpaceByte(actionText[contentEnd-1])) {
		contentEnd--
	}
	return actionText[contentStart:contentEnd], contentStart, true
}

type templateIncludeRef struct {
	name  string
	path  string
	start int
	end   int
}

func templateIncludeReferenceAt(text string, offset int, idx lspIndex, relative string) (templateIncludeRef, bool) {
	for _, action := range lspActionPattern.FindAllStringIndex(text, -1) {
		if offset < action[0] || offset > action[1] {
			continue
		}
		actionText := text[action[0]:action[1]]
		if isTemplateCommentAction(actionText) {
			return templateIncludeRef{}, false
		}
		content, contentOffset, ok := actionContent(actionText)
		if !ok {
			return templateIncludeRef{}, false
		}
		match := lspTemplateCallRegexp.FindStringSubmatchIndex(content)
		if len(match) == 0 {
			return templateIncludeRef{}, false
		}
		nameStart := action[0] + contentOffset + match[2]
		nameEnd := action[0] + contentOffset + match[3]
		if offset < nameStart || offset > nameEnd {
			return templateIncludeRef{}, false
		}
		name := content[match[2]:match[3]]
		_, path, ok := templateContractByNameScopedText(idx, name, relative, text)
		if !ok {
			return templateIncludeRef{}, false
		}
		return templateIncludeRef{name: name, path: path, start: nameStart, end: nameEnd}, true
	}
	return templateIncludeRef{}, false
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

func looksLikeModelAccessor(root string) bool {
	if root == "" || strings.HasPrefix(root, ".") || strings.HasPrefix(root, "$") || root == "_" {
		return false
	}
	if _, ok := builtInTemplateFuncs[root]; ok {
		return false
	}
	return isIdentifierToken(root)
}

func lenDiagnosticForAction(text string, actionStart int, actionText string, idx lspIndex, contract templateIndex, dotType string) (diagnostic, bool) {
	name, _, end, ok := builtInFunctionInAction(actionText)
	if !ok || name != "len" {
		return diagnostic{}, false
	}
	argStart := end
	for argStart < len(actionText) && isSpaceByte(actionText[argStart]) {
		argStart++
	}
	argEnd := strings.LastIndex(actionText, "}}")
	if argEnd < 0 || argStart >= argEnd {
		return diagnostic{}, false
	}
	rawExpression := actionText[argStart:argEnd]
	trimmedLeft := strings.TrimLeft(rawExpression, " \t\r\n")
	leading := len(rawExpression) - len(trimmedLeft)
	expression := strings.TrimSpace(strings.TrimRight(trimmedLeft, "- "))
	if expression == "" {
		return diagnostic{}, false
	}
	expressionStart := actionStart + argStart + leading
	valueType := resolveExpressionValueType(idx, contract, expression, dotType)
	if valueType == "" || isLenable(valueType) {
		return diagnostic{}, false
	}
	return diagnostic{
		Range:    rangeFromOffsets(text, expressionStart, expressionStart+len(expression)),
		Severity: 2,
		Source:   lspSource,
		Message:  fmt.Sprintf("Cannot call len on '%s' because it is %s", expression, valueType),
	}, true
}

func functionArgumentDiagnosticForAction(text string, actionStart int, actionText string, idx lspIndex, contract templateIndex, dotType string) (diagnostic, bool) {
	name, _, end, ok := templateFunctionInAction(actionText, idx, contract)
	if !ok {
		return diagnostic{}, false
	}
	fnName := contract.Funcs[name]
	if fnName == "" {
		return diagnostic{}, false
	}
	fn := idx.Funcs[fnName]
	args := templateArgs(actionText, end)
	return functionArgumentDiagnostic(text, actionStart, name, end, fn, args, idx, contract, dotType)
}

func nestedFunctionArgumentDiagnosticForAction(text string, actionStart int, actionText string, idx lspIndex, contract templateIndex, dotType string) (diagnostic, bool) {
	close := strings.LastIndex(actionText, "}}")
	if close < 0 {
		return diagnostic{}, false
	}
	for index := 0; index < close; index++ {
		if actionText[index] != '(' || inQuotedString(actionText, index) {
			continue
		}
		parenClose := matchingCloseParen(actionText, index)
		if parenClose < 0 || parenClose > close {
			continue
		}
		name, _, end, ok := firstCommandInRange(actionText, index+1, parenClose)
		if !ok {
			continue
		}
		fnName := contract.Funcs[name]
		if fnName == "" {
			continue
		}
		fn := idx.Funcs[fnName]
		if item, ok := unsupportedFunctionReturnDiagnostic(text, actionStart, name, index+1, end, fn); ok {
			return item, true
		}
		args := templateArgsInRange(actionText, end, parenClose)
		if item, ok := functionArgumentDiagnostic(text, actionStart, name, end, fn, args, idx, contract, dotType); ok {
			return item, true
		}
	}
	return diagnostic{}, false
}

func methodArgumentDiagnosticForAction(text string, actionStart int, actionText string, idx lspIndex, contract templateIndex, dotType string) (diagnostic, bool) {
	close := strings.LastIndex(actionText, "}}")
	if close < 0 {
		return diagnostic{}, false
	}
	for _, match := range lspAccessorPattern.FindAllStringIndex(actionText, -1) {
		if inQuotedString(actionText, match[0]) {
			continue
		}
		start := actionStart + match[0]
		end := actionStart + match[1]
		ref, ok := lastFieldReferenceForToken(text, start, end, idx, contract)
		if !ok {
			continue
		}
		owner := idx.Types[ref.ownerType]
		method, ok := owner.Methods[ref.fieldName]
		if !ok {
			continue
		}
		fn := goFuncIndex{Name: ref.fieldName, Params: method.Params, Result: method.Type, ReturnOK: true}
		args := templateArgsInRange(actionText, match[1], close)
		if item, ok := functionArgumentDiagnostic(text, actionStart, ref.fieldName, match[1], fn, args, idx, contract, dotType); ok {
			return item, true
		}
	}
	return diagnostic{}, false
}

func functionArgumentDiagnostic(text string, actionStart int, name string, functionEnd int, fn goFuncIndex, args []templateArg, idx lspIndex, contract templateIndex, dotType string) (diagnostic, bool) {
	var first diagnostic
	var hasFirst bool
	for _, variant := range functionSignatureVariants(fn) {
		item, ok := functionArgumentDiagnosticSingle(text, actionStart, name, functionEnd, variant, args, idx, contract, dotType)
		if !ok {
			return diagnostic{}, false
		}
		if !hasFirst {
			first = item
			hasFirst = true
		}
	}
	return first, hasFirst
}

func functionArgumentDiagnosticSingle(text string, actionStart int, name string, functionEnd int, fn goFuncIndex, args []templateArg, idx lspIndex, contract templateIndex, dotType string) (diagnostic, bool) {
	if item, ok := functionArityDiagnostic(text, actionStart, name, functionEnd, fn, args); ok {
		return item, true
	}
	for index, arg := range args {
		expected := expectedArgumentType(fn.Params, index)
		if expected == "" {
			continue
		}
		actual := literalType(arg.Text)
		if actual == "" {
			actual = resolveExpressionValueType(idx, contract, arg.Text, dotType)
		}
		if actual == "" || argumentAssignable(idx, expected, actual) {
			continue
		}
		return diagnostic{
			Range:    rangeFromOffsets(text, actionStart+arg.Start, actionStart+arg.End),
			Severity: 2,
			Source:   lspSource,
			Message:  fmt.Sprintf("Cannot pass %s to %s argument %d because it expects %s", actual, name, index+1, expected),
		}, true
	}
	return diagnostic{}, false
}

func functionSignatureVariants(fn goFuncIndex) []goFuncIndex {
	if len(fn.Signatures) == 0 {
		return []goFuncIndex{fn}
	}
	variants := make([]goFuncIndex, 0, len(fn.Signatures))
	for _, signature := range fn.Signatures {
		variant := fn
		variant.Signature = signature.Signature
		variant.Params = signature.Params
		variant.Result = signature.Result
		variant.Results = signature.Results
		variant.ReturnOK = len(signature.Results) > 0 || signature.Result != ""
		variants = append(variants, variant)
	}
	return variants
}

func unsupportedFunctionReturnDiagnostic(text string, actionStart int, name string, functionStart, functionEnd int, fn goFuncIndex) (diagnostic, bool) {
	if templateFunctionResultType(fn) != "" {
		return diagnostic{}, false
	}
	if !fn.ReturnOK {
		return diagnostic{}, false
	}
	results := functionResults(fn)
	if len(results) == 0 {
		return diagnostic{
			Range:    rangeFromOffsets(text, actionStart+functionStart, actionStart+functionEnd),
			Severity: 2,
			Source:   lspSource,
			Message:  fmt.Sprintf("Function %s cannot be used in a template action because it returns no value", name),
		}, true
	}
	return diagnostic{
		Range:    rangeFromOffsets(text, actionStart+functionStart, actionStart+functionEnd),
		Severity: 2,
		Source:   lspSource,
		Message:  fmt.Sprintf("Function %s has unsupported template return values (%s); use one value or (value, error)", name, strings.Join(results, ", ")),
	}, true
}

func pipelineFunctionDiagnosticForAction(text string, actionStart int, actionText string, idx lspIndex, contract templateIndex, dotType string) (diagnostic, bool) {
	segments := templatePipelineSegments(actionText)
	if len(segments) < 2 {
		return diagnostic{}, false
	}
	prevType := ""
	for index, segment := range segments {
		expression := strings.TrimSpace(actionText[segment.Start:segment.End])
		if expression == "" {
			continue
		}
		name, nameStart, nameEnd, ok := firstCommandInSegment(actionText, segment)
		if !ok {
			prevType = resolveExpressionValueType(idx, contract, expression, dotType)
			continue
		}
		fnName := contract.Funcs[name]
		if fnName == "" {
			prevType = resolveExpressionValueType(idx, contract, expression, dotType)
			continue
		}
		fn := idx.Funcs[fnName]
		args := templateArgsInRange(actionText, nameEnd, segment.End)
		if index == 0 {
			prevType = templateFunctionResultType(fn)
			continue
		}
		piped := prevType != ""
		totalArgs := len(args)
		if piped {
			totalArgs++
		}
		if item, ok := functionArityDiagnosticForCount(text, actionStart+nameStart, actionStart+nameEnd, name, fn, totalArgs); ok {
			return item, true
		}
		for argIndex, arg := range args {
			expected := expectedArgumentType(fn.Params, argIndex)
			if expected == "" {
				continue
			}
			actual := literalType(arg.Text)
			if actual == "" {
				actual = resolveExpressionValueType(idx, contract, arg.Text, dotType)
			}
			if actual == "" || argumentAssignable(idx, expected, actual) {
				continue
			}
			return diagnostic{
				Range:    rangeFromOffsets(text, actionStart+arg.Start, actionStart+arg.End),
				Severity: 2,
				Source:   lspSource,
				Message:  fmt.Sprintf("Cannot pass %s to %s argument %d because it expects %s", actual, name, argIndex+1, expected),
			}, true
		}
		if piped {
			pipedIndex := totalArgs - 1
			expected := expectedArgumentType(fn.Params, pipedIndex)
			if expected != "" && !argumentAssignable(idx, expected, prevType) {
				return diagnostic{
					Range:    rangeFromOffsets(text, actionStart+segments[index-1].Start, actionStart+segments[index-1].End),
					Severity: 2,
					Source:   lspSource,
					Message:  fmt.Sprintf("Cannot pipe %s to %s argument %d because it expects %s", prevType, name, pipedIndex+1, expected),
				}, true
			}
		}
		prevType = templateFunctionResultType(fn)
	}
	return diagnostic{}, false
}

func functionReturnDiagnosticForAction(text string, actionStart int, actionText string, idx lspIndex, contract templateIndex) (diagnostic, bool) {
	name, start, end, ok := templateFunctionInAction(actionText, idx, contract)
	if !ok {
		return diagnostic{}, false
	}
	fnName := contract.Funcs[name]
	if fnName == "" {
		return diagnostic{}, false
	}
	fn := idx.Funcs[fnName]
	if templateFunctionResultType(fn) != "" {
		return diagnostic{}, false
	}
	if !fn.ReturnOK {
		return diagnostic{}, false
	}
	results := functionResults(fn)
	if len(results) == 0 {
		return diagnostic{
			Range:    rangeFromOffsets(text, actionStart+start, actionStart+end),
			Severity: 2,
			Source:   lspSource,
			Message:  fmt.Sprintf("Function %s cannot be used in a template action because it returns no value", name),
		}, true
	}
	return diagnostic{
		Range:    rangeFromOffsets(text, actionStart+start, actionStart+end),
		Severity: 2,
		Source:   lspSource,
		Message:  fmt.Sprintf("Function %s has unsupported template return values (%s); use one value or (value, error)", name, strings.Join(results, ", ")),
	}, true
}

func functionArityDiagnostic(text string, actionStart int, name string, functionEnd int, fn goFuncIndex, args []templateArg) (diagnostic, bool) {
	if item, ok := functionArityDiagnosticForCount(text, actionStart+functionEnd, actionStart+functionEnd, name, fn, len(args)); ok {
		if len(args) > 0 {
			minArgs, _ := functionArgBounds(fn)
			if len(args) < minArgs {
				last := args[len(args)-1]
				item.Range = rangeFromOffsets(text, actionStart+last.Start, actionStart+last.End)
			} else {
				extra := args[max(0, minArgs)]
				item.Range = rangeFromOffsets(text, actionStart+extra.Start, actionStart+extra.End)
			}
		}
		return item, true
	}
	return diagnostic{}, false
}

func functionArityDiagnosticForCount(text string, start, end int, name string, fn goFuncIndex, argCount int) (diagnostic, bool) {
	minArgs, maxArgs := functionArgBounds(fn)
	if argCount >= minArgs && (maxArgs < 0 || argCount <= maxArgs) {
		return diagnostic{}, false
	}
	if start == end {
		end = start + len(name)
	}
	want := fmt.Sprintf("%d", minArgs)
	if maxArgs < 0 {
		want = fmt.Sprintf("at least %d", minArgs)
	}
	return diagnostic{
		Range:    rangeFromOffsets(text, start, end),
		Severity: 2,
		Source:   lspSource,
		Message:  fmt.Sprintf("Function %s expects %s argument(s), got %d", name, want, argCount),
	}, true
}

func functionArgBounds(fn goFuncIndex) (int, int) {
	minArgs := len(fn.Params)
	maxArgs := len(fn.Params)
	variadic := len(fn.Params) > 0 && strings.HasPrefix(fn.Params[len(fn.Params)-1], "...")
	if variadic {
		minArgs--
		maxArgs = -1
	}
	return minArgs, maxArgs
}

func expectedArgumentType(params []string, index int) string {
	if index < 0 || len(params) == 0 {
		return ""
	}
	if index >= len(params) {
		if strings.HasPrefix(params[len(params)-1], "...") {
			return strings.TrimPrefix(params[len(params)-1], "...")
		}
		return ""
	}
	return strings.TrimPrefix(params[index], "...")
}

type templateArg struct {
	Text  string
	Start int
	End   int
}

func templateArgs(actionText string, start int) []templateArg {
	close := strings.LastIndex(actionText, "}}")
	if close < 0 || start >= close {
		return nil
	}
	return templateArgsInRange(actionText, start, close)
}

func templateArgsInRange(actionText string, start, close int) []templateArg {
	if start >= close {
		return nil
	}
	var args []templateArg
	argStart := -1
	inQuote := false
	escaped := false
	parenDepth := 0
	for index := start; index < close; index++ {
		ch := actionText[index]
		switch {
		case escaped:
			escaped = false
		case ch == '\\':
			escaped = true
		case ch == '"':
			inQuote = !inQuote
			if argStart < 0 {
				argStart = index
			}
		case !inQuote && ch == '(':
			parenDepth++
			if argStart < 0 {
				argStart = index
			}
		case !inQuote && ch == ')' && parenDepth > 0:
			parenDepth--
		case !inQuote && parenDepth == 0 && isSpaceByte(ch):
			if argStart >= 0 {
				appendTemplateArg(&args, actionText, argStart, index)
				argStart = -1
			}
		default:
			if argStart < 0 {
				argStart = index
			}
		}
	}
	if argStart >= 0 {
		appendTemplateArg(&args, actionText, argStart, close)
	}
	return args
}

type templateSegment struct {
	Start int
	End   int
}

func templatePipelineSegments(actionText string) []templateSegment {
	open := strings.Index(actionText, "{{")
	close := strings.LastIndex(actionText, "}}")
	if open < 0 || close < open {
		return nil
	}
	start := open + len("{{")
	for start < close && (isSpaceByte(actionText[start]) || actionText[start] == '-') {
		start++
	}
	start = skipConditionalPrefix(actionText, start, close)
	var segments []templateSegment
	segmentStart := start
	inQuote := false
	escaped := false
	parenDepth := 0
	for index := start; index < close; index++ {
		ch := actionText[index]
		switch {
		case escaped:
			escaped = false
		case ch == '\\':
			escaped = true
		case ch == '"':
			inQuote = !inQuote
		case inQuote:
		case ch == '(':
			parenDepth++
		case ch == ')' && parenDepth > 0:
			parenDepth--
		case ch == '|' && parenDepth == 0:
			appendTemplateSegment(&segments, actionText, segmentStart, index)
			segmentStart = index + 1
		}
	}
	appendTemplateSegment(&segments, actionText, segmentStart, close)
	return segments
}

func appendTemplateSegment(segments *[]templateSegment, actionText string, start, end int) {
	for start < end && isSpaceByte(actionText[start]) {
		start++
	}
	for end > start && (isSpaceByte(actionText[end-1]) || actionText[end-1] == '-') {
		end--
	}
	if start < end {
		*segments = append(*segments, templateSegment{Start: start, End: end})
	}
}

func firstCommandInSegment(actionText string, segment templateSegment) (string, int, int, bool) {
	return firstCommandInRange(actionText, segment.Start, segment.End)
}

func firstCommandInRange(actionText string, start, endLimit int) (string, int, int, bool) {
	for start < endLimit && isSpaceByte(actionText[start]) {
		start++
	}
	end := start
	for end < endLimit && !isSpaceByte(actionText[end]) {
		end++
	}
	if start == end {
		return "", 0, 0, false
	}
	name := strings.TrimSpace(actionText[start:end])
	if name == "" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "$") || strings.HasPrefix(name, `"`) || strings.HasPrefix(name, "(") || isIntegerLiteral(name) {
		return "", 0, 0, false
	}
	return name, start, end, true
}

func appendTemplateArg(args *[]templateArg, actionText string, start, end int) {
	for start < end && isSpaceByte(actionText[start]) {
		start++
	}
	for end > start && (isSpaceByte(actionText[end-1]) || actionText[end-1] == '-') {
		end--
	}
	if start >= end {
		return
	}
	*args = append(*args, templateArg{Text: actionText[start:end], Start: start, End: end})
}

func literalType(value string) string {
	value = strings.TrimSpace(value)
	switch {
	case strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`):
		return "string"
	case value == "true" || value == "false":
		return "bool"
	case isIntegerLiteral(value):
		return "int"
	default:
		return ""
	}
}

func isIntegerLiteral(value string) bool {
	if value == "" {
		return false
	}
	if value[0] == '-' || value[0] == '+' {
		value = value[1:]
	}
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func argumentAssignable(idx lspIndex, expected, actual string) bool {
	expected = normalizeComparableType(expected)
	actual = normalizeComparableType(actual)
	if expected == actual || expected == "any" || expected == "interface{}" {
		return true
	}
	resolvedExpected := resolveGoType(idx, expected)
	if resolvedExpected == "" {
		resolvedExpected = expected
	}
	resolvedActual := resolveGoType(idx, actual)
	if resolvedActual == "" {
		resolvedActual = actual
	}
	return resolvedExpected == resolvedActual
}

func normalizeComparableType(value string) string {
	return strings.TrimSpace(strings.TrimPrefix(value, "*"))
}

func isTemplateCommentAction(actionText string) bool {
	start := strings.Index(actionText, "{{")
	end := strings.LastIndex(actionText, "}}")
	if start < 0 || end < start {
		return false
	}
	body := strings.TrimSpace(actionText[start+2 : end])
	body = strings.TrimSpace(strings.Trim(body, "- "))
	return strings.HasPrefix(body, "/*")
}

func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n'
}

func (s *lspServer) completions(params textDocumentPositionParams) []completionItem {
	text, ok := s.documentText(params.TextDocument.URI)
	if !ok {
		return nil
	}
	idx := s.indexForURI(params.TextDocument.URI)
	offset := offsetAt(text, params.Position)
	if inAnnotationNamePosition(text, offset) {
		return annotationCompletionItems(idx)
	}
	if inFuncTypePosition(text, offset) {
		return functionCompletionItems(idx)
	}
	if inDeclarationTypePosition(text, offset) {
		return typeCompletionItems(idx)
	}
	contract, ok := s.contractForURIAt(params.TextDocument.URI, idx, offset)
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
	if isIdentifierToken(token) || strings.HasPrefix(token, "$") {
		return token, true
	}
	return "", false
}

func isIdentifierToken(token string) bool {
	if token == "" {
		return false
	}
	for i, r := range token {
		if i == 0 {
			if r != '_' && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
				return false
			}
			continue
		}
		if r != '_' && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func validModelName(name string) bool {
	return isIdentifierToken(name) && !strings.Contains(name, ".")
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
	names := make([]string, 0, len(builtInTemplateFuncs)+len(contract.Roots)+len(contract.Funcs)+len(contract.Roots))
	seen := make(map[string]bool, len(builtInTemplateFuncs)+len(contract.Roots)+len(contract.Funcs)+len(contract.Roots))
	for name := range contract.Funcs {
		if strings.HasPrefix(name, prefix) && !seen[name] {
			names = append(names, name)
			seen[name] = true
		}
	}
	for _, name := range contract.typedRootNames() {
		if strings.HasPrefix(name, prefix) && !seen[name] {
			names = append(names, name)
			seen[name] = true
		}
	}
	for name := range builtInTemplateFuncs {
		if strings.HasPrefix(name, prefix) && !seen[name] {
			names = append(names, name)
			seen[name] = true
		}
	}
	sort.Strings(names)
	items := make([]completionItem, 0, len(names))
	for _, name := range names {
		if fn, ok := builtInTemplateFuncs[name]; ok {
			items = append(items, completionItem{
				Label:         name,
				Kind:          3,
				Detail:        fn.Signature,
				Documentation: fn.Doc,
				InsertText:    name + " ",
			})
			continue
		}
		if fnName, ok := contract.Funcs[name]; ok {
			fn := idx.Funcs[fnName]
			detail := fnName
			if fn.Name != "" {
				detail = fn.Name
			}
			items = append(items, completionItem{
				Label:         name,
				Kind:          3,
				Detail:        detail,
				Documentation: fn.Doc,
				InsertText:    name + " ",
			})
			continue
		}
		if typeName, kind, ok := contract.typedRootType(name); ok {
			typ := idx.Types[typeName]
			detail := typeName
			doc := ""
			itemKind := 6
			if kind == "symbol" {
				doc = "Runtime-provided template symbol."
				itemKind = 3
			}
			if typ.Name != "" {
				detail = typ.Name
				if typ.Doc != "" {
					doc = typ.Doc
				}
			}
			items = append(items, completionItem{
				Label:         name,
				Kind:          itemKind,
				Detail:        detail,
				Documentation: doc,
				InsertText:    name,
			})
			continue
		}
	}
	return items
}

func inDeclarationTypePosition(text string, offset int) bool {
	before := text[:max(0, min(offset, len(text)))]
	if len(before) > 300 {
		before = before[len(before)-300:]
	}
	return lspTypePrefixRegexp.MatchString(before)
}

func inFuncTypePosition(text string, offset int) bool {
	before := text[:max(0, min(offset, len(text)))]
	if len(before) > 300 {
		before = before[len(before)-300:]
	}
	return lspFuncPrefixRegexp.MatchString(before)
}

func inAnnotationNamePosition(text string, offset int) bool {
	before := text[:max(0, min(offset, len(text)))]
	if len(before) > 80 {
		before = before[len(before)-80:]
	}
	if !lspAnnotationPrefixRegexp.MatchString(before) {
		return false
	}
	lineStart := strings.LastIndex(before, "\n")
	line := before
	if lineStart >= 0 {
		line = before[lineStart+1:]
	}
	return strings.TrimSpace(strings.TrimPrefix(line, "@")) == strings.TrimPrefix(strings.TrimSpace(line), "@")
}

func annotationCompletionItems(idx lspIndex) []completionItem {
	names := []string{"model", "dot", "func", "gen", "symbol"}
	for name := range idx.SymbolAliases {
		names = append(names, name)
	}
	sort.Strings(names)
	items := make([]completionItem, 0, len(names))
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true
		detail := "go-doc annotation"
		doc := ""
		if typ, ok := idx.SymbolAliases[name]; ok {
			detail = "configured symbol annotation"
			if typ != "" {
				doc = "Defaults to " + typ
			} else {
				doc = "Requires an explicit type in the template."
			}
		}
		items = append(items, completionItem{Label: name, Kind: 14, Detail: detail, Documentation: doc})
	}
	return items
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

func functionCompletionItems(idx lspIndex) []completionItem {
	names := make([]string, 0, len(idx.Funcs))
	for name := range idx.Funcs {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		left, right := idx.Funcs[names[i]], idx.Funcs[names[j]]
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		return names[i] < names[j]
	})
	items := make([]completionItem, 0, len(names))
	for _, fullName := range names {
		fn := idx.Funcs[fullName]
		items = append(items, completionItem{
			Label:         fullName,
			Kind:          3,
			Detail:        fn.Package,
			Documentation: fn.Doc,
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
	if ref, ok := funcReferenceAt(text, offset, idx); ok {
		fn := idx.Funcs[ref.funcName]
		signature := fn.Signature
		if signature == "" {
			signature = "func " + fn.Name
		}
		return hover{
			Contents: markdown(fmt.Sprintf("```go\n%s\n```\n%s", signature, fn.Doc)),
			Range:    rangeFromOffsets(text, ref.start, ref.end),
		}
	}
	relative := ""
	if path, err := pathFromURI(params.TextDocument.URI); err == nil {
		relative = rel(idx.rootPath, path)
	}
	if ref, ok := templateIncludeReferenceAt(text, offset, idx, relative); ok {
		child, _, _ := templateContractByNameScopedText(idx, ref.name, relative, text)
		expected := resolveGoType(idx, child.Dot)
		if expected == "" {
			expected = child.Dot
		}
		if expected == "" {
			expected = "template data"
		}
		return hover{
			Contents: markdown(fmt.Sprintf("```gotemplate\ntemplate %q\n```\nExpects `%s`.", ref.name, shortTypeName(expected))),
			Range:    rangeFromOffsets(text, ref.start, ref.end),
		}
	}
	contract, ok := s.contractForURIAt(params.TextDocument.URI, idx, offset)
	if !ok {
		return nil
	}
	if ref, ok := typedRootNameReferenceAt(text, offset, contract, idx.symbolParseConfig()); ok {
		typeName, kind, _ := contract.typedRootType(ref.name)
		typ := idx.Types[typeName]
		detail := typeName
		doc := typ.Doc
		fence := "go"
		if kind == "symbol" {
			doc = "Runtime-provided template symbol."
			fence = "gotemplate"
		}
		if typ.Name != "" {
			detail = typ.Name
			if typ.Doc != "" {
				doc = typ.Doc
			}
		}
		return hover{
			Contents: markdown(fmt.Sprintf("```%s\n%s\n```\n%s", fence, detail, doc)),
			Range:    rangeFromOffsets(text, ref.start, ref.end),
		}
	}
	if name, start, end, ok := templateFunctionAt(text, offset, idx, contract); ok {
		if fn, builtIn := builtInTemplateFuncs[name]; builtIn {
			return hover{
				Contents: markdown(fmt.Sprintf("```gotemplate\n%s\n```\n%s", fn.Signature, fn.Doc)),
				Range:    rangeFromOffsets(text, start, end),
			}
		}
		fn := idx.Funcs[contract.Funcs[name]]
		signature := functionSignatureText(fn, name)
		return hover{
			Contents: markdown(fmt.Sprintf("```go\n%s\n```\n%s", signature, fn.Doc)),
			Range:    rangeFromOffsets(text, start, end),
		}
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

func functionSignatureText(fn goFuncIndex, fallback string) string {
	if len(fn.Signatures) > 0 {
		lines := make([]string, 0, len(fn.Signatures))
		for _, signature := range fn.Signatures {
			if signature.Signature != "" {
				lines = append(lines, signature.Signature)
			}
		}
		if len(lines) > 0 {
			return strings.Join(lines, "\n")
		}
	}
	if fn.Signature != "" {
		return fn.Signature
	}
	return "func " + fallback
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
	if ref, ok := funcReferenceAt(text, offset, idx); ok {
		fn := idx.Funcs[ref.funcName]
		return locationForTarget(idx.rootPath, fn.File, fn.Line, fn.Column)
	}
	relative := ""
	if path, err := pathFromURI(params.TextDocument.URI); err == nil {
		relative = rel(idx.rootPath, path)
	}
	if ref, ok := templateIncludeReferenceAt(text, offset, idx, relative); ok {
		child, _, _ := templateContractByNameScopedText(idx, ref.name, relative, text)
		targetPath := ref.path
		targetLine := 1
		targetColumn := 1
		if child.Source != "" {
			targetPath = child.Source
			targetLine = child.Line
			targetColumn = child.Column
		}
		return locationForTarget(idx.rootPath, targetPath, targetLine, targetColumn)
	}
	contract, ok := s.contractForURIAt(params.TextDocument.URI, idx, offset)
	if !ok {
		return nil
	}
	if ref, ok := typedRootNameReferenceAt(text, offset, contract, idx.symbolParseConfig()); ok {
		if typeName, _, ok := contract.typedRootType(ref.name); ok {
			typ := idx.Types[typeName]
			return locationForTarget(idx.rootPath, typ.File, typ.Line, typ.Column)
		}
	}
	if name, _, _, ok := templateFunctionAt(text, offset, idx, contract); ok {
		if fnName := contract.Funcs[name]; fnName != "" {
			fn := idx.Funcs[fnName]
			return locationForTarget(idx.rootPath, fn.File, fn.Line, fn.Column)
		}
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

func (s *lspServer) prepareRename(params textDocumentPositionParams) any {
	text, ok := s.documentText(params.TextDocument.URI)
	if !ok {
		return nil
	}
	idx := s.indexForURI(params.TextDocument.URI)
	contract, ok := s.contractForURIAt(params.TextDocument.URI, idx, offsetAt(text, params.Position))
	if !ok {
		return nil
	}
	ref, ok := typedRootNameReferenceAt(text, offsetAt(text, params.Position), contract, idx.symbolParseConfig())
	if !ok {
		return nil
	}
	return rangeFromOffsets(text, ref.start, ref.end)
}

func (s *lspServer) rename(params renameParams) any {
	newName := strings.TrimSpace(params.NewName)
	if !validModelName(newName) {
		return workspaceEdit{Changes: map[string][]textEdit{}}
	}
	text, ok := s.documentText(params.TextDocument.URI)
	if !ok {
		return workspaceEdit{Changes: map[string][]textEdit{}}
	}
	idx := s.indexForURI(params.TextDocument.URI)
	contract, ok := s.contractForURIAt(params.TextDocument.URI, idx, offsetAt(text, params.Position))
	if !ok {
		return workspaceEdit{Changes: map[string][]textEdit{}}
	}
	ref, ok := typedRootNameReferenceAt(text, offsetAt(text, params.Position), contract, idx.symbolParseConfig())
	if !ok || ref.name == newName {
		return workspaceEdit{Changes: map[string][]textEdit{}}
	}
	edits := typedRootRenameEdits(text, contract, ref.name, newName)
	return workspaceEdit{Changes: map[string][]textEdit{params.TextDocument.URI: edits}}
}

func (s *lspServer) codeActions(params codeActionParams) []codeAction {
	text, ok := s.documentText(params.TextDocument.URI)
	if !ok {
		return nil
	}
	idx := s.indexForURI(params.TextDocument.URI)
	actionOffset := offsetAt(text, params.Range.Start)
	contract, ok := s.contractForURIAt(params.TextDocument.URI, idx, actionOffset)
	if !ok {
		return nil
	}
	relative := ""
	if path, err := pathFromURI(params.TextDocument.URI); err == nil {
		relative = rel(idx.rootPath, path)
	}
	var actions []codeAction
	seen := make(map[string]bool)
	for _, item := range diagnosticsForTextScoped(text, idx, contract, relative) {
		if len(params.Context.Diagnostics) > 0 {
			if !diagnosticListed(item, params.Context.Diagnostics) {
				continue
			}
		} else if !rangesOverlap(item.Range, params.Range) {
			continue
		}
		switch {
		case strings.HasPrefix(item.Message, unknownFieldDiagnosticPrefix):
			action, ok := missingFieldCodeAction(text, idx, contract, item)
			if ok && !seen[action.Title] {
				actions = append(actions, action)
				seen[action.Title] = true
			}
		case strings.HasPrefix(item.Message, unknownTypedRootTypePrefix):
			action, ok := missingModelCodeAction(idx, item)
			if ok && !seen[action.Title] {
				actions = append(actions, action)
				seen[action.Title] = true
			}
		case strings.HasPrefix(item.Message, moveDefineAnnotationsPrefix):
			action, ok := moveDefineContractCodeAction(params.TextDocument.URI, text, item)
			if ok && !seen[action.Title] {
				actions = append(actions, action)
				seen[action.Title] = true
			}
		}
	}
	return actions
}

func moveDefineContractCodeAction(uri string, text string, item diagnostic) (codeAction, bool) {
	warningOffset := offsetAt(text, item.Range.Start)
	for _, match := range lspLeadingDefineContractRegexp.FindAllStringSubmatchIndex(text, -1) {
		commentStart, commentEnd := match[2], match[3]
		if warningOffset < commentStart || warningOffset > commentEnd {
			continue
		}
		removeEnd := match[8]
		defineEnd := match[9]
		comment := compactContractComment(strings.TrimSpace(text[commentStart:commentEnd]))
		newText := "\n" + comment + "\n"
		return codeAction{
			Title:       moveDefineAnnotationsCodeAction,
			Kind:        lspQuickFixKind,
			Diagnostics: []diagnostic{item},
			Edit: &workspaceEdit{Changes: map[string][]textEdit{
				uri: {
					{Range: rangeFromOffsets(text, commentStart, removeEnd), NewText: ""},
					{Range: rangeFromOffsets(text, defineEnd, defineEnd), NewText: newText},
				},
			}},
		}, true
	}
	return codeAction{}, false
}

func compactContractComment(comment string) string {
	body := strings.TrimSpace(contractScanText(comment))
	if body == "" || strings.Contains(body, "\n") {
		return comment
	}
	return "{{/* " + body + " */}}"
}

func missingFieldCodeAction(text string, idx lspIndex, contract templateIndex, item diagnostic) (codeAction, bool) {
	offset := offsetAt(text, item.Range.Start)
	ref, ok := fieldReferenceAt(text, offset, idx, contract)
	if !ok {
		return codeAction{}, false
	}
	owner := idx.Types[ref.ownerType]
	if owner.File == "" || owner.Name == "" {
		return codeAction{}, false
	}
	insertOffset, ok := structFieldInsertOffset(idx.rootPath, owner.File, owner.Name)
	if !ok {
		return codeAction{}, false
	}
	fieldType := inferredFieldType(text, ref)
	newText := fmt.Sprintf("\n\t%s %s", ref.fieldName, fieldType)
	targetURI := uriFromPath(filepath.Join(idx.rootPath, filepath.FromSlash(owner.File)))
	editRange := lspRange{Start: positionAtFileOffset(idx.rootPath, owner.File, insertOffset), End: positionAtFileOffset(idx.rootPath, owner.File, insertOffset)}
	action := codeAction{
		Title:       fmt.Sprintf(missingFieldCodeActionTitleFormat, ref.fieldName, fieldType, owner.Name),
		Kind:        lspQuickFixKind,
		Diagnostics: []diagnostic{item},
		Command: &command{
			Title:   fmt.Sprintf(missingFieldCodeActionTitleFormat, ref.fieldName, fieldType, owner.Name),
			Command: lspCommandMissingFieldApplied,
			Arguments: []any{missingFieldAppliedCommand{
				Root:      idx.rootPath,
				OwnerType: ref.ownerType,
				FieldName: ref.fieldName,
				FieldType: fieldType,
				TargetURI: targetURI,
				Range:     editRange,
				NewText:   newText,
			}},
		},
	}
	return action, true
}

func missingModelCodeAction(idx lspIndex, item diagnostic) (codeAction, bool) {
	typeName := normalizeType(textFromDiagnosticMessage(item.Message))
	if typeName == "" || idx.Module == "" {
		return codeAction{}, false
	}
	separator := strings.LastIndex(typeName, ".")
	if separator < 0 || separator == len(typeName)-1 {
		return codeAction{}, false
	}
	packagePath := typeName[:separator]
	structName := typeName[separator+1:]
	if !strings.HasPrefix(packagePath, idx.Module) {
		return codeAction{}, false
	}
	relativePackage := strings.TrimPrefix(packagePath, idx.Module)
	relativePackage = strings.TrimPrefix(relativePackage, "/")
	dir := filepath.Join(idx.rootPath, filepath.FromSlash(relativePackage))
	packageName := packageNameForDir(idx.rootPath, dir, packagePath)
	targetURI := uriFromPath(filepath.Join(dir, "go_doc_models.go"))
	edit := missingModelTextEdit(dir, packageName, structName)
	action := codeAction{
		Title:       fmt.Sprintf("Create model struct %s", structName),
		Kind:        lspQuickFixKind,
		Diagnostics: []diagnostic{item},
		Edit: &workspaceEdit{Changes: map[string][]textEdit{
			targetURI: {edit},
		}},
	}
	return action, true
}

func rangesOverlap(left, right lspRange) bool {
	leftStart := positionOrder(left.Start)
	leftEnd := positionOrder(left.End)
	rightStart := positionOrder(right.Start)
	rightEnd := positionOrder(right.End)
	return leftStart <= rightEnd && rightStart <= leftEnd
}

func diagnosticListed(item diagnostic, diagnostics []diagnostic) bool {
	for _, other := range diagnostics {
		if other.Message == item.Message &&
			other.Range.Start.Line == item.Range.Start.Line &&
			other.Range.Start.Character == item.Range.Start.Character &&
			other.Range.End.Line == item.Range.End.Line &&
			other.Range.End.Character == item.Range.End.Character {
			return true
		}
	}
	return false
}

func positionOrder(pos position) int {
	return pos.Line*1_000_000 + pos.Character
}

func textFromDiagnosticMessage(message string) string {
	start := strings.Index(message, "'")
	end := strings.LastIndex(message, "'")
	if start < 0 || end <= start {
		return ""
	}
	return message[start+1 : end]
}

func structFieldInsertOffset(root, file, typeName string) (int, bool) {
	path := filepath.Join(root, filepath.FromSlash(file))
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return 0, false
	}
	var insertOffset int
	ast.Inspect(parsed, func(node ast.Node) bool {
		if insertOffset != 0 {
			return false
		}
		spec, ok := node.(*ast.TypeSpec)
		if !ok || spec.Name == nil || spec.Name.Name != typeName {
			return true
		}
		structType, ok := spec.Type.(*ast.StructType)
		if !ok {
			return false
		}
		file := fset.File(structType.End())
		if file == nil {
			return false
		}
		insertOffset = file.Offset(structType.End() - 1)
		return false
	})
	return insertOffset, insertOffset != 0
}

func positionAtFileOffset(root, file string, offset int) position {
	path := filepath.Join(root, filepath.FromSlash(file))
	data, err := os.ReadFile(path)
	if err != nil {
		return position{}
	}
	return positionAt(string(data), offset)
}

func inferredFieldType(text string, ref fieldRef) string {
	actionStart := strings.LastIndex(text[:ref.start], "{{")
	actionEnd := strings.Index(text[ref.end:], "}}")
	if actionStart < 0 || actionEnd < 0 {
		return "string"
	}
	action := strings.TrimSpace(strings.Trim(text[actionStart+2:ref.end+actionEnd], "- "))
	switch {
	case strings.HasPrefix(action, "if "):
		return "bool"
	case strings.HasPrefix(action, "range "):
		return "[]string"
	default:
		return "string"
	}
}

func packageNameForDir(root, dir, packagePath string) string {
	files, err := os.ReadDir(dir)
	if err == nil {
		for _, file := range files {
			if file.IsDir() || filepath.Ext(file.Name()) != ".go" || strings.HasSuffix(file.Name(), "_test.go") {
				continue
			}
			fset := token.NewFileSet()
			parsed, err := parser.ParseFile(fset, filepath.Join(dir, file.Name()), nil, parser.PackageClauseOnly)
			if err == nil && parsed.Name != nil {
				return parsed.Name.Name
			}
		}
	}
	if dir == root {
		base := filepath.Base(root)
		return sanitizePackageName(base)
	}
	return sanitizePackageName(filepath.Base(packagePath))
}

func sanitizePackageName(name string) string {
	name = strings.Map(func(r rune) rune {
		if r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, name)
	name = strings.Trim(name, "_")
	if name == "" || (name[0] >= '0' && name[0] <= '9') {
		return "models"
	}
	return name
}

func missingModelTextEdit(dir, packageName, structName string) textEdit {
	path := filepath.Join(dir, "go_doc_models.go")
	newType := fmt.Sprintf("// %s is rendered by go-doc templates.\ntype %s struct {\n}\n", structName, structName)
	data, err := os.ReadFile(path)
	if err != nil {
		return textEdit{
			Range:   lspRange{},
			NewText: fmt.Sprintf("package %s\n\n%s", packageName, newType),
		}
	}
	text := string(data)
	offset := len(text)
	prefix := "\n\n"
	if strings.HasSuffix(text, "\n\n") {
		prefix = ""
	} else if strings.HasSuffix(text, "\n") {
		prefix = "\n"
	}
	pos := positionAt(text, offset)
	return textEdit{
		Range:   lspRange{Start: pos, End: pos},
		NewText: prefix + newType,
	}
}

func (s *lspServer) documentSymbols(params documentSymbolParams) []documentSymbol {
	idx := s.indexForURI(params.TextDocument.URI)
	contract, ok := s.contractForURI(params.TextDocument.URI, idx)
	if !ok {
		return nil
	}
	var symbols []documentSymbol
	for _, name := range contract.typedRootNames() {
		typeName, kind, _ := contract.typedRootType(name)
		rng := lspRange{Start: position{}, End: position{}}
		symbolKind := 13
		if kind == "symbol" {
			symbolKind = 12
		}
		symbols = append(symbols, documentSymbol{Name: name, Detail: typeName, Kind: symbolKind, Range: rng, SelectionRange: rng})
	}
	return symbols
}

func (s *lspServer) semanticTokens(params semanticTokensParams) semanticTokens {
	text, ok := s.documentText(params.TextDocument.URI)
	if !ok {
		return semanticTokens{}
	}
	idx := s.indexForURI(params.TextDocument.URI)
	contract, _ := s.contractForURI(params.TextDocument.URI, idx)
	relative := ""
	if path, err := pathFromURI(params.TextDocument.URI); err == nil {
		relative = rel(idx.rootPath, path)
	}
	tokens := semanticTokensForTextScoped(text, idx, contract, relative)
	return semanticTokens{Data: encodeSemanticTokens(text, tokens)}
}

func semanticTokensForText(text string, idx lspIndex, contract templateIndex) []semanticToken {
	return semanticTokensForTextScoped(text, idx, contract, "")
}

func semanticTokensForTextScoped(text string, idx lspIndex, contract templateIndex, relative string) []semanticToken {
	var tokens []semanticToken
	for _, ref := range typedRootDeclarationRefs(text, idx.symbolParseConfig()) {
		if ref.unknown {
			continue
		}
		if contract.Roots[ref.name] != "" {
			tokens = append(tokens, semanticToken{start: ref.nameStart, length: ref.nameEnd - ref.nameStart, tokenType: semanticAccessor})
		}
		if ref.typeStart >= 0 && ref.typeName != "" {
			if resolveGoType(idx, ref.typeName) != "" || idx.Types[ref.typeName].Name != "" {
				shortStart := ref.typeStart + len(ref.typeName) - len(shortTypeName(ref.typeName))
				tokens = append(tokens, semanticToken{start: shortStart, length: ref.typeEnd - shortStart, tokenType: semanticType})
			}
		}
	}
	for _, match := range lspDotTypeRegexp.FindAllStringSubmatchIndex(text, -1) {
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
	for _, match := range lspFuncTypeRegexp.FindAllStringSubmatchIndex(text, -1) {
		start, end := match[2], match[3]
		raw := text[start:end]
		funcName := normalizeType(raw)
		if _, ok := idx.Funcs[funcName]; !ok {
			continue
		}
		shortStart := start + len(raw) - len(shortTypeName(raw))
		tokens = append(tokens, semanticToken{start: shortStart, length: end - shortStart, tokenType: semanticFunction})
	}
	for _, match := range lspFuncDeclarationRegexp.FindAllStringSubmatchIndex(text, -1) {
		nameStart, nameEnd := match[2], match[3]
		funcType := normalizeType(text[match[4]:match[5]])
		if _, ok := idx.Funcs[funcType]; !ok {
			continue
		}
		tokens = append(tokens, semanticToken{start: nameStart, length: nameEnd - nameStart, tokenType: semanticFunction})
	}
	for _, ref := range typedRootDeclarationRefs(text, idx.symbolParseConfig()) {
		if ref.unknown {
			continue
		}
		if ref.typeStart >= 0 && ref.typeName != "" {
			if resolveGoType(idx, ref.typeName) != "" || idx.Types[ref.typeName].Name != "" {
				shortStart := ref.typeStart + len(ref.typeName) - len(shortTypeName(ref.typeName))
				tokens = append(tokens, semanticToken{start: shortStart, length: ref.typeEnd - shortStart, tokenType: semanticType})
			}
		}
		tokens = append(tokens, semanticToken{start: ref.nameStart, length: ref.nameEnd - ref.nameStart, tokenType: semanticFunction})
	}
	for _, action := range lspActionPattern.FindAllStringIndex(text, -1) {
		actionText := text[action[0]:action[1]]
		if isTemplateCommentAction(actionText) {
			continue
		}
		actionContract := activeContractAt(text, idx, relative, contract, action[0])
		for _, token := range templateFunctionTokensInAction(actionText, idx, actionContract) {
			tokens = append(tokens, semanticToken{start: action[0] + token.start, length: token.end - token.start, tokenType: semanticFunction})
		}
		for _, match := range lspAccessorPattern.FindAllStringIndex(actionText, -1) {
			if inQuotedString(actionText, match[0]) {
				continue
			}
			start := action[0] + match[0]
			end := action[0] + match[1]
			token := actionText[match[0]:match[1]]
			root := tokenRoot(token)
			if _, _, ok := actionContract.typedRootType(root); ok || strings.HasPrefix(root, "$") {
				tokens = append(tokens, semanticToken{start: start, length: len(root), tokenType: semanticAccessor})
			}
			for _, ref := range fieldReferencesForToken(text, start, end, idx, actionContract) {
				owner := idx.Types[ref.ownerType]
				if hasMember(owner, ref.fieldName) {
					tokens = append(tokens, semanticToken{start: ref.start, length: ref.end - ref.start, tokenType: semanticField})
				}
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

func builtInFunctionAt(text string, offset int) (string, int, int, bool) {
	for _, action := range lspActionPattern.FindAllStringIndex(text, -1) {
		if offset < action[0] || offset > action[1] {
			continue
		}
		name, start, end, ok := builtInFunctionInAction(text[action[0]:action[1]])
		if !ok {
			return "", 0, 0, false
		}
		start += action[0]
		end += action[0]
		if offset >= start && offset <= end {
			return name, start, end, true
		}
	}
	return "", 0, 0, false
}

func templateFunctionAt(text string, offset int, idx lspIndex, contract templateIndex) (string, int, int, bool) {
	for _, action := range lspActionPattern.FindAllStringIndex(text, -1) {
		if offset < action[0] || offset > action[1] {
			continue
		}
		for _, token := range templateFunctionTokensInAction(text[action[0]:action[1]], idx, contract) {
			start := action[0] + token.start
			end := action[0] + token.end
			if offset >= start && offset <= end {
				return token.name, start, end, true
			}
		}
	}
	return "", 0, 0, false
}

type templateFunctionToken struct {
	name       string
	start, end int
}

func templateFunctionTokensInAction(actionText string, idx lspIndex, contract templateIndex) []templateFunctionToken {
	start := strings.Index(actionText, "{{")
	if start < 0 {
		return nil
	}
	end := strings.LastIndex(actionText, "}}")
	if end < start {
		return nil
	}
	var tokens []templateFunctionToken
	for cursor := start + len("{{"); cursor < end; {
		if inQuotedString(actionText, cursor) || !isTokenChar(actionText[cursor]) || actionText[cursor] == '.' || actionText[cursor] == '$' {
			cursor++
			continue
		}
		tokenStart := cursor
		for cursor < end && isTokenChar(actionText[cursor]) {
			cursor++
		}
		name := actionText[tokenStart:cursor]
		candidateName, candidateEnd := templateFunctionTokenCandidate(name, tokenStart, cursor)
		if _, ok := builtInTemplateFuncs[candidateName]; ok {
			tokens = append(tokens, templateFunctionToken{name: candidateName, start: tokenStart, end: candidateEnd})
			continue
		}
		if fn := contract.Funcs[candidateName]; fn != "" {
			if _, ok := idx.Funcs[fn]; ok {
				tokens = append(tokens, templateFunctionToken{name: candidateName, start: tokenStart, end: candidateEnd})
			}
			continue
		}
		if contract.Roots[name] != "" {
			tokens = append(tokens, templateFunctionToken{name: name, start: tokenStart, end: cursor})
		}
	}
	return tokens
}

func templateFunctionTokenCandidate(token string, start, end int) (string, int) {
	if dot := strings.Index(token, "."); dot > 0 {
		return token[:dot], start + dot
	}
	return token, end
}

func templateFunctionInAction(actionText string, idx lspIndex, contract templateIndex) (string, int, int, bool) {
	name, start, end, ok := firstCommandInAction(actionText)
	if !ok {
		return "", 0, 0, false
	}
	if _, ok := builtInTemplateFuncs[name]; ok {
		return name, start, end, true
	}
	if fn := contract.Funcs[name]; fn != "" {
		if _, ok := idx.Funcs[fn]; ok {
			return name, start, end, true
		}
	}
	if contract.Roots[name] != "" {
		return name, start, end, true
	}
	return "", 0, 0, false
}

func builtInFunctionInAction(actionText string) (string, int, int, bool) {
	name, start, end, ok := firstCommandInAction(actionText)
	if !ok {
		return "", 0, 0, false
	}
	if _, ok := builtInTemplateFuncs[name]; !ok {
		return "", 0, 0, false
	}
	return name, start, end, true
}

func firstCommandInAction(actionText string) (string, int, int, bool) {
	start := strings.Index(actionText, "{{")
	if start < 0 {
		return "", 0, 0, false
	}
	start += len("{{")
	for start < len(actionText) && (actionText[start] == '-' || actionText[start] == ' ' || actionText[start] == '\t' || actionText[start] == '\r' || actionText[start] == '\n') {
		start++
	}
	close := strings.LastIndex(actionText, "}}")
	if close < 0 {
		close = len(actionText)
	}
	start = skipConditionalPrefix(actionText, start, close)
	for start < close && (actionText[start] == '-' || isSpaceByte(actionText[start])) {
		start++
	}
	end := start
	for end < len(actionText) && isTokenChar(actionText[end]) {
		end++
	}
	if start == end {
		return "", 0, 0, false
	}
	return actionText[start:end], start, end, true
}

func skipConditionalPrefix(text string, start, end int) int {
	token, tokenStart, tokenEnd, ok := nextActionToken(text, start, end)
	if !ok {
		return start
	}
	switch token {
	case "if":
		return tokenEnd
	case "else":
		next, _, nextEnd, ok := nextActionToken(text, tokenEnd, end)
		if ok && next == "if" {
			return nextEnd
		}
		return tokenStart
	default:
		return tokenStart
	}
}

func nextActionToken(text string, start, end int) (string, int, int, bool) {
	for start < end && (isSpaceByte(text[start]) || text[start] == '-') {
		start++
	}
	tokenStart := start
	for start < end && isTokenChar(text[start]) {
		start++
	}
	if tokenStart == start {
		return "", 0, 0, false
	}
	return text[tokenStart:start], tokenStart, start, true
}

func inQuotedString(text string, offset int) bool {
	inQuote := false
	escaped := false
	for i := 0; i < offset && i < len(text); i++ {
		switch {
		case escaped:
			escaped = false
		case text[i] == '\\':
			escaped = true
		case text[i] == '"':
			inQuote = !inQuote
		}
	}
	return inQuote
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
	if inner, ok := unwrapParenthesizedExpression(clean); ok {
		return resolveExpressionValueType(idx, contract, inner, dotType)
	}
	if rootType, path, ok := parenthesizedExpressionPath(clean); ok {
		rootType = resolveExpressionValueType(idx, contract, rootType, dotType)
		if rootType == "" {
			return ""
		}
		return resolveFieldValuePath(idx, rootType, path)
	}
	if commandType := resolveFunctionCommandValueType(idx, contract, clean); commandType != "" {
		return commandType
	}
	parts := strings.FieldsFunc(clean, func(r rune) bool { return r == '.' })
	if len(parts) == 0 {
		return ""
	}
	root := parts[0]
	var rootType string
	var path []string
	switch {
	case strings.HasPrefix(root, "$"):
		rootType = contractRootType(contract, root)
		path = parts[1:]
	case strings.HasPrefix(clean, "."):
		rootType = dotType
		path = parts
	default:
		rootType = contractRootType(contract, root)
		path = parts[1:]
		if rootType == "" {
			rootType = functionResultValueType(idx, contract, root)
		}
	}
	if rootType == "" {
		return ""
	}
	return resolveFieldValuePath(idx, rootType, path)
}

func contractRootType(contract templateIndex, root string) string {
	if root == "" {
		return ""
	}
	if typeName, _, ok := contract.typedRootType(root); ok {
		return typeName
	}
	return ""
}

func resolveExpressionType(idx lspIndex, contract templateIndex, expression, dotType string) string {
	return resolveGoType(idx, resolveExpressionValueType(idx, contract, expression, dotType))
}

func unwrapParenthesizedExpression(expression string) (string, bool) {
	clean := strings.TrimSpace(expression)
	if !strings.HasPrefix(clean, "(") {
		return "", false
	}
	close := matchingCloseParen(clean, 0)
	if close < 0 || strings.TrimSpace(clean[close+1:]) != "" {
		return "", false
	}
	return strings.TrimSpace(clean[1:close]), true
}

func parenthesizedExpressionPath(expression string) (string, []string, bool) {
	clean := strings.TrimSpace(expression)
	if !strings.HasPrefix(clean, "(") {
		return "", nil, false
	}
	close := matchingCloseParen(clean, 0)
	if close < 0 || close+1 >= len(clean) || clean[close+1] != '.' {
		return "", nil, false
	}
	path := strings.FieldsFunc(clean[close+1:], func(r rune) bool { return r == '.' })
	if len(path) == 0 {
		return "", nil, false
	}
	return strings.TrimSpace(clean[1:close]), path, true
}

func matchingCloseParen(text string, open int) int {
	if open < 0 || open >= len(text) || text[open] != '(' {
		return -1
	}
	depth := 0
	inQuote := false
	escaped := false
	for index := open; index < len(text); index++ {
		ch := text[index]
		switch {
		case escaped:
			escaped = false
		case ch == '\\':
			escaped = true
		case ch == '"':
			inQuote = !inQuote
		case inQuote:
		case ch == '(':
			depth++
		case ch == ')':
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

func resolveFunctionCommandValueType(idx lspIndex, contract templateIndex, expression string) string {
	fields := strings.Fields(expression)
	if len(fields) == 0 {
		return ""
	}
	if len(fields) == 1 && strings.Contains(fields[0], ".") {
		return ""
	}
	return functionResultValueType(idx, contract, fields[0])
}

func functionResultValueType(idx lspIndex, contract templateIndex, name string) string {
	fnName := contract.Funcs[name]
	if fnName == "" {
		return ""
	}
	result := templateFunctionResultType(idx.Funcs[fnName])
	if result == "" {
		return ""
	}
	if isCompositeValueType(result) {
		return result
	}
	if resolved := resolveGoType(idx, result); resolved != "" {
		return resolved
	}
	return result
}

func templateFunctionResultType(fn goFuncIndex) string {
	results := functionResults(fn)
	switch {
	case len(results) == 1:
		return results[0]
	case len(results) == 2 && results[1] == "error":
		return results[0]
	default:
		return ""
	}
}

func functionResults(fn goFuncIndex) []string {
	if len(fn.Signatures) > 0 {
		return fn.Signatures[0].Results
	}
	if len(fn.Results) > 0 {
		return fn.Results
	}
	if fn.Result != "" && !fn.ReturnOK {
		return []string{fn.Result}
	}
	return nil
}

func isCompositeValueType(typeExpr string) bool {
	normalized := stripPointer(strings.TrimSpace(typeExpr))
	return strings.HasPrefix(normalized, "[]") || strings.HasPrefix(normalized, "[") || strings.HasPrefix(normalized, "map[")
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

func isLenable(typeExpr string) bool {
	normalized := stripPointer(strings.TrimSpace(typeExpr))
	return normalized == "string" ||
		strings.HasPrefix(normalized, "[]") ||
		strings.HasPrefix(normalized, "[") ||
		strings.HasPrefix(normalized, "map[") ||
		strings.HasPrefix(normalized, "chan ") ||
		strings.HasPrefix(normalized, "<-chan ") ||
		strings.HasPrefix(normalized, "chan<- ")
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
	stack := []scope{{dotType: contract.Dot, vars: map[string]string{}}}
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
	clipStart := 0
	if len(before) > 500 {
		clipStart = len(before) - 500
		before = before[len(before)-500:]
	}
	token := trailingToken(before)
	if token == "" || !strings.Contains(token, ".") {
		return "", false
	}
	tokenStart := clipStart + len(before) - len(token)
	if strings.HasPrefix(token, ".") {
		if rootType, ok := parenthesizedRootBefore(text[:max(0, min(offset, len(text)))], tokenStart, idx, contract); ok {
			parts := strings.FieldsFunc(token[:strings.LastIndex(token, ".")+1], func(r rune) bool { return r == '.' })
			typeName := resolveFieldPath(idx, rootType, parts)
			return typeName, typeName != ""
		}
	}
	lastDot := strings.LastIndex(token, ".")
	chain := token[:lastDot+1]
	parts := strings.FieldsFunc(chain, func(r rune) bool { return r == '.' })
	if !strings.HasPrefix(chain, ".") {
		vars := scopeAt(text, offset, idx, contract).vars
		rootType := contractRootType(contract, parts[0])
		if rootType == "" {
			rootType = vars[parts[0]]
		}
		if rootType == "" {
			rootType = functionResultValueType(idx, contract, parts[0])
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
	token, start, end := tokenAt(text, offset)
	if token == "" {
		return fieldRef{}, false
	}
	for _, ref := range fieldReferencesForToken(text, start, end, idx, contract) {
		if offset >= ref.start && offset <= ref.end {
			return ref, true
		}
	}
	return fieldRef{}, false
}

func lastFieldReferenceForToken(text string, start, end int, idx lspIndex, contract templateIndex) (fieldRef, bool) {
	refs := fieldReferencesForToken(text, start, end, idx, contract)
	if len(refs) == 0 {
		return fieldRef{}, false
	}
	return refs[len(refs)-1], true
}

func fieldReferencesForToken(text string, start, end int, idx lspIndex, contract templateIndex) []fieldRef {
	if start < 0 || end > len(text) || start >= end {
		return nil
	}
	token := text[start:end]
	ranges := tokenIdentifierRanges(token, start)
	if len(ranges) == 0 {
		return nil
	}

	scope := scopeAt(text, start, idx, contract)
	rootType := ""
	memberRanges := ranges
	if strings.HasPrefix(token, ".") {
		if parentType, ok := parenthesizedRootBefore(text, start, idx, contract); ok {
			rootType = parentType
		} else {
			rootType = scope.dotType
		}
	} else {
		root := token[ranges[0].start-start : ranges[0].end-start]
		rootType = contractRootType(contract, root)
		if rootType == "" {
			rootType = scope.vars[root]
		}
		if rootType == "" {
			rootType = functionResultValueType(idx, contract, root)
		}
		memberRanges = ranges[1:]
	}
	if rootType == "" || len(memberRanges) == 0 {
		return nil
	}

	var refs []fieldRef
	ownerType := resolveGoType(idx, rootType)
	if ownerType == "" {
		ownerType = rootType
	}
	for _, rng := range memberRanges {
		name := text[rng.start:rng.end]
		if ownerType == "" {
			return refs
		}
		refs = append(refs, fieldRef{ownerType: ownerType, fieldName: name, start: rng.start, end: rng.end})
		owner := idx.Types[ownerType]
		valueType, ok := memberValueType(owner, name)
		if !ok {
			return refs
		}
		ownerType = resolveGoType(idx, valueType)
		if ownerType == "" {
			ownerType = valueType
		}
	}
	return refs
}

type tokenRange struct {
	start int
	end   int
}

func tokenIdentifierRanges(token string, base int) []tokenRange {
	var ranges []tokenRange
	for index := 0; index < len(token); {
		for index < len(token) && (token[index] == '.' || !isIdentifierTokenStart(token[index])) {
			index++
		}
		start := index
		for index < len(token) && isIdentifierTokenPart(token[index]) {
			index++
		}
		if start < index {
			ranges = append(ranges, tokenRange{start: base + start, end: base + index})
		}
	}
	return ranges
}

func isIdentifierTokenStart(b byte) bool {
	return b == '_' || b == '$' || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

func isIdentifierTokenPart(b byte) bool {
	return isIdentifierTokenStart(b) || (b >= '0' && b <= '9')
}

func parenthesizedRootBefore(text string, dotStart int, idx lspIndex, contract templateIndex) (string, bool) {
	if dotStart <= 0 || dotStart > len(text) || text[dotStart-1] != ')' {
		return "", false
	}
	open := matchingOpenParen(text, dotStart-1)
	if open < 0 {
		return "", false
	}
	actionStart := strings.LastIndex(text[:open], "{{")
	actionEnd := strings.LastIndex(text[:open], "}}")
	if actionStart < 0 || actionEnd > actionStart {
		return "", false
	}
	inner := strings.TrimSpace(text[open+1 : dotStart-1])
	if inner == "" {
		return "", false
	}
	rootType := resolveExpressionType(idx, contract, inner, "")
	return rootType, rootType != ""
}

func matchingOpenParen(text string, close int) int {
	if close < 0 || close >= len(text) || text[close] != ')' {
		return -1
	}
	depth := 0
	inQuote := false
	escaped := false
	for index := close; index >= 0; index-- {
		ch := text[index]
		switch {
		case escaped:
			escaped = false
		case ch == '\\':
			escaped = true
		case ch == '"':
			inQuote = !inQuote
		case inQuote:
		case ch == ')':
			depth++
		case ch == '(':
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

func typeReferenceAt(text string, offset int, idx lspIndex) (typeRef, bool) {
	for _, match := range lspDotTypeRegexp.FindAllStringSubmatchIndex(text, -1) {
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
		if offset < shortStart {
			return typeRef{}, false
		}
		start = shortStart
		return typeRef{typeName: typeName, start: start, end: end}, true
	}
	for _, ref := range typedRootDeclarationRefs(text, idx.symbolParseConfig()) {
		if ref.unknown {
			continue
		}
		if ref.typeStart < 0 || offset < ref.typeStart || offset > ref.typeEnd {
			continue
		}
		typeName := resolveGoType(idx, ref.typeName)
		if typeName == "" {
			if _, ok := idx.Types[ref.typeName]; ok {
				typeName = ref.typeName
			}
		}
		if typeName == "" {
			return typeRef{}, false
		}
		shortStart := ref.typeStart + len(ref.typeName) - len(shortTypeName(ref.typeName))
		if offset < shortStart {
			return typeRef{}, false
		}
		return typeRef{typeName: typeName, start: shortStart, end: ref.typeEnd}, true
	}
	return typeRef{}, false
}

func funcReferenceAt(text string, offset int, idx lspIndex) (funcRef, bool) {
	for _, match := range lspFuncTypeRegexp.FindAllStringSubmatchIndex(text, -1) {
		start, end := match[2], match[3]
		if offset < start || offset > end {
			continue
		}
		raw := text[start:end]
		funcName := normalizeType(raw)
		if _, ok := idx.Funcs[funcName]; !ok {
			return funcRef{}, false
		}
		shortStart := start + len(raw) - len(shortTypeName(raw))
		if offset >= shortStart {
			start = shortStart
		}
		return funcRef{funcName: funcName, start: start, end: end}, true
	}
	return funcRef{}, false
}

func typedRootNameReferenceAt(text string, offset int, contract templateIndex, symbols symbolParseConfig) (modelNameRef, bool) {
	for _, ref := range typedRootDeclarationRefs(text, symbols) {
		if ref.unknown {
			continue
		}
		if offset >= ref.nameStart && offset <= ref.nameEnd {
			return modelNameRef{name: ref.name, start: ref.nameStart, end: ref.nameEnd}, true
		}
	}

	token, start, _ := tokenAt(text, offset)
	if token == "" {
		return modelNameRef{}, false
	}
	parts := strings.FieldsFunc(token, func(r rune) bool { return r == '.' })
	if strings.HasPrefix(token, ".") || len(parts) == 0 {
		return modelNameRef{}, false
	}
	if _, _, ok := contract.typedRootType(parts[0]); !ok {
		return modelNameRef{}, false
	}
	rootEnd := start + len(parts[0])
	if offset > rootEnd {
		return modelNameRef{}, false
	}
	return modelNameRef{name: parts[0], start: start, end: rootEnd}, true
}

type symbolDeclRef struct {
	annotation      string
	name            string
	typeName        string
	annotationStart int
	annotationEnd   int
	nameStart       int
	nameEnd         int
	typeStart       int
	typeEnd         int
	unknown         bool
}

func typedRootDeclarationRefs(text string, symbols symbolParseConfig) []symbolDeclRef {
	var refs []symbolDeclRef
	for _, match := range annotationPattern.FindAllStringSubmatchIndex(text, -1) {
		annotation := text[match[2]:match[3]]
		if reservedContractAnnotation(annotation) {
			continue
		}
		defaultType, known := symbols.Aliases[annotation]
		unknown := !known
		if unknown && symbols.Strict {
			refs = append(refs, symbolDeclRef{
				annotation:      annotation,
				annotationStart: match[2],
				annotationEnd:   match[3],
				name:            text[match[4]:match[5]],
				nameStart:       match[4],
				nameEnd:         match[5],
				typeStart:       -1,
				typeEnd:         -1,
				unknown:         true,
			})
			continue
		}
		name := text[match[4]:match[5]]
		typeName := defaultType
		typeStart, typeEnd := -1, -1
		if match[6] >= 0 && match[7] >= 0 {
			typeStart, typeEnd = match[6], match[7]
			typeName = normalizeType(text[typeStart:typeEnd])
		}
		if unknown && typeName == "" {
			continue
		}
		refs = append(refs, symbolDeclRef{
			annotation:      annotation,
			name:            name,
			typeName:        typeName,
			annotationStart: match[2],
			annotationEnd:   match[3],
			nameStart:       match[4],
			nameEnd:         match[5],
			typeStart:       typeStart,
			typeEnd:         typeEnd,
		})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].nameStart < refs[j].nameStart })
	return refs
}

func typedRootRenameEdits(text string, contract templateIndex, oldName, newName string) []textEdit {
	var edits []textEdit
	add := func(start, end int) {
		edits = append(edits, textEdit{
			Range:   rangeFromOffsets(text, start, end),
			NewText: newName,
		})
	}

	for _, ref := range typedRootDeclarationRefs(text, symbolParseConfig{}) {
		if ref.name == oldName {
			add(ref.nameStart, ref.nameEnd)
		}
	}
	for _, action := range lspActionPattern.FindAllStringIndex(text, -1) {
		actionText := text[action[0]:action[1]]
		if isTemplateCommentAction(actionText) {
			continue
		}
		for _, match := range lspAccessorPattern.FindAllStringIndex(actionText, -1) {
			tokenStart := action[0] + match[0]
			token := actionText[match[0]:match[1]]
			parts := strings.FieldsFunc(token, func(r rune) bool { return r == '.' })
			if !strings.HasPrefix(token, ".") && len(parts) > 0 && parts[0] == oldName && contract.Roots[oldName] != "" {
				add(tokenStart, tokenStart+len(oldName))
			}
		}
	}
	sort.Slice(edits, func(i, j int) bool {
		left := offsetAt(text, edits[i].Range.Start)
		right := offsetAt(text, edits[j].Range.Start)
		return left < right
	})
	return compactTextEdits(text, edits)
}

func compactTextEdits(text string, edits []textEdit) []textEdit {
	if len(edits) == 0 {
		return nil
	}
	compacted := edits[:0]
	lastStart, lastEnd := -1, -1
	for _, edit := range edits {
		start := offsetAt(text, edit.Range.Start)
		end := offsetAt(text, edit.Range.End)
		if start == lastStart && end == lastEnd {
			continue
		}
		compacted = append(compacted, edit)
		lastStart, lastEnd = start, end
	}
	return compacted
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
	return location{URI: uriFromPath(targetPath(root, file)), Range: lspRange{Start: pos, End: pos}}
}

func targetPath(root, file string) string {
	file = filepath.FromSlash(file)
	if strings.HasPrefix(file, goRootMarker) {
		rest := strings.TrimLeft(strings.TrimPrefix(file, goRootMarker), `\/`)
		return filepath.Join(goRootPath(), rest)
	}
	if filepath.IsAbs(file) {
		return file
	}
	return filepath.Join(root, file)
}

func goRootPath() string {
	if root := strings.TrimSpace(os.Getenv(goRootEnv)); root != "" {
		return root
	}
	out, err := exec.Command("go", "env", goRootEnv).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
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
