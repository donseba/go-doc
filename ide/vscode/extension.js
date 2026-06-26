const vscode = require("vscode");
const fs = require("fs");
const path = require("path");
const cp = require("child_process");
const os = require("os");

const output = vscode.window.createOutputChannel("go-doc");

let client = null;
let rebuildTimer = null;
let installPromptOpen = false;
let extensionContext = null;
let goDocCommand = null;
let languageClientModule = null;
let activeLspCommand = null;
let activeLspVersion = "-";

async function activate(context) {
  extensionContext = context;
  context.subscriptions.push(output);
  output.appendLine(`go-doc extension activated at ${new Date().toISOString()}`);

  const watcher = vscode.workspace.createFileSystemWatcher("**/*.{go,gohtml,tmpl,html}");
  context.subscriptions.push(
    watcher,
    watcher.onDidChange((uri) => scheduleRebuild(uri.fsPath)),
    watcher.onDidCreate((uri) => scheduleRebuild(uri.fsPath)),
    watcher.onDidDelete((uri) => scheduleRebuild(uri.fsPath)),
    vscode.workspace.onDidSaveTextDocument((document) => scheduleRebuild(document.uri.fsPath)),
    vscode.commands.registerCommand("goDoc.rebuildIndex", rebuildCurrentWorkspace),
    vscode.commands.registerCommand("goDoc.showIndexStatus", showIndexStatus),
    vscode.commands.registerCommand("goDoc.toggleEnabled", toggleEnabled),
    vscode.commands.registerCommand("goDoc.toggleAutoIndex", toggleAutoIndex),
    vscode.commands.registerCommand("goDoc.restartLsp", restartLsp),
  );

  await startClient(context);
}

async function deactivate() {
  await stopClient();
  output.dispose();
}

async function startClient(context) {
  const root = workspaceRoot();
  if (!root) {
    output.appendLine("No workspace folder found; go-doc LSP not started");
    return;
  }
  if (!goDocEnabled(root)) {
    output.appendLine("go-doc disabled for this workspace");
    return;
  }

  const command = await ensureGoDoc(root, false);
  if (!command) {
    output.appendLine("go-doc CLI is not available; LSP not started");
    return;
  }
  const lspCommand = prepareLspCommand(command);

  await stopClient();
  activeLspCommand = lspCommand;
  activeLspVersion = await commandVersion(lspCommand, root);
  output.appendLine(`Starting go-doc LSP: ${lspCommand} lsp ${root} (${activeLspVersion})`);

  const lsp = loadLanguageClient();
  if (!lsp) return;

  client = new lsp.LanguageClient(
    "go-doc",
    "go-doc",
    {
      command: lspCommand,
      args: ["lsp", root],
      options: { cwd: root },
    },
    {
      documentSelector: [
        { language: "go-template", scheme: "file" },
        { language: "go-html-template", scheme: "file" },
        { language: "gotmpl", scheme: "file" },
        { language: "html", scheme: "file" },
        { scheme: "file", pattern: "**/*.gohtml" },
        { scheme: "file", pattern: "**/*.tmpl" },
      ],
      outputChannel: output,
    },
  );

  context.subscriptions.push(client.onDidChangeState((event) => {
    output.appendLine(`go-doc LSP state: ${stateName(event.oldState)} -> ${stateName(event.newState)}`);
  }));
  context.subscriptions.push(client);
  try {
    await client.start();
    output.appendLine("go-doc LSP started");
    await vscode.commands.executeCommand("editor.action.restartSemanticTokensProvider").then(undefined, () => undefined);
  } catch (err) {
    output.appendLine(`go-doc LSP failed to start: ${err.message}`);
    vscode.window.showWarningMessage(`go-doc LSP failed to start: ${err.message}`);
  }
}

async function stopClient() {
  if (!client) return;
  const current = client;
  client = null;
  activeLspCommand = null;
  activeLspVersion = "-";
  try {
    await current.stop();
  } catch (err) {
    output.appendLine(`go-doc LSP stop failed: ${err.message}`);
  }
}

function loadLanguageClient() {
  if (languageClientModule) return languageClientModule;
  try {
    languageClientModule = require("vscode-languageclient/node");
    output.appendLine("vscode-languageclient loaded");
    return languageClientModule;
  } catch (err) {
    const message = `go-doc extension could not load vscode-languageclient: ${err.message}`;
    output.appendLine(message);
    vscode.window.showErrorMessage(message);
    return null;
  }
}

function workspaceRoot() {
  const folder = vscode.workspace.workspaceFolders && vscode.workspace.workspaceFolders[0];
  if (!folder) return null;
  return findModuleRoot(folder.uri.fsPath) || folder.uri.fsPath;
}

function scheduleRebuild(filePath) {
  if (ignoredPath(filePath)) return;
  const root = findModuleRoot(filePath);
  if (!root) return;
  if (!autoIndexEnabled(root)) return;
  clearTimeout(rebuildTimer);
  const delay = vscode.workspace.getConfiguration("goDoc").get("debounceMilliseconds", 1200);
  rebuildTimer = setTimeout(() => rebuildIndex(root, false), delay);
}

