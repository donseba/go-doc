# go-doc

<p align="center">
    <img src="./assets/go-doc-logo.png" alt="go-doc" width="420">
</p>

`go-doc` brings typed editor tooling to Go templates.

It reads lightweight `@model` annotations in `.gohtml`, `.tmpl`, and `.html`
templates, scans exported Go structs in your module, and serves that knowledge
to editors through a small Language Server Protocol server.

```gotemplate
{{/*
@model page github.com/example/app.Page
*/}}

<h1>{{ _page.Title }}</h1>

{{ range _page.Items }}
    <a href="/items/{{ .ID }}">{{ .Label }}</a>
{{ end }}
```

With that contract in place, editors can complete fields, validate unknown
members, understand `range` dot context, show hover information, and navigate
back to Go source.

## Why

Go templates are intentionally simple at runtime, but that usually means the
editor has no idea what `{{ .Title }}` or `{{ _page.Items }}` refers to.

`go-doc` keeps runtime behavior unchanged. Your application still owns template
parsing, execution, routing, rendering, and data. `go-doc` only adds a typed
contract that editors and tools can understand.

## Features

- Typeahead for declared template models, accessors, exported fields, and
  exported methods.
- Dot-context completion inside `range` and `with` blocks.
- Diagnostics for unknown models, accessors, fields, and invalid `range`
  sources.
- Hover and go-to-definition for model types, fields, and methods.
- Semantic highlighting for model types, accessors, fields, and methods.
- A shared LSP core used by GoLand, VS Code, Sublime Text, Vim, and Neovim.
- Optional `.go-doc/index.json` generation for editor refreshes and tool
  interoperability.

## Install

Install the CLI:

```bash
go install github.com/donseba/go-doc@latest
```

Then install the editor package you use from the release assets.

| Editor | Package |
| --- | --- |
| GoLand | `go-doc-goland-plugin-*.zip` |
| VS Code | `go-doc-vscode-*.vsix` |
| Sublime Text | `LSP-go-doc.sublime-package` |
| Vim | `go-doc-vim.zip` |
| Neovim | `go-doc-neovim.zip` |

GoLand and VS Code can also help rebuild the index from inside the editor.

## Quick Start

From a Go module root, generate an index:

```bash
go-doc index -o .go-doc/index.json .
```

`go-doc index` only writes an index when at least one template declares an
`@model` annotation. If no typed template contract exists, the command exits
successfully without creating `.go-doc/index.json`.

Use the model in a template:

```gotemplate
{{/*
@model todo github.com/example/app.Todo
*/}}

<article>
    <h2>{{ _todo.Title }}</h2>
    <p>{{ _todo.Priority }}</p>
</article>
```

The model name becomes an accessor by prefixing it with `_`, so `todo` becomes
`_todo`.

## Configuration

By default, `go-doc` scans the module and skips `vendor`.

You can limit the scan scope with `.go-doc/config.json`:

```json
{
  "include": ["/"],
  "exclude": ["vendor", "tmp", "internal/generated"]
}
```

Entries are module-relative paths. `/` means the module root. Excludes win over
includes.

## Commands

```bash
go-doc types [-query Todo] [root]
go-doc templates [root]
go-doc index [-o .go-doc/index.json] [root]
go-doc lsp [root]
```

## Language Server

`go-doc lsp` starts a Language Server Protocol server over stdio. It is the
shared implementation used by all editor packages.

The server reads `.go-doc/index.json` when present. If the index is missing, it
can build an in-memory index from the module root. Writing the index file is
still useful because editors can rebuild it after changes and the long-running
server can refresh its state.

## Runtime Integration

`go-doc` does not require a framework. It does not execute your templates or
change how your application renders HTML.

For projects that want the annotated accessors available during ordinary
`html/template` parsing, the repository includes a small `renderer` package. It
registers model accessors on a template so this works naturally:

```gotemplate
{{ _page.Title }}
```

The standalone example in `examples/standalone` shows this without depending on
`go-partial`.

The todo example in `examples/todo` shows a small multi-template setup with a
main shell, todo list, and todo detail template.

## Local Development

This repository includes a `Taskfile.yml` for common local commands.

Install Task first:

```bash
go install github.com/go-task/task/v3/cmd/task@latest
```

Useful tasks:

```bash
task doctor
task install:tools
task test
task build:goland
task build:vscode
task build:sublime
task build:vim
task build:neovim
task dist
```

`task install:tools` can install the local development toolchain for your
platform. On Windows it uses Scoop to install Go, Node.js, JDK 17, and Gradle.
On macOS it uses Homebrew.

## Release Assets

Build outputs are collected locally in `dist`.

Release archives contain editor packages only. The CLI is distributed through:

```bash
go install github.com/donseba/go-doc@latest
```
