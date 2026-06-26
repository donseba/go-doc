# go-doc for Sublime Text

Sublime Text package for typed Go templates powered by `go-doc lsp`.

The package is intentionally small: syntax highlighting lives here, while
completion, diagnostics, hover, navigation, document symbols, and semantic
tokens come from the language server.

## Requirements

Install Sublime's `LSP` package through Package Control.

Install `go-doc` on your PATH:

```bash
go install github.com/donseba/go-doc@latest
```

## Install

Build the package from the repository root:

```bash
task build:sublime
```

Then install `dist/go-doc-sublime.sublime-package` into Sublime Text's
`Installed Packages` folder.

On Windows this is usually:

```text
%APPDATA%\Sublime Text\Installed Packages
```

Restart Sublime Text, open a `.gohtml` or `.tmpl` file in a Go module, and check
the LSP status. The language server command is:

```json
["go-doc", "lsp"]
```

Disable go-doc for one project with `.go-doc/config.json`:

```json
{
  "enabled": false
}
```

## Template Contracts

The package includes a small `Go Template HTML` syntax for `.gohtml` and `.tmpl`
files. The LSP selector attaches to that syntax using `text.html.go-template`.
Template contracts use `@model`:

```gotemplate
{{/*
@model Page github.com/example/app.Page
*/}}
{{ Page.Title }}
```

## LSP Features

The same `go-doc lsp` server powers completion, diagnostics, hover,
go-to-definition, semantic tokens, and document symbols. It understands
`@model`, `@dot`, `@func`, range/with dot context, typed function returns,
template includes, named defines, and block calls.
