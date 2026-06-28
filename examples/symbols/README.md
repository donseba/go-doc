# Symbols Example

This example shows open typed-root annotations in go-doc.

Any non-special annotation with a name and type becomes a typed root. `@model`
is the recommended convention for page data, but `@component`, `@interaction`,
`@struct`, or another project word use the same completion, hover, diagnostics,
and navigation machinery. Callable helpers still use `@func`.

Custom annotations with explicit types work without config. This example also
uses project config to declare two known annotations, one with a default type and
one that still requires an explicit type:

```json
{
  "symbolAnnotations": [
    {
      "name": "interaction",
      "type": "github.com/donseba/go-doc/examples/symbols.Interaction"
    },
    {
      "name": "component"
    }
  ],
  "symbolStrictMode": false
}
```

That lets the template use framework or application vocabulary:

```gotemplate
{{/*
@model Page github.com/donseba/go-doc/examples/symbols.Page
@interaction LikesPoll
@component PrimaryButton github.com/donseba/go-doc/examples/symbols.Button
*/}}

{{ Page.Title }}
{{ LikesPoll.Endpoint }}
{{ PrimaryButton.Label }}
```

`@interaction LikesPoll` gets its type from config. `@component PrimaryButton`
declares its type inline because the config does not define a default component
type. With `symbolStrictMode` left false, an experimental annotation such as
`@jimmy PrimaryButton github.com/example.Button` would also be accepted when it
declares a type. Set `symbolStrictMode` to true when you want unknown annotation
names to be reported as typos. After parsing, all accepted custom annotations
are treated as typed roots.

This is still a two-way contract. The annotation only teaches go-doc and the
editor what `LikesPoll` and `PrimaryButton` mean. Runtime code still has to
register those names before parsing:

```go
tmpl := template.New("page.gohtml").Funcs(template.FuncMap{
    "LikesPoll": func() Interaction {
        return symbols.LikesPoll
    },
    "PrimaryButton": func() Button {
        return symbols.PrimaryButton
    },
})
```

Run it:

```bash
cd examples/symbols
go run .
```

Then open:

```text
http://localhost:8094
```

Open `templates/page.gohtml` in an editor with go-doc enabled. You should get
completion, hover, semantic highlighting, and go-to-definition for
`LikesPoll.Endpoint` and `PrimaryButton.Label`.
