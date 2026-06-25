# go-doc for VS Code

VS Code integration for typed Go templates powered by `go-doc lsp`.

## Features

- completions for `@model` Go type names
- completions for typed template model names such as `Page.`
- completions for dot context inside `range` blocks
- completions for exported fields and methods
- diagnostics for unknown model names and fields
- diagnostics for invalid range sources such as `range page.Title`
- hover and go to definition for contract types, fields, and methods
- semantic highlighting for model types, model names, fields, and methods
- debounced automatic index rebuilds

## Requirements

Install the `go-doc` CLI and make sure it is available on `PATH`:

```bash
go install github.com/donseba/go-doc@latest
```

If the CLI is missing when the extension rebuilds the index, VS Code asks before
running that install command for you.

## Quick Start

Generate the first index from a Go module root:

```bash
go-doc index -o .go-doc/index.json .
```

The language server can build an in-memory index when no generated file exists,
but writing `.go-doc/index.json` lets VS Code rebuild and refresh the shared
server state after edits.

Add a template contract:

```gotemplate
{{/*
@model Page github.com/example/app.Page
*/}}
{{ Page.Title }}
```

## Commands

- `go-doc: Rebuild Index`
- `go-doc: Show Index Status`
- `go-doc: Toggle Auto Index`
- `go-doc: Restart LSP`

## Settings

- `goDoc.autoIndex`
- `goDoc.debounceMilliseconds`
