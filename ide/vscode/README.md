# go-doc for VS Code

VS Code integration for typed Go templates powered by `go-doc lsp`.

## Features

- completions for typed-root Go type names
- completions for typed-root names, custom functions, and dot context
  inside `range` and `with` blocks
- completions for exported fields and methods
- diagnostics for unknown typed roots, fields, invalid range sources, bad
  function arguments, unsupported function returns, and wrong template include
  data
- hover and go to definition for contract types, fields, methods, functions, and
  child templates
- semantic highlighting for typed root types, typed root names, functions, fields, and
  methods
- optional debounced index rebuilds

## Requirements

Install the `go-doc` CLI and make sure it is available on `PATH`:

```bash
go install github.com/donseba/go-doc@latest
```

If the CLI is missing when the extension starts, VS Code asks before running
that install command for you.

On Windows, the extension starts the long-lived LSP from a temporary copy of
`go-doc.exe`. That means `go install github.com/donseba/go-doc@latest` can
replace the installed binary while VS Code is open. Restart the LSP to use the
newly installed version.
`go-doc: Show Index Status` shows both the installed CLI version and the active
LSP copy version.

## Quick Start

Add a template contract:

```gotemplate
{{/*
@model Page github.com/example/app.Page
*/}}
{{ Page.Title }}
```

`@model Page ...` is the editor-side entrance of the contract. Runtime code
must still register a real `Page` template accessor before parsing, usually
with go-doc's optional renderer. For plain `tmpl.Execute(w, page)` templates,
use `@dot` and `{{ .Title }}` instead.

No `.go-doc` folder is required. The language server finds `go.mod` and builds
an in-memory index for completion, diagnostics, hover, navigation, and semantic
highlighting.

Writing `.go-doc/index.json` is optional. Use it only when you want a generated
artifact for CI, debugging, or other tools.

## Commands

- `go-doc: Rebuild Index`
- `go-doc: Show Index Status`
- `go-doc: Toggle Auto Index`
- `go-doc: Restart LSP`

## Settings

- `goDoc.enabled`
- `goDoc.autoIndex`
- `goDoc.debounceMilliseconds`

`goDoc.enabled` is enabled by default. Disable it globally in VS Code settings,
or opt out per project with `.go-doc/config.json`:

```json
{
  "enabled": false
}
```

`goDoc.autoIndex` is disabled by default. You can also opt in per project with
`.go-doc/config.json`:

```json
{
  "writeIndex": true
}
```
