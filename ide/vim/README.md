# go-doc for Vim

Vim adapter for typed Go templates powered by `go-doc lsp`.

Classic Vim does not include a built-in LSP client, so this package registers
`go-doc lsp` with the popular `vim-lsp` plugin.

## Requirements

- Vim 8.2 or newer
- `prabirshrestha/vim-lsp`
- `go-doc` on `PATH`

Install the CLI:

```bash
go install github.com/donseba/go-doc@latest
```

## Install

With `vim-plug`:

```vim
Plug 'prabirshrestha/vim-lsp'
Plug 'donseba/go-doc', { 'rtp': 'ide/vim' }
```

From a release ZIP, copy the contents of `go-doc-vim` into:

```text
~/.vim/pack/go-doc/start/go-doc
```

On Windows:

```text
%USERPROFILE%\vimfiles\pack\go-doc\start\go-doc
```

## Quick Start

The plugin auto-registers the server on `User lsp_setup`:

```text
go-doc lsp <nearest go.mod directory>
```

Disable automatic registration:

```vim
let g:go_doc_auto_start = 0
```

Then copy the server registration from `plugin/go_doc_lsp.vim` into your own
Vim config and customize it.

Disable go-doc for one project with `.go-doc/config.json`:

```json
{
  "enabled": false
}
```

## Template Contracts

Template contracts use `@model`:

```gotemplate
{{/*
@model Page github.com/example/app.Page
*/}}
{{ Page.Title }}
```

## LSP Features

The Vim package only registers the server. Completion, diagnostics, hover,
go-to-definition, and document symbols come from `go-doc lsp`. The server
understands `@model`, `@dot`, `@func`, range/with dot context, typed function
returns, template includes, named defines, and block calls.
