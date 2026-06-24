# go-doc for Neovim

Neovim adapter for typed Go templates powered by `go-doc lsp`.

The plugin uses Neovim's built-in LSP client. It does not implement completion,
diagnostics, hover, navigation, or semantic tokens itself; all editor
intelligence comes from `go-doc lsp`.

## Requirements

- Neovim 0.10 or newer
- `go-doc` on `PATH`

Install the CLI:

```bash
go install github.com/donseba/go-doc@latest
```

## Install

With `lazy.nvim`:

```lua
{
  "donseba/go-doc",
  dir = "ide/neovim",
  ft = { "gohtml", "gotmpl", "html" },
  config = function()
    require("go-doc").setup()
  end,
}
```

From a release ZIP, copy the contents of `go-doc-neovim` into a Neovim package
directory such as:

```text
~/.local/share/nvim/site/pack/go-doc/start/go-doc
```

On Windows:

```text
%LOCALAPPDATA%\nvim-data\site\pack\go-doc\start\go-doc
```

## Configure

Default setup:

```lua
require("go-doc").setup()
```

Custom command or filetypes:

```lua
require("go-doc").setup({
  cmd = { "go-doc", "lsp" },
  filetypes = { "gohtml", "gotmpl", "html" },
  autostart = true,
})
```

The plugin finds the nearest `go.mod` and starts the server with that directory
as the root:

```text
go-doc lsp /path/to/module
```

Disable automatic startup:

```lua
vim.g.go_doc_auto_start = false
```

Then call:

```lua
require("go-doc").start()
```

## Template Contracts

Template contracts use `@model`:

```gotemplate
{{/*
@model page github.com/example/app.Page
*/}}
{{ _page.Title }}
```
