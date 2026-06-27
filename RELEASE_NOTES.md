# go-doc v0.10.0

Release notes compared with the v0.8 line.

v0.10.0 is the release where go-doc becomes much more than a small annotation
scanner. The language server is now the shared brain for the editor
integrations, template contracts understand more of normal Go template flow, and
the project has a clearer runtime story for using typed template accessors
without pretending that `html/template` has magic imports.

## Highlights

### The LSP Is Now The Core

GoLand, VS Code, Sublime Text, Vim, and Neovim are now thin editor adapters
around the same `go-doc lsp` behavior.

That shared language server provides:

- completion for models, dot context, fields, methods, and functions
- hover and go-to-definition for Go structs, fields, methods, functions, and
  child templates
- semantic highlighting for contracts and template expressions
- diagnostics for unknown models, unknown fields, invalid ranges, bad function
  calls, wrong include data, and contract collisions
- in-memory indexing by default, so `.go-doc/index.json` is optional

### `@model` Is The Contract For Named Template Accessors

Templates can declare named model accessors:

```gotemplate
{{/*
@model Page github.com/example/app.Page
*/}}

<h1>{{ Page.Title }}</h1>
```

This is a two-way contract. The annotation tells go-doc and the editor what
`Page` should be. Runtime code still has to register a real `Page` accessor
before parsing, either through go-doc's renderer or equivalent application glue.

### `@dot` Is Now A First-Class Contract

For normal `tmpl.Execute(w, value)` templates, or for included row/card
templates, `@dot` describes the value available as `.`:

```gotemplate
{{/* @dot github.com/example/app.User */}}

<tr>
    <td>{{ .Name }}</td>
    <td>{{ .Email }}</td>
</tr>
```

This is one of the biggest quality-of-life additions. go-doc can now understand
large existing templates that already rely on dot-heavy Go template style.

### Template Includes Understand Their Data

go-doc now checks template calls such as:

```gotemplate
{{ range Page.Users }}
    {{ template "user_row.gohtml" . }}
{{ end }}
```

If `user_row.gohtml` declares `@dot User`, go-doc verifies that the value passed
to the child template is actually a `User`. This works for cross-file templates,
same-file `define` sections, and `block` usage.

### `@func` Adds Typed Template Helpers

Templates can declare helper functions:

```gotemplate
{{/*
@func add github.com/example/app.Add
*/}}

{{ add 10 2 }}
```

The language server understands argument count, argument types, return types,
hover, go-to-definition, and semantic highlighting for these functions.

Configured global functions are also supported through `.go-doc/config.json`,
so helpers that every template receives do not have to be repeated in every
file.

### Go Type Resolution Is Much Stronger

The scanner now uses Go package/type information instead of relying only on
string-shaped AST guesses.

That improves support for:

- imported types
- aliases
- exported fields and methods
- method calls such as `Page.GeneratedAt.Format "15:04:05"`
- function return types
- parenthesized function results such as `{{ (userByID 42).Name }}`
- nested dot context inside `range` and `with`

### Renderer Package

The new `renderer` package gives runtime code a matching path for template
contracts.

It can scan template annotations, match declared model types to Go values, and
register the correct template accessors before parsing:

```go
views := renderer.New(renderer.Config{
    Files: []string{"templates/page.gohtml"},
    Mode:  renderer.Development,
})

tmpl := template.New("page.gohtml")
err := views.Register(tmpl, page)
```

Development mode can rescan templates while editing. Production mode scans once
and keeps runtime behavior predictable.

### Experimental `@gen` Helper Namespaces

v0.10.0 includes an experimental generator under `exp/gen` and
`cmd/godoc-exp-gen`.

Templates can declare helper namespaces:

```gotemplate
{{/*
@gen time github.com/example/app/internal/timefuncs
@gen money github.com/example/app/internal/moneyfuncs
*/}}

{{ time.Format Page.GeneratedAt "15:04:05" }}
{{ money.EUR .MonthlyCents }}
```

The generator writes ordinary Go code that exposes a normal
`template.FuncMap`. This is not a runtime import system; the generated package
must still be imported and registered by the application.

The default command now scans the nearest Go module for `@gen` declarations:

```sh
godoc-exp-gen -package gen -out gen/gen.go
```

`-templates` is still available when a project wants to scan only specific
files.

### Documentation And Examples

This release adds more complete documentation and working examples:

- docs website example
- showcase website example
- todo HTTP example
- table/include example
- single-file `define`/`block` example
- experimental generated-helper example
- renderer documentation
- package evaluation and manifesto notes

## Editor Packages

Release assets are now versioned consistently:

- `go-doc-goland-plugin-0.10.0.zip`
- `go-doc-vscode-0.10.0.vsix`
- `go-doc-sublime-0.10.0.sublime-package`
- `go-doc-vim-0.10.0.zip`
- `go-doc-neovim-0.10.0.zip`

The GoLand and VS Code integrations run the long-lived LSP from a temporary
copy on Windows. This lets users update `go-doc.exe` while the editor is open,
then restart the LSP/editor to use the new binary.

## Hotfix Notes

### Auto-Index Timeout In Larger Template Projects

After testing v0.10.0 against `go-partial`, the auto-indexer could time out in
GoLand because the CLI hit a stack overflow while walking recursive or highly
connected Go type graphs.

The reachable-type scanner now marks named types as visited before descending
into their underlying fields and method signatures. This prevents infinite
recursion while still indexing the fields, methods, and related types needed for
template completion and diagnostics.

A regression test now covers recursive local types so this path stays stable.
With the fix in place, `go-doc index` completes quickly for `go-partial` again.

### CLI Version Now Comes From The Go Module Build

`go-doc -version` previously read a hardcoded source constant. That could drift
from the release tag, especially because CI can update release assets without
changing the source archive that `go install github.com/donseba/go-doc@version`
downloads.

The CLI now derives its version from Go build metadata. Installing
`github.com/donseba/go-doc@vX.Y.Z` reports `X.Y.Z`, while local development
builds report `dev`.

## Configuration Changes

`.go-doc/config.json` is optional. Without it, go-doc is enabled and indexes in
memory.

Default shape:

```json
{
  "enabled": true,
  "include": ["/"],
  "exclude": ["vendor"],
  "functions": {},
  "writeIndex": false
}
```

`writeIndex` replaces the older experimental `index` naming. Keep it `false`
unless you specifically want editor adapters to maintain `.go-doc/index.json`
on disk.

## Breaking Or Notable Changes Since v0.8

- `@param` has been replaced by `@model`.
- The old underscore accessor style is no longer the preferred model.
  `@model Page ...` maps to `{{ Page.Title }}`.
- Use `@dot` for templates that receive data through normal dot execution.
- `.go-doc/index.json` is optional and no longer required for normal editor
  features.
- Project config uses `writeIndex`, not `index`.
- `@gen` is experimental and not part of the stable v1 contract yet.

## Known Caveats

- Sublime Text, Vim, and Neovim use the shared LSP path, but the most heavily
  tested adapters for this release are GoLand and VS Code.
- `@gen` deliberately lives under `exp/`; use it when you want to try the model,
  not when you need a locked v1 promise.
- Runtime templates still need real Go registration. go-doc improves the
  contract and editor experience, but it does not change how `html/template`
  executes.

## Verification

Before release:

```sh
go test -count=1 ./...
node --check ide/vscode/extension.js
task check:release-version -- v0.10.0
task dist
```