function autoIndexEnabled(root) {
  if (vscode.workspace.getConfiguration("goDoc").get("autoIndex", false)) return true;
  const config = projectConfig(root);
  return config && config.writeIndex === true;
}

function goDocEnabled(root) {
  if (!vscode.workspace.getConfiguration("goDoc").get("enabled", true)) return false;
  const config = projectConfig(root);
  return !config || config.enabled !== false;
}

function projectConfig(root) {
  try {
    const configPath = path.join(root, ".go-doc", "config.json");
    if (!fs.existsSync(configPath)) return null;
    const config = JSON.parse(fs.readFileSync(configPath, "utf8"));
    return config || null;
  } catch (err) {
    output.appendLine(`go-doc config read failed: ${err.message}`);
    return null;
  }
}

async function rebuildCurrentWorkspace() {
  const root = workspaceRoot();
  if (!root) return;
  await rebuildIndex(root, true);
}

async function rebuildIndex(root, notify) {
  const command = await ensureGoDoc(root, notify);
  if (!command) return;

  const outDir = path.join(root, ".go-doc");
  const outFile = path.join(outDir, "index.json");
  fs.mkdirSync(outDir, { recursive: true });

  const result = await execFile(command, ["index", "-o", outFile, "."], root);
  if (result.err) {
    const message = result.stderr || result.stdout || result.err.message;
    output.appendLine(message);
    vscode.window.showWarningMessage(`go-doc index failed: ${message.slice(0, 160)}`);
    return;
  }

  if (!fs.existsSync(outFile)) {
    const message = (result.stderr || result.stdout || "no @model annotations found; index not written").trim();
    if (message) output.appendLine(message);
    if (notify) vscode.window.showInformationMessage("go-doc index not needed: no @model annotations found");
  } else if (notify) {
    vscode.window.showInformationMessage("go-doc index rebuilt");
  }

  if (client) {
    await client.stop();
    client = null;
  }
  await startClient(extensionContext || { subscriptions: [] });
}

async function ensureGoDoc(root, notify) {
  if (goDocCommand) return goDocCommand;

  const resolved = await resolveGoDoc(root);
  if (resolved) {
    goDocCommand = resolved;
    output.appendLine(`go-doc probe succeeded: ${resolved}`);
    return resolved;
  }

  output.appendLine("go-doc probe failed: command not found");
  const installed = await offerInstall(root, notify);
  if (!installed) return null;

  goDocCommand = await resolveGoDoc(root);
  return goDocCommand;
}

async function offerInstall(root, notify) {
  if (installPromptOpen) return false;
  installPromptOpen = true;
  try {
    const answer = await vscode.window.showWarningMessage(
      "go-doc is not available on PATH. Install it now with `go install github.com/donseba/go-doc@latest`?",
      { modal: true },
      "Install",
    );
    if (answer !== "Install") return false;

    output.appendLine("Installing go-doc CLI: go install github.com/donseba/go-doc@latest");
    const result = await execFile("go", ["install", "github.com/donseba/go-doc@latest"], root);
    if (result.stdout) output.appendLine(result.stdout);
    if (result.stderr) output.appendLine(result.stderr);
    if (result.err) {
      const message = result.err.code === "ENOENT"
        ? "Go is not available on PATH. Install Go or add it to PATH before installing go-doc."
        : result.stderr || result.stdout || result.err.message;
      vscode.window.showErrorMessage(`go-doc install failed: ${message.slice(0, 180)}`);
      return false;
    }
    if (notify) vscode.window.showInformationMessage("go-doc CLI installed");
    return true;
  } finally {
    installPromptOpen = false;
  }
}

async function resolveGoDoc(root) {
  const envCandidates = await goDocCandidatesFromGoEnv(root);
  const pathCandidate = await firstGoDocPath(root);
  const candidates = [...envCandidates, pathCandidate, "go-doc"];

  for (const candidate of unique(candidates.filter(Boolean))) {
    const probe = await execFile(candidate, ["--help"], root);
    if (!probe.err || probe.err.code !== "ENOENT") return candidate;
  }

  return null;
}

async function goDocCandidatesFromGoEnv(root) {
  const result = await execFile("go", ["env", "GOBIN", "GOPATH"], root);
  if (result.err) {
    return [defaultGoDocBin()];
  }

  const lines = result.stdout.split(/\r?\n/).map((line) => line.trim()).filter(Boolean);
  const gobin = lines[0] || "";
  const gopath = lines[1] || "";
  const exe = process.platform === "win32" ? "go-doc.exe" : "go-doc";
  const paths = [];
  if (gobin) paths.push(path.join(gobin, exe));
  if (gopath) paths.push(path.join(gopath.split(path.delimiter)[0], "bin", exe));
  paths.push(defaultGoDocBin());
  return paths;
}

function defaultGoDocBin() {
  const exe = process.platform === "win32" ? "go-doc.exe" : "go-doc";
  return path.join(os.homedir(), "go", "bin", exe);
}

