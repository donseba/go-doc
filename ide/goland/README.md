# go-doc GoLand plugin

This plugin reads a generated go-doc index and uses it to improve Go template
editing in GoLand.

Preferred index path:

```text
.go-doc/index.json
```

Compatibility fallback:

```text
.partial/index.json
```

Generate an index from a Go module root:

```bash
go-doc index -o .go-doc/index.json .
```

## Completions

In `.gohtml`, `.tmpl`, or `.html` files:

```gotemplate
@param todo 
```

Completes Go struct types from the index.

```gotemplate
{{ _todo. }}
```

Completes fields from the struct mapped by the current template contract.

Inside a range, the plugin follows the dot context:

```gotemplate
{{ range _page.Items }}
    {{ .Title }}
{{ end }}
```

If `_page.Items` is `[]Todo`, completion after `.` suggests fields from `Todo`.
Assigned range variables work too:

```gotemplate
{{ range $todo := _page.Items }}
    {{ $todo.Title }}
{{ end }}
```

For standalone local variables, add `@var` to the template contract:

```gotemplate
{{/*
@param page github.com/example/app.TodoPage
@var $todo github.com/example/app.Todo
*/}}
```

## Highlighting and diagnostics

The plugin annotates template contracts from the generated index:

```gotemplate
{{ _todo.Title }}
{{ _todo.UnknownField }}
{{ _missing.Title }}
```

Known accessors, fields, and `@param` types receive light semantic coloring.
Unknown accessors or fields are reported in the editor. Unknown fields include
a quick fix when the plugin can find a close field name.

Quick documentation shows the owner type, Go field type, and field comment when
the generated index contains source positions and docs. Go to declaration opens
the field in the Go source file.

Type names inside `@param` and `@var` contracts are navigable too:

```gotemplate
{{/*
@param page github.com/example/app.TodoPage
*/}}
```

Go to declaration on `TodoPage` opens the Go type declaration.

## Indexing model

The plugin should consume the `go-doc` CLI rather than duplicate Go parsing in
Kotlin.

When the project has a `go.mod` but no `.go-doc/index.json` yet, the plugin tries
to build the index on startup by running `go-doc` from `PATH`.

If `go-doc` is not available, the plugin asks before installing it with:

```bash
go install github.com/donseba/go-doc@latest
```

After startup, the plugin watches `.go`, `.gohtml`, `.tmpl`, and `.html` files
inside the project. Changes are debounced before rebuilding the index, so a burst
of edits should result in one CLI run rather than one run per keystroke.

## Rebuild action

The plugin adds this menu action:

```text
Tools > Rebuild go-doc Index
```

It runs `go-doc index -o .go-doc/index.json .` from the project root and writes:

```text
.go-doc/index.json
```

It also adds:

```text
Tools > Show go-doc Index Status
```

This opens a dialog with the index path, project root, current file, and whether
the current editor file matched a template contract.

Automatic rebuilding can be toggled per project:

```text
Tools > go-doc Auto Index
```

## Build

From this folder:

```bash
gradle buildPlugin
```

The plugin ZIP will be written below:

```text
build/distributions
```

Install it in GoLand through:

```text
Settings > Plugins > Install Plugin from Disk...
```

## Current limitations

- Auto-indexing is intentionally project-wide for now. There is no include/exclude
  UI beyond the built-in ignores for generated/build folders.
- It registers against GoLand's `GoTemplate` language and keeps `HTML`/`TEXT` as fallback surfaces.
