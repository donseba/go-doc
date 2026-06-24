const vscode = require("vscode");
const fs = require("fs");
const path = require("path");
const cp = require("child_process");

const templateExtensions = new Set([".gohtml", ".tmpl", ".html"]);
const output = vscode.window.createOutputChannel("go-doc");
const diagnostics = vscode.languages.createDiagnosticCollection("go-doc");

let cachedIndex = null;
let rebuildTimer = null;
let installPromptOpen = false;

function activate(context) {
  context.subscriptions.push(output, diagnostics);
  const watcher = vscode.workspace.createFileSystemWatcher("**/*.{go,gohtml,tmpl,html}");

  const selector = [
    { language: "go-template", scheme: "file" },
    { language: "html", scheme: "file" },
  ];

  context.subscriptions.push(
    vscode.languages.registerCompletionItemProvider(selector, new CompletionProvider(), ".", "_", "$", " "),
    vscode.languages.registerHoverProvider(selector, new HoverProvider()),
    vscode.languages.registerDefinitionProvider(selector, new DefinitionProvider()),
    vscode.languages.registerCodeActionsProvider(selector, new CodeActionProvider(), {
      providedCodeActionKinds: [vscode.CodeActionKind.QuickFix],
    }),
    vscode.workspace.onDidOpenTextDocument(updateDiagnostics),
    vscode.workspace.onDidChangeTextDocument((event) => updateDiagnostics(event.document)),
    vscode.workspace.onDidSaveTextDocument((document) => {
      updateDiagnostics(document);
      scheduleRebuild(document.uri.fsPath);
    }),
    watcher,
    watcher.onDidChange((uri) => scheduleRebuild(uri.fsPath)),
    watcher.onDidCreate((uri) => scheduleRebuild(uri.fsPath)),
    watcher.onDidDelete((uri) => scheduleRebuild(uri.fsPath)),
    vscode.commands.registerCommand("goDoc.rebuildIndex", rebuildCurrentWorkspace),
    vscode.commands.registerCommand("goDoc.showIndexStatus", showIndexStatus),
    vscode.commands.registerCommand("goDoc.toggleAutoIndex", toggleAutoIndex),
  );

  for (const document of vscode.workspace.textDocuments) {
    updateDiagnostics(document);
  }
}

function deactivate() {
  diagnostics.dispose();
  output.dispose();
}

class CompletionProvider {
  provideCompletionItems(document, position) {
    if (!isTemplateDocument(document)) return undefined;
    const index = loadIndex(document.uri.fsPath);
    const prefix = document.getText(new vscode.Range(new vscode.Position(Math.max(0, position.line - 10), 0), position));
    if (/@(param|var)\s+[\$A-Za-z][A-Za-z0-9_]*\s+[A-Za-z0-9_./-]*$/.test(prefix)) {
      return Object.values(index.types)
        .sort((left, right) => left.name.localeCompare(right.name) || left.fqName.localeCompare(right.fqName))
        .map((type) => {
          const item = new vscode.CompletionItem(type.fqName, vscode.CompletionItemKind.Class);
          item.detail = type.pkg || type.fqName;
          item.documentation = type.doc || type.file || "";
          return item;
        });
    }

    const contract = contractForDocument(index, document.uri.fsPath);
    if (!contract) return undefined;

    const target = fieldTargetBeforeCaret(document.getText(), document.offsetAt(position), index, contract);
    if (!target) return undefined;
    const type = index.types[target.typeName];
    if (!type) return undefined;

    return Object.entries(type.fields)
      .sort(([left], [right]) => left.localeCompare(right))
      .map(([name, field]) => {
        const item = new vscode.CompletionItem(name, vscode.CompletionItemKind.Field);
        item.detail = `${type.name}.${name} ${field.type}`;
        item.documentation = field.doc || field.file || "";
        return item;
      });
  }
}

class HoverProvider {
  provideHover(document, position) {
    if (!isTemplateDocument(document)) return undefined;
    const index = loadIndex(document.uri.fsPath);
    const text = document.getText();
    const offset = document.offsetAt(position);

    const typeRef = typeReferenceAt(text, offset, index);
    if (typeRef) {
      const type = index.types[typeRef.typeName];
      if (!type) return undefined;
      return new vscode.Hover([
        new vscode.MarkdownString(`**${type.name}**\n\n\`${type.fqName}\``),
        type.doc || "No type documentation found in the Go source.",
      ]);
    }

    const contract = contractForDocument(index, document.uri.fsPath);
    if (!contract) return undefined;
    const ref = fieldReferenceAt(text, offset, index, contract);
    if (!ref) return undefined;
    const owner = index.types[ref.ownerTypeName];
    const field = owner && owner.fields[ref.fieldName];
    if (!owner || !field) return undefined;

    return new vscode.Hover([
      new vscode.MarkdownString(`**${owner.name}.${field.name}** \`${field.type}\`\n\n\`${owner.fqName}\``),
      field.doc || "No field documentation found in the Go source.",
    ]);
  }
}

