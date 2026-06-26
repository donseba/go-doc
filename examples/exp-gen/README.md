# go-doc exp-gen example

This example shows the experimental `exp/gen` helper namespace idea with
multiple generated namespaces.

The template declares three generated helper namespaces:

```gohtml
{{/*
@model Page github.com/donseba/go-doc/examples/exp-gen.Page
@gen time github.com/donseba/go-doc/examples/exp-gen/internal/timefuncs
@gen money github.com/donseba/go-doc/examples/exp-gen/internal/moneyfuncs
@gen text github.com/donseba/go-doc/examples/exp-gen/internal/textfuncs
*/}}
```

The helper packages live under `internal/`. They are normal application code
that the generator can wrap:

```text
internal/timefuncs
internal/moneyfuncs
internal/textfuncs
```

The generated package lives under `gen/` and combines all three declarations
into one FuncMap:

```text
gen/gen.go
```

The app wires it into ordinary `html/template` through a normal FuncMap:

```go
tmpl := template.New("page.gohtml").Funcs(gen.FuncMap())
```

`gen.FuncMap()` only registers the generated helper namespaces such as `time`,
`money`, and `text`. It does not create the `Page` model accessor. The `@model`
line is still the entrance of a separate two-way contract: runtime code must
provide the matching `Page` accessor, usually through go-doc's renderer. Without
that runtime side, plain `html/template` cannot resolve `{{ Page.Title }}`.

Then templates can call namespace-style helpers:

```gohtml
{{ time.Format Page.GeneratedAt "15:04:05" }}
{{ money.EUR .MonthlyCents }}
{{ text.Initials .Owner }}
```

Regenerate the helper wrapper from the example root:

```sh
godoc-exp-gen \
  -package gen \
  -out gen/gen.go
```

Run the example:

```sh
go run .
```