function prepareLspCommand(command) {
  if (process.platform !== "win32" || !path.isAbsolute(command) || !fs.existsSync(command)) {
    return command;
  }

  try {
    const dir = extensionContext?.globalStorageUri?.fsPath || path.join(os.tmpdir(), "go-doc-vscode");
    fs.mkdirSync(dir, { recursive: true });
    cleanupOldLspCopies(dir);
    const copy = path.join(dir, `go-doc-lsp-${process.pid}-${Date.now()}.exe`);
    fs.copyFileSync(command, copy);
    output.appendLine(`Copied go-doc LSP binary to ${copy}`);
    return copy;
  } catch (err) {
    output.appendLine(`go-doc LSP binary copy failed, using installed binary: ${err.message}`);
    return command;
  }
}

function cleanupOldLspCopies(dir) {
  try {
    for (const entry of fs.readdirSync(dir)) {
      if (/^go-doc-lsp-\d+-\d+\.exe$/.test(entry)) {
        fs.rmSync(path.join(dir, entry), { force: true });
      }
    }
  } catch (err) {
    output.appendLine(`go-doc LSP copy cleanup skipped: ${err.message}`);
  }
}

function unique(values) {
  return [...new Set(values)];
}

function execFile(command, args, cwd) {
  return new Promise((resolve) => {
    cp.execFile(command, args, { cwd }, (err, stdout, stderr) => {
      resolve({ err, stdout, stderr });
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
  return parts.some((part) => [".git", ".idea", ".go-doc", "build", "out", "vendor", "node_modules"].includes(part));
}

async function showIndexStatus() {
  const root = workspaceRoot();
  const editor = vscode.window.activeTextEditor;
  const filePath = editor && editor.document.uri.fsPath;
  const languageId = editor && editor.document.languageId;
  const indexFile = root ? path.join(root, ".go-doc", "index.json") : null;
  const exists = indexFile && fs.existsSync(indexFile);
  const goDocPath = root ? (goDocCommand || await resolveGoDoc(root) || "-") : "-";
  const installedVersion = root && goDocPath !== "-" ? await commandVersion(goDocPath, root) : "-";
  let templates = 0;
  let types = 0;
  let error = "-";

  if (exists) {
    try {
      const index = JSON.parse(fs.readFileSync(indexFile, "utf8"));
      templates = Object.keys(index.templates || {}).length;
      types = Object.keys(index.types || {}).length;
    } catch (err) {
      error = `${err.name}: ${err.message}`;
    }
  }

  vscode.window.showInformationMessage(
    [
      `Optional index: ${exists ? indexFile : "no optional .go-doc/index.json file"}`,
      `Index root: ${root || "-"}`,
      `Project: ${vscode.workspace.workspaceFolders?.[0]?.uri.fsPath || "-"}`,
      `File: ${filePath || "-"}`,
      `Language: ${languageId || "-"}`,
      `Client: ${client ? stateName(client.state) : "not created"}`,
      `go-doc: ${goDocPath}`,
      `Installed version: ${installedVersion}`,
      `LSP executable: ${activeLspCommand || "-"}`,
      `LSP version: ${activeLspVersion}`,
      `Templates: ${templates}`,
      `Types: ${types}`,
      `Error: ${error}`,
    ].join("\n"),
    { modal: true },
  );
}

async function toggleAutoIndex() {
  const config = vscode.workspace.getConfiguration("goDoc");
  const next = !config.get("autoIndex", false);
  await config.update("autoIndex", next, vscode.ConfigurationTarget.Workspace);
  vscode.window.showInformationMessage(`go-doc auto index ${next ? "enabled" : "disabled"}`);
}

async function toggleEnabled() {
  const config = vscode.workspace.getConfiguration("goDoc");
  const next = !config.get("enabled", true);
  await config.update("enabled", next, vscode.ConfigurationTarget.Workspace);
  vscode.window.showInformationMessage(`go-doc ${next ? "enabled" : "disabled"} for this workspace`);
  if (next) {
    await startClient(extensionContext || { subscriptions: [] });
    return;
  }
  await stopClient();
}

async function restartLsp() {
  output.show(true);
  await startClient(extensionContext || { subscriptions: [] });
  vscode.window.showInformationMessage("go-doc LSP restarted");
}

function stateName(state) {
  const State = languageClientModule && languageClientModule.State;
  if (!State) return String(state);
  switch (state) {
    case State.Stopped:
      return "stopped";
    case State.Starting:
      return "starting";
    case State.Running:
      return "running";
    default:
      return String(state);
  }
}

async function findGoDocPath(root) {
  const command = process.platform === "win32" ? "where" : "which";
  const result = await execFile(command, ["go-doc"], root);
  return (result.stdout || result.stderr || result.err?.message || "-").trim();
}

async function firstGoDocPath(root) {
  const output = await findGoDocPath(root);
  const first = output.split(/\r?\n/).map((line) => line.trim()).find(Boolean);
  return first && first !== "-" ? first : null;
}

async function commandVersion(command, root) {
  const result = await execFile(command, ["version"], root);
  if (result.err) return "-";
  return (result.stdout || result.stderr || "-").trim() || "-";
}

module.exports = { activate, deactivate };