class DefinitionProvider {
  provideDefinition(document, position) {
    if (!isTemplateDocument(document)) return undefined;
    const index = loadIndex(document.uri.fsPath);
    const text = document.getText();
    const offset = document.offsetAt(position);

    const typeRef = typeReferenceAt(text, offset, index);
    if (typeRef) {
      const type = index.types[typeRef.typeName];
      return type && locationForIndexTarget(index, type.file, type.line, type.column);
    }

    const contract = contractForDocument(index, document.uri.fsPath);
    if (!contract) return undefined;
    const ref = fieldReferenceAt(text, offset, index, contract);
    if (!ref) return undefined;
    const owner = index.types[ref.ownerTypeName];
    const field = owner && owner.fields[ref.fieldName];
    if (!owner || !field) return undefined;
    return locationForIndexTarget(index, field.file || owner.file, field.line, field.column);
  }
}

class CodeActionProvider {
  provideCodeActions(document, range, context) {
    return context.diagnostics
      .filter((diagnostic) => diagnostic.source === "go-doc" && diagnostic.code && diagnostic.code.replacement)
      .map((diagnostic) => {
        const action = new vscode.CodeAction(`Replace with '${diagnostic.code.replacement}'`, vscode.CodeActionKind.QuickFix);
        action.edit = new vscode.WorkspaceEdit();
        action.edit.replace(document.uri, diagnostic.range, diagnostic.code.replacement);
        action.diagnostics = [diagnostic];
        return action;
      });
  }
}

function updateDiagnostics(document) {
  if (!isTemplateDocument(document)) return;
  const index = loadIndex(document.uri.fsPath);
  const contract = contractForDocument(index, document.uri.fsPath);
  if (!contract) {
    diagnostics.set(document.uri, []);
    return;
  }

  const text = document.getText();
  const items = [];
  annotateContractTypes(document, text, index, items);
  annotateRangeTypes(document, text, contract, index, items);
  annotateAccessors(document, text, contract, index, items);
  annotateDotFields(document, text, contract, index, items);
  diagnostics.set(document.uri, items);
}

function annotateContractTypes(document, text, index, items) {
  const pattern = /@(param|var)\s+[\$A-Za-z][A-Za-z0-9_]*\s+([A-Za-z0-9_./-]+)/g;
  for (const match of text.matchAll(pattern)) {
    const typeName = match[2];
    if (resolveGoType(index, typeName) || index.types[typeName]) continue;
    items.push(newDiagnostic(document, match.index + match[0].lastIndexOf(typeName), typeName.length, `Unknown go-doc template type '${typeName}'`, vscode.DiagnosticSeverity.Warning));
  }
}

function annotateRangeTypes(document, text, contract, index, items) {
  const pattern = /\{\{\s*-?\s*range\s+([^}]*)\}\}/g;
  for (const match of text.matchAll(pattern)) {
    const expression = match[1].trim();
    const source = sourceExpression(expression);
    const sourceType = resolveExpressionValueType(index, contract, source);
    if (!sourceType || isRangeable(sourceType)) continue;
    const start = match.index + match[0].indexOf(match[1]);
    items.push(newDiagnostic(document, start, match[1].length, `Cannot range over '${source}' because it is ${sourceType}`, vscode.DiagnosticSeverity.Warning));
  }
}

