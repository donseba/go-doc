# go-doc exp/gen

`exp/gen` is an experiment for generated template helper namespaces.

The current annotation is `@gen`, not `@import`, on purpose. `@gen` makes it
clear that a generation step is required. `@import` sounds like normal Go import
semantics, which this is not.

A Go binary only has access to packages that were compiled into it. The safe
path is generated Go code that the application imports explicitly.

## Shape

Add an experimental generation declaration to a template:

```gohtml
{{/*
@gen view github.com/example/app/viewfuncs
*/}}
```

Generate a small wrapper package from those template declarations:

```sh
godoc-exp-gen \
  -package gen \
  -out gen/gen.go
```

When `-templates` is omitted, the command finds the nearest `go.mod` and scans
the module for `.gohtml`, `.tmpl`, and `.html` files with `@gen` declarations.
Use `-templates` only when you want to limit generation to specific files.

The generated package exposes a normal `template.FuncMap`:

```go
tmpl := template.New("page.gohtml").Funcs(gen.FuncMap())
```

Templates can then use a namespace-style helper:

```gohtml
{{ view.Money .MonthlyCents }}
{{ view.FormatTime Page.GeneratedAt "15:04:05" }}
```

This works because the generated `view` template function returns a namespace
value with exported methods.

`@gen` only creates helper namespace functions. It does not create model
accessors. If the same template also declares `@model Page ...`, that is a
separate two-way contract: the annotation tells go-doc what `Page` should be,
and runtime code must register a real `Page` template function before parsing.
For direct `tmpl.Execute(w, page)` templates, use `@dot` and normal dot access
instead.

For direct one-off generation without scanning templates, pass `-pkg` and
`-namespace`:

```sh
godoc-exp-gen -pkg time -namespace time -package gen -out gen/gen.go
```

## Why Not In The Main Path?

The main go-doc path should stay boring and reliable: `@model`, `@dot`, `@func`,
includes, diagnostics, and editor support. Generated helper namespaces are more
opinionated because they affect application runtime wiring, so they live here
until the ergonomics prove themselves.

## Current Limits

- Package-level exported functions are wrapped.
- Generic functions are skipped for now.
- Exported types are only used in function signatures; this does not generate
  constructors or expose package constants yet.
- The application still has to import and register the generated `FuncMap`.
- The language server understands `@gen` for completion, diagnostics, hover,
  highlighting, and go-to-definition, but the runtime generation shape is still
  experimental.

For the full design notes, see [`../../README_gen.md`](../../README_gen.md).
