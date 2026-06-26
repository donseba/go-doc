<p align="center">
    <img src="./assets/go-doc-logo.png" alt="go-doc" width="420">
</p>

`go-doc` brings typed editor tooling to Go templates.

It reads lightweight `@model`, `@dot`, and `@func` annotations in `.gohtml`,
`.tmpl`, and `.html` templates, scans exported Go structs and functions in your
module, and serves that knowledge to editors through a small Language Server
Protocol server.

```gotemplate
{{/*
@model Page github.com/example/app.Page
*/}}

<h1>{{ Page.Title }}</h1>

{{ range Page.Items }}
    <a href="/items/{{ .ID }}">{{ .Label }}</a>
{{ end }}
```

With that contract in place, editors can complete fields, validate unknown
members, understand `range` dot context, show hover information, and navigate
back to Go source.

This is a two-way contract, not magic. The annotation is the template-side
entrance: it tells go-doc and the editor that `Page` should be an
`github.com/example/app.Page`. Your application still provides the runtime
exit: it must register a `Page` accessor before parsing, for example through
the optional `renderer`, or through equivalent application glue.

Plain `html/template` dot execution is different. If you call
`tmpl.Execute(w, page)`, the template receives `page` as `.`, so the matching
contract is `@dot`, not `@model`.

## Why

Go templates are intentionally simple at runtime, but that usually means the
editor has no idea what `{{ .Title }}` or `{{ Page.Items }}` refers to.

`go-doc` keeps runtime behavior unchanged. Your application still owns template
parsing, execution, routing, rendering, and data. `go-doc` only adds a typed
contract that editors and tools can understand.

## Features

- Typeahead for declared template models, built-in and custom template
  functions, exported fields, and exported methods.
- Dot-context completion inside `range` and `with` blocks.
- Diagnostics for unknown model names, fields, invalid `range` sources, and bad
  function calls.
- Include checks for `template`, `block`, same-file `define`, and cross-file
  child templates that declare `@dot`.
- Quick fixes for creating missing model structs and adding missing struct
  fields from template diagnostics.
- Hover and go-to-definition for model types, fields, methods, functions, and
  template includes.
- Semantic highlighting for model types, model names, built-in functions,
  custom functions, fields, and methods.
- A shared LSP core used by GoLand, VS Code, Sublime Text, Vim, and Neovim.
- Optional `.go-doc/index.json` generation for CI, debugging, and tool
  interoperability.

## Install

Install the CLI:

```bash
go install github.com/donseba/go-doc@latest
```

Install the experimental helper generator when using `@gen`:

```bash
go install github.com/donseba/go-doc/cmd/godoc-exp-gen@latest
```

Then install the editor package you use from the release assets.

On Windows, the GoLand and VS Code integrations run the long-lived language
server from a temporary copy of `go-doc.exe`. That keeps `go install
github.com/donseba/go-doc@latest` able to replace the installed binary while the
editor is open. Restart the LSP/editor to pick up the newly installed version.
The editor status commands show both the installed CLI version and the active
LSP copy version.

| Editor | Package |
| --- | --- |
| GoLand | `go-doc-goland-plugin-*.zip` |
| VS Code | `go-doc-vscode-*.vsix` |
| Sublime Text | `go-doc-sublime-*.sublime-package` |
| Vim | `go-doc-vim-*.zip` |
| Neovim | `go-doc-neovim-*.zip` |

## Quick Start

Use the model in a template:

```gotemplate
{{/*
@model Todo github.com/example/app.Todo
*/}}

<article>
    <h2>{{ Todo.Title }}</h2>
    <p>{{ Todo.Priority }}</p>
</article>
```

The declared model name is the template accessor. `@model Todo ...` is used as
`{{ Todo.Title }}`. At runtime, that accessor must exist. The optional renderer
creates it by matching the declared type to a Go value and adding a template
function named `Todo` before parsing. Without that runtime registration,
`html/template` has no built-in idea what `Todo` means.

Capitalized model names are recommended because they read like Go types and
avoid common template helper names, but lowercase names are not forbidden.
go-doc reports a diagnostic when a model name collides with a built-in,
configured, or local template function, because the same identifier cannot
reliably be both a model accessor and a function in `html/template`.

Use `@dot` when the template is rendered with dot set to one value, such as a
table row or card:

```gotemplate
{{/* @dot github.com/example/app.User */}}
<tr>
    <td>{{ .Name }}</td>
    <td>{{ .Status }}</td>
</tr>
```

Use `@dot` for normal `tmpl.Execute(w, value)` templates where the value is
available as `.`:

```gotemplate
{{/* @dot github.com/example/app.Page */}}
<h1>{{ .Title }}</h1>
```

When a parent calls that child with `{{ template "user_row.gohtml" . }}`,
go-doc checks that the passed value matches the child `@dot` contract. The same
check applies to named `define` sections and `block` calls.

Use `@func` for custom helpers that are local to one template:

```gotemplate
{{/*
@func userByID github.com/example/app.UserByID
*/}}

{{ (userByID 2).Name }}
```