function annotateAccessors(document, text, contract, index, items) {
  const pattern = /([_$][A-Za-z][A-Za-z0-9_]*)\.([A-Za-z][A-Za-z0-9_]*)/g;
  for (const match of text.matchAll(pattern)) {
    const accessor = match[1];
    const fieldName = match[2];
    const variableTypes = variableTypesAt(text, match.index, index, contract);
    const typeName = contract.accessors[accessor] || contract.vars[accessor] || variableTypes[accessor];
    if (!typeName) {
      items.push(newDiagnostic(document, match.index, accessor.length, `Unknown go-doc accessor '${accessor}'`, vscode.DiagnosticSeverity.Error));
      continue;
    }
    const type = index.types[typeName];
    if (!type) continue;
    if (!type.fields[fieldName]) {
      const start = match.index + match[0].lastIndexOf(fieldName);
      const diagnostic = newDiagnostic(document, start, fieldName.length, `Unknown field '${fieldName}' on ${type.name}`, vscode.DiagnosticSeverity.Error);
      const replacement = nearestField(fieldName, Object.keys(type.fields));
      if (replacement) diagnostic.code = { replacement };
      items.push(diagnostic);
    }
  }
}

function annotateDotFields(document, text, contract, index, items) {
  const pattern = /(?<![$A-Za-z0-9_])(?:\.[A-Za-z][A-Za-z0-9_]*)+/g;
  for (const match of text.matchAll(pattern)) {
    const dotType = dotTypeAt(text, match.index, index, contract);
    if (!dotType) continue;
    const fields = match[0].split(".").filter(Boolean);
    if (!fields.length) continue;
    const ownerType = resolveFieldPath(index, dotType, fields.slice(0, -1));
    const owner = index.types[ownerType];
    if (!owner) continue;
    const fieldName = fields[fields.length - 1];
    if (!owner.fields[fieldName]) {
      const start = match.index + match[0].lastIndexOf(fieldName);
      const diagnostic = newDiagnostic(document, start, fieldName.length, `Unknown field '${fieldName}' on ${owner.name}`, vscode.DiagnosticSeverity.Error);
      const replacement = nearestField(fieldName, Object.keys(owner.fields));
      if (replacement) diagnostic.code = { replacement };
      items.push(diagnostic);
    }
  }
}

function loadIndex(filePath) {
  const existing = cachedIndex && cachedIndex.file && fs.existsSync(cachedIndex.file) ? cachedIndex : null;
  const candidate = findIndexFile(filePath);
  if (!candidate) return emptyIndex();
  const stat = fs.statSync(candidate.file);
  if (existing && existing.file === candidate.file && existing.mtimeMs === stat.mtimeMs) return existing.index;

  try {
    const index = normalizeIndex(JSON.parse(fs.readFileSync(candidate.file, "utf8")), candidate.file, candidate.root);
    cachedIndex = { file: candidate.file, mtimeMs: stat.mtimeMs, index };
    return index;
  } catch (err) {
    output.appendLine(`failed to read go-doc index: ${err.message}`);
    return emptyIndex(candidate.file, candidate.root, err.message);
  }
}

function emptyIndex(source = null, rootPath = null, loadError = null) {
  return { types: {}, templates: {}, funcs: {}, short: {}, source, rootPath, loadError };
}

function normalizeIndex(index, source, rootPath) {
  return {
    types: index.types || {},
    templates: index.templates || {},
    funcs: index.funcs || {},
    short: index.short || {},
    source,
    rootPath,
    loadError: null,
  };
}

function findIndexFile(filePath) {
  const folders = [];
  let dir = fs.statSync(filePath).isDirectory() ? filePath : path.dirname(filePath);
  while (dir && dir !== path.dirname(dir)) {
    folders.push(dir);
    dir = path.dirname(dir);
  }
  for (const folder of folders) {
    for (const name of [".go-doc/index.json", ".partial/index.json"]) {
      const candidate = path.join(folder, name);
      if (fs.existsSync(candidate)) return { file: candidate, root: path.dirname(path.dirname(candidate)) };
    }
  }
  return null;
}

function contractForDocument(index, filePath) {
  if (!index.rootPath) return null;
  const relative = path.relative(index.rootPath, filePath).replaceAll("\\", "/");
  return index.templates[relative] || Object.entries(index.templates).find(([key]) => relative.endsWith(key))?.[1] || null;
}

function resolveExpressionType(index, contract, expression, dotType = null) {
  const valueType = resolveExpressionValueType(index, contract, expression, dotType);
  return valueType && resolveGoType(index, valueType);
}

function resolveExpressionValueType(index, contract, expression, dotType = null) {
  const clean = expression.trim();
  if (!clean) return null;
  if (clean === ".") return dotType;
  const parts = clean.split(".").filter(Boolean);
  if (!parts.length) return null;
  const root = parts[0];
  const rootType = root.startsWith("_")
    ? contract.accessors[root]
    : root.startsWith("$")
      ? contract.vars[root] || contract.accessors[root]
      : clean.startsWith(".")
        ? dotType
        : null;
  return rootType ? resolveFieldValuePath(index, rootType, parts.slice(1)) : null;
}

