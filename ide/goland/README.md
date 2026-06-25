# go-doc for GoLand

GoLand integration for typed Go templates powered by `go-doc lsp`.

The plugin starts the shared language server for `.gohtml`, `.tmpl`, and `.html`
files, then adds a few GoLand-native conveniences for installation, indexing,
status, hover, and navigation.

## Requirements

Install `go-doc` on your PATH:

```bash
go install github.com/donseba/go-doc@latest
```

If `go-doc` is missing, the plugin can offer to install it with the same command.

## Quick Start

Install the plugin ZIP through:

```text
Settings > Plugins > Install Plugin from Disk...
```

Generate the first index from a Go module root:

```bash
go-doc index -o .go-doc/index.json .
```

Add a template contract:

```gotemplate
{{/*
@model Page github.com/example/app.Page
*/}}
{{ Page.Title }}
```

The language server also builds an in-memory index when no generated file exists.
Writing `.go-doc/index.json` is still useful because it lets the server refresh
after editor-triggered rebuilds and gives other tools a stable file to inspect.

## Editor Features

- completions for `@model` types, model names, fields, methods, and range dot
  contexts
- diagnostics for unknown model names, unknown fields, unknown model types, and
  invalid `range` sources
- hover and go-to-definition for model types, fields, and methods
- document symbols for declared models
- semantic highlighting for model types, model names, fields, and methods

## Actions

The plugin adds:

```text
Tools > Rebuild go-doc Index
Tools > Show go-doc Index Status
Tools > go-doc Auto Index
```

Auto-indexing watches `.go`, `.gohtml`, `.tmpl`, and `.html` files inside the
project and debounces rebuilds.

## Build

From the repository root:

```bash
task build:goland
```

The plugin ZIP is written below:

```text
ide/goland/build/distributions
```