For helpers available everywhere, prefer `.go-doc/config.json` so you do not
repeat the same `@func` declarations across templates.

## Configuration

No `.go-doc` folder is required. By default, `go-doc` finds the nearest
`go.mod`, scans that module in memory, skips `vendor`, and does not write
`.go-doc/index.json`.

The default configuration is:

```json
{
  "enabled": true,
  "include": ["/"],
  "exclude": ["vendor"],
  "functions": {},
  "writeIndex": false
}
```

Add `.go-doc/config.json` only when a project needs to change those defaults:

```json
{
  "enabled": true,
  "include": ["/"],
  "exclude": ["vendor", "tmp", "internal/generated"],
  "writeIndex": false,
  "functions": {
    "asset": "github.com/example/app.Asset",
    "formatDate": "github.com/example/app.FormatDate"
  }
}
```

Entries are module-relative paths. `/` means the module root. Excludes win over
includes. `enabled: false` disables go-doc for the project while leaving the
editor plugin installed. `functions` describes helpers that are available in every template so
the language server can complete and validate them without repeating `@func` in
each file.

`writeIndex` controls editor auto-indexing. Keep it `false` unless you want editor
adapters to maintain `.go-doc/index.json` after file changes. Even when it is
false, the language server still builds an in-memory index and all editor
features continue to work.

## Optional Index File

The generated index is an optional artifact, not the project root marker.
`go-doc` uses `go.mod` to find the module root.

Create the file explicitly when you want a concrete artifact for CI, debugging,
or other tools:

```bash
go-doc index -o .go-doc/index.json .
```

`go-doc index` only writes an index when at least one template declares a typed
contract, or when `.go-doc/config.json` declares global template functions. If
no typed template surface exists, the command exits successfully without
creating `.go-doc/index.json`.

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

The server builds an in-memory index from the module root by default. It reads
`.go-doc/index.json` only when `.go-doc/config.json` opts into `"writeIndex": true`.
Completion, diagnostics, hover, go-to-definition, semantic tokens, and include
checks still work without a `.go-doc` folder.

## Experimental Generation

`@gen` explores generated helper namespaces for templates:

```gotemplate
{{/*
@gen time github.com/example/app/internal/timefuncs
@gen money github.com/example/app/internal/moneyfuncs
*/}}

{{ time.Format Page.GeneratedAt "15:04:05" }}
{{ money.EUR .MonthlyCents }}
```

This is not a runtime import. The generator writes ordinary Go code that exposes
a normal `template.FuncMap`, and the editor understands the generated namespace
through the same LSP index.

Keep the source helpers in ordinary application packages and generate one small
bridge package:

```text
internal/timefuncs/timefuncs.go
internal/moneyfuncs/moneyfuncs.go

gen/gen.go
```

```bash
godoc-exp-gen \
  -package gen \
  -out gen/gen.go
```

Register the generated helpers before parsing:

```go
tmpl := template.New("page.gohtml").Funcs(gen.FuncMap())
```

That FuncMap only provides helper namespaces. Named models such as `Page` still
need the renderer or equivalent application glue described below.

See [README_gen.md](./README_gen.md) and [examples/exp-gen](./examples/exp-gen)
for the full explanation.

## Runtime Integration

`go-doc` does not require a framework. It does not execute your templates or
change how your application renders HTML.

For projects that want the annotated model names available during ordinary
`html/template` parsing, the repository includes a small `renderer` package. It
can scan the same template declarations, match them to the Go values you pass,
and register the declared model accessors before parsing:

Think of `@model` as a tunnel. The template declares the entrance:

```gotemplate
{{/* @model Page github.com/example/app.Page */}}
<h1>{{ Page.Title }}</h1>
```

The renderer creates the exit by registering a template function named `Page`
that returns the matching Go value:

Create one renderer for a template set. Use `renderer.Development` while
editing templates, or `renderer.Production` when you want contracts scanned once
at startup:

```go
views, err := renderer.New(renderer.Config{
    Mode:  renderer.Development,
    Files: []string{"templates/page.gohtml"},
    Funcs: template.FuncMap{
        "asset":      app.Asset,
        "formatDate": app.FormatDate,
    },
})
```

The render path stays the same in both modes:

```go
tmpl := template.New("page.gohtml")
err := views.Register(tmpl, page)
_, err = tmpl.ParseFiles("templates/page.gohtml")
```

If `views.Register` is omitted, `Page.Title` is not valid plain
`html/template` syntax. Use `@dot` and `.Title` instead when executing directly
with `tmpl.Execute(w, page)`.

`Config.Funcs` is the runtime counterpart to `.go-doc/config.json` functions:
the config teaches editors about globally available helpers, while the renderer
registers the real Go functions with `html/template`.

Development mode re-reads `@model` declarations on each `Register` call, so
template-side renames are picked up without restarting. Production mode reads
the declarations once in `renderer.New`, avoiding repeated file reads and
keeping the render path predictable.

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
go install github.com/donseba/go-doc/cmd/godoc-exp-gen@latest
```