function resolveFieldPath(index, rootType, fields) {
  const valueType = resolveFieldValuePath(index, rootType, fields);
  return valueType && resolveGoType(index, valueType);
}

function resolveFieldValuePath(index, rootType, fields) {
  let current = rootType;
  for (let i = 0; i < fields.length; i++) {
    const type = index.types[current];
    const field = type && type.fields[fields[i]];
    if (!field) return null;
    if (i === fields.length - 1) return typeof field === "string" ? field : field.type;
    current = resolveGoType(index, typeof field === "string" ? field : field.type);
  }
  return current;
}

function resolveGoType(index, typeExpr) {
  if (!typeExpr) return null;
  const normalized = stripPointer(typeExpr).replace(/^\[\]/, "").trim();
  if (index.types[normalized]) return normalized;
  const matches = index.short[normalized] || [];
  return matches.length === 1 ? matches[0] : null;
}

function rangeElementType(index, typeExpr) {
  const normalized = stripPointer(typeExpr || "");
  if (normalized.startsWith("[]")) return resolveGoType(index, normalized.substring(2));
  if (normalized.startsWith("map[")) {
    const end = normalized.indexOf("]");
    return end >= 0 ? resolveGoType(index, normalized.substring(end + 1)) : null;
  }
  return resolveGoType(index, normalized);
}

function isRangeable(typeExpr) {
  const normalized = stripPointer(typeExpr || "");
  return normalized.startsWith("[]") || normalized.startsWith("map[");
}

function stripPointer(typeExpr) {
  return String(typeExpr).trim().replace(/^\*/, "").trim();
}

function dotTypeAt(text, offset, index, contract) {
  return scopeAt(text, offset, index, contract).dotType;
}

function variableTypesAt(text, offset, index, contract) {
  return scopeAt(text, offset, index, contract).vars;
}

function scopeAt(text, offset, index, contract) {
  const stack = [{ dotType: null, vars: {} }];
  const before = text.slice(0, Math.max(0, Math.min(offset, text.length)));
  const pattern = /\{\{\s*-?\s*(range|with|end)\b([^}]*)\}\}/g;
  for (const match of before.matchAll(pattern)) {
    const keyword = match[1];
    const expression = match[2].trim();
    const parent = stack[stack.length - 1];
    if (keyword === "range") {
      const sourceType = resolveExpressionValueType(index, contract, sourceExpression(expression), parent.dotType);
      const itemType = rangeElementType(index, sourceType);
      stack.push({ dotType: itemType, vars: { ...parent.vars, ...rangeVariables(expression, itemType) } });
    } else if (keyword === "with") {
      const valueType = resolveExpressionType(index, contract, sourceExpression(expression), parent.dotType);
      stack.push({ dotType: valueType, vars: { ...parent.vars } });
    } else if (stack.length > 1) {
      stack.pop();
    }
  }
  return stack[stack.length - 1];
}

function sourceExpression(expression) {
  return expression.includes(":=")
    ? expression.split(":=").slice(1).join(":=").trim()
    : expression.includes("=")
      ? expression.split("=").slice(1).join("=").trim()
      : expression.trim();
}

function rangeVariables(expression, elementType) {
  if (!elementType || !expression.includes(":=")) return {};
  const names = expression.split(":=")[0].split(",").map((name) => name.trim()).filter((name) => name.startsWith("$"));
  const valueName = names[names.length - 1];
  return valueName ? { [valueName]: elementType } : {};
}

function fieldTargetBeforeCaret(text, offset, index, contract) {
  const before = text.slice(0, offset).replaceAll("IntellijIdeaRulezzz", "").replaceAll("DummyIdentifier", "").trimEnd().slice(-500);
  const token = before.match(/[$A-Za-z0-9_.]+$/)?.[0];
  if (!token || !token.includes(".")) return null;
  const lastDot = token.lastIndexOf(".");
  const chain = token.slice(0, lastDot + 1);
  const typedPrefix = token.slice(lastDot + 1);
  const parts = chain.split(".").filter(Boolean);
  if (chain.startsWith("_") || chain.startsWith("$")) {
    const rootType = contract.accessors[parts[0]] || contract.vars[parts[0]] || variableTypesAt(text, offset, index, contract)[parts[0]];
    const typeName = rootType && resolveFieldPath(index, rootType, parts.slice(1));
    return typeName ? { typeName, typedPrefix } : null;
  }
  if (chain.startsWith(".")) {
    const dotType = dotTypeAt(text, offset, index, contract);
    const typeName = dotType && resolveFieldPath(index, dotType, parts);
    return typeName ? { typeName, typedPrefix } : null;
  }
  return null;
}

function fieldReferenceAt(text, offset, index, contract) {
  const token = tokenAt(text, offset);
  if (!token) return null;
  const parts = token.split(".").filter(Boolean);
  if (!parts.length) return null;
  if (token.startsWith("_") || token.startsWith("$")) {
    const rootType = contract.accessors[parts[0]] || contract.vars[parts[0]] || variableTypesAt(text, offset, index, contract)[parts[0]];
    const ownerTypeName = rootType && resolveFieldPath(index, rootType, parts.slice(1, -1));
    return ownerTypeName ? { ownerTypeName, fieldName: parts[parts.length - 1] } : null;
  }
  if (token.startsWith(".")) {
    const dotType = dotTypeAt(text, offset, index, contract);
    const ownerTypeName = dotType && resolveFieldPath(index, dotType, parts.slice(0, -1));
    return ownerTypeName ? { ownerTypeName, fieldName: parts[parts.length - 1] } : null;
  }
  return null;
}

function typeReferenceAt(text, offset, index) {
  const pattern = /@(param|var)\s+[\$A-Za-z][A-Za-z0-9_]*\s+([A-Za-z0-9_./-]+)/g;
  for (const match of text.matchAll(pattern)) {
    const start = match.index + match[0].lastIndexOf(match[2]);
    const end = start + match[2].length;
    if (offset < start || offset > end) continue;
    const typeName = resolveGoType(index, match[2]) || (index.types[match[2]] ? match[2] : null);
    return typeName ? { typeName } : null;
  }
  return null;
}

function tokenAt(text, offset) {
  let start = Math.max(0, Math.min(offset, text.length));
  let end = start;
  while (start > 0 && /[$_.A-Za-z0-9]/.test(text[start - 1])) start--;
  while (end < text.length && /[$_.A-Za-z0-9]/.test(text[end])) end++;
  const token = text.slice(start, end);
  return token.includes(".") ? token : null;
}

function locationForIndexTarget(index, file, line, column) {
  if (!index.rootPath || !file) return undefined;
  const uri = vscode.Uri.file(path.join(index.rootPath, file));
  const position = new vscode.Position(Math.max(0, (line || 1) - 1), Math.max(0, (column || 1) - 1));
  return new vscode.Location(uri, position);
}

function newDiagnostic(document, startOffset, length, message, severity) {
  const start = document.positionAt(startOffset);
  const end = document.positionAt(startOffset + length);
  const diagnostic = new vscode.Diagnostic(new vscode.Range(start, end), message, severity);
  diagnostic.source = "go-doc";
  return diagnostic;
}

function nearestField(value, candidates) {
  let best = null;
  for (const candidate of candidates) {
    const distance = levenshtein(value.toLowerCase(), candidate.toLowerCase());
    if (distance <= 2 && (!best || distance < best.distance)) best = { candidate, distance };
  }
  return best && best.candidate;
}

function levenshtein(left, right) {
  const previous = Array.from({ length: right.length + 1 }, (_, index) => index);
  let current = new Array(right.length + 1);
  for (let i = 0; i < left.length; i++) {
    current[0] = i + 1;
    for (let j = 0; j < right.length; j++) {
      const cost = left[i] === right[j] ? 0 : 1;
      current[j + 1] = Math.min(current[j] + 1, previous[j + 1] + 1, previous[j] + cost);
    }
    for (let j = 0; j < current.length; j++) previous[j] = current[j];
  }
  return previous[right.length];
}

function isTemplateDocument(document) {
  return document.uri.scheme === "file" && templateExtensions.has(path.extname(document.uri.fsPath));
}

function scheduleRebuild(filePath) {
  if (!vscode.workspace.getConfiguration("goDoc").get("autoIndex", true)) return;
  if (ignoredPath(filePath)) return;
  const root = findModuleRoot(filePath);
  if (!root) return;
  clearTimeout(rebuildTimer);
  const delay = vscode.workspace.getConfiguration("goDoc").get("debounceMilliseconds", 1200);
  rebuildTimer = setTimeout(() => rebuildIndex(root, false), delay);
}

async function rebuildCurrentWorkspace() {
  const folder = vscode.workspace.workspaceFolders && vscode.workspace.workspaceFolders[0];
  if (!folder) return;
  const root = findModuleRoot(folder.uri.fsPath) || folder.uri.fsPath;
  await rebuildIndex(root, true);
}

function rebuildIndex(root, notify) {
  const outDir = path.join(root, ".go-doc");
  const outFile = path.join(outDir, "index.json");
  fs.mkdirSync(outDir, { recursive: true });
  return new Promise((resolve) => {
    cp.execFile("go-doc", ["index", "-o", outFile, "."], { cwd: root }, (err, stdout, stderr) => {
      if (err) {
        if (err.code === "ENOENT") {
          offerInstallAndRetry(root, notify).finally(resolve);
          return;
        }
        const message = stderr || stdout || err.message;
        output.appendLine(message);
        vscode.window.showWarningMessage(`go-doc index failed: ${message.slice(0, 160)}`);
      } else if (notify) {
        vscode.window.showInformationMessage("go-doc index rebuilt");
      }
      cachedIndex = null;
      for (const document of vscode.workspace.textDocuments) updateDiagnostics(document);
      resolve();
    });
  });
}

async function offerInstallAndRetry(root, notify) {
  if (installPromptOpen) return;
  installPromptOpen = true;
  try {
    const answer = await vscode.window.showWarningMessage(
      "go-doc is not available on PATH. Install it now with `go install github.com/donseba/go-doc@latest`?",
      { modal: true },
      "Install",
    );
    if (answer !== "Install") return;

    const installed = await installGoDoc(root);
    if (!installed) return;
    await rebuildIndex(root, notify);
  } finally {
    installPromptOpen = false;
  }
}

function installGoDoc(root) {
  return new Promise((resolve) => {
    output.appendLine("Installing go-doc CLI: go install github.com/donseba/go-doc@latest");
    cp.execFile("go", ["install", "github.com/donseba/go-doc@latest"], { cwd: root }, (err, stdout, stderr) => {
      if (stdout) output.appendLine(stdout);
      if (stderr) output.appendLine(stderr);
      if (err) {
        const message = err.code === "ENOENT"
          ? "Go is not available on PATH. Install Go or add it to PATH before installing go-doc."
          : stderr || stdout || err.message;
        vscode.window.showErrorMessage(`go-doc install failed: ${message.slice(0, 180)}`);
        resolve(false);
      } else {
        vscode.window.showInformationMessage("go-doc CLI installed");
        resolve(true);
      }
    });
  });
}

function findModuleRoot(filePath) {
  let dir = fs.existsSync(filePath) && fs.statSync(filePath).isDirectory() ? filePath : path.dirname(filePath);
  while (dir && dir !== path.dirname(dir)) {
    if (fs.existsSync(path.join(dir, "go.mod"))) return dir;
    dir = path.dirname(dir);
  }
  return null;
}

function ignoredPath(filePath) {
  const parts = filePath.replaceAll("\\", "/").split("/");
  return parts.some((part) => [".git", ".idea", ".go-doc", ".partial", "build", "out", "vendor", "node_modules"].includes(part));
}

async function showIndexStatus() {
  const editor = vscode.window.activeTextEditor;
  const filePath = editor && editor.document.uri.fsPath;
  const index = loadIndex(filePath || vscode.workspace.workspaceFolders?.[0]?.uri.fsPath || process.cwd());
  const contract = filePath ? contractForDocument(index, filePath) : null;
  vscode.window.showInformationMessage(
    [
      `Source: ${index.source || "no .go-doc/index.json or .partial/index.json found"}`,
      `Index root: ${index.rootPath || "-"}`,
      `Templates: ${Object.keys(index.templates).length}`,
      `Types: ${Object.keys(index.types).length}`,
      `Contract: ${contract ? "matched" : "not matched"}`,
      `Error: ${index.loadError || "-"}`,
    ].join("\n"),
    { modal: true },
  );
}

async function toggleAutoIndex() {
  const config = vscode.workspace.getConfiguration("goDoc");
  const next = !config.get("autoIndex", true);
  await config.update("autoIndex", next, vscode.ConfigurationTarget.Workspace);
  vscode.window.showInformationMessage(`go-doc auto index ${next ? "enabled" : "disabled"}`);
}

module.exports = { activate, deactivate };
