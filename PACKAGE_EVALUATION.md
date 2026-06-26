# Package Evaluation

This document is a neutral assessment of `go-doc` as it exists today. It is
not a roadmap, pitch, or migration guide.

## Summary

`go-doc` is developer tooling for ordinary Go templates. It adds typed editor
support to `.gohtml`, `.tmpl`, and `.html` files through lightweight template
contracts such as `@model`, `@dot`, and `@func`.

The package is strongest when a project already wants to keep using
`html/template`, but wants a better authoring experience: completion,
diagnostics, hover, go-to-definition, include checks, and typed helper
functions.

It is not a replacement for `html/template`, `templ`, or a component framework.
It is closer to a language server and contract layer for existing Go template
projects.

## Rating

Current objective rating: **7.5 / 10**

Potential rating if hardened for v1: **8.5 / 10**

| Area | Rating | Notes |
| --- | ---: | --- |
| Problem fit | 9 | The gap is real: Go templates are runtime-safe but editor-poor. |
| Runtime design | 8 | Mostly non-invasive. Existing rendering can stay in place. |
| Editor value | 8 | Completion, diagnostics, hover, and navigation are high-value. |
| Reliability maturity | 7 | The core is promising, but editor parity and edge cases need continued hardening. |
| Simplicity | 7 | Annotations are small, but users must learn the contract model. |
| Ecosystem clarity | 7 | It complements some tools and competes with others; docs must stay explicit. |
| Long-term maintainability | 7 | LSP plus multiple editor adapters is powerful but maintenance-heavy. |

The rating is intentionally conservative. The concept is strong, but v1 quality
depends on boring reliability across editors, template includes, function calls,
and refresh behavior.

## What It Competes With

### Plain `html/template`

The Go standard library's [`html/template`](https://pkg.go.dev/html/template)
is the baseline. It provides HTML-safe template execution and the same core
interface as `text/template`.

`go-doc` does not replace it. Instead, it makes `html/template` easier to edit
by giving the editor type information that the template engine itself does not
carry.

Competes on:

- authoring experience
- diagnostics before runtime
- navigation from templates to Go source

Does not compete on:

- escaping
- execution
- parsing semantics
- production rendering

### `templ`

[`templ`](https://templ.guide/) generates Go code from `.templ` files and gives
stronger compile-time guarantees. Its docs describe it as a way to build HTML
with Go components.

`templ` is a stronger choice when a project wants to adopt a component syntax
and generated Go code as the primary view layer.

`go-doc` is a stronger fit when a project already has many Go templates, wants
to keep them, or prefers the standard `html/template` runtime.

Competes on:

- typed HTML authoring
- component-like confidence
- editor support

Differs because:

- `templ` changes the template language and adds generation to the main view
  path.
- `go-doc` keeps normal Go templates and adds tooling around them.

### Gomponents

[`gomponents`](https://pkg.go.dev/maragu.dev/gomponents) builds HTML from Go
functions and types. It is a pure-Go component approach.

Gomponents is better when a team wants all view code in Go and prefers Go
syntax over template syntax.

`go-doc` is better when designers, backend developers, or existing systems
benefit from keeping HTML-looking templates.

Competes on:

- type-aware UI construction
- reusable components
- avoiding loosely typed template mistakes

Differs because:

- Gomponents moves markup into Go.
- `go-doc` keeps markup in templates.

### Framework-specific template helpers

Many projects solve this locally with framework conventions, template loading
wrappers, custom FuncMaps, live reloaders, or app-specific partial systems.

`go-doc` can coexist with those, especially when they still use
`html/template`. Its value is editor intelligence, not a rendering framework.

### IDE-native template support

Editors already provide some HTML and Go-template awareness. `go-doc` competes
with the default editor experience by adding project-specific type knowledge.

The package helps most when native editor support cannot infer the type behind
`Page.Users`, `.Name`, `{{ template "row" . }}`, or custom functions.

## Who It Helps

### Existing `html/template` applications

The best target user is a Go project that already has real `.gohtml` templates
and wants better safety without rewriting the view layer.

Typical pain points:

- unknown fields only fail at runtime
- row templates lose dot type information
- custom functions are invisible to the editor
- include calls can pass the wrong shape
- refactors across Go structs and templates are risky

### Server-rendered Go applications

`go-doc` fits server-rendered applications where templates are still a normal
part of the architecture. This includes small web apps, internal tools,
documentation sites, admin panels, and htmx-oriented applications.

### Teams that want gradual adoption

The contract annotations can be introduced one template at a time. A project
does not need to convert its rendering stack.

### Developers who like standard library primitives

If a team prefers `net/http`, `html/template`, and explicit Go code over a
larger framework, `go-doc` aligns well with that philosophy.

## Why It Helps

`html/template` is intentionally dynamic. At runtime, this is flexible. In the
editor, it often means the template has no useful type information.

`go-doc` helps by making the implicit contract explicit:

```gotemplate
{{/*
@model Page github.com/example/app.Page
*/}}

<h1>{{ Page.Title }}</h1>
```

This is a two-way contract. The annotation tells the editor what `Page` is; the
application must still register `Page` at runtime, either with go-doc's optional
renderer or equivalent template glue. For direct `tmpl.Execute(w, page)`, the
matching contract is `@dot`, and the template uses `{{ .Title }}`.

Once the editor knows `Page` is `app.Page`, it can provide:

- field and method completion
- hover documentation
- go-to-definition
- invalid field diagnostics
- `range` and `with` dot tracking
- function argument validation
- include and child template checks

The key advantage is that this happens without changing the runtime template
engine.

## Main Advantages

### Keeps normal Go templates

Projects can keep `html/template`, existing template files, and existing render
paths.

### Low migration cost

Adding `@model` or `@dot` is much smaller than rewriting a view layer.

### Strong value for row and partial templates

`@dot` is especially valuable. It lets child templates declare the type of `.`,
which is a common blind spot in Go templates.

### Function return awareness

`@func` and configured global functions allow helper calls to be typed. If a
function returns a struct, go-doc can continue completion through `with` blocks
or parenthesized calls.

### Include awareness

Checking `{{ template "user_row.gohtml" . }}` against the child template's
`@dot` contract is a high-value feature. It catches a class of mistakes that
plain template parsing cannot catch early.

### Framework-neutral

The package can work with many server setups because it is centered on
templates and Go source, not a web framework.

### Optional renderer

The `renderer` package can bridge template declarations and runtime values for
projects that want declared model names available during parsing. It remains
optional.

It is also the clearest way to explain `@model`: the annotation is the
template-side entrance, and `renderer.Register` creates the runtime exit by
adding the named template function.

## Caveats

### It adds a contract language

The annotations are small, but they are still another layer to learn. Teams
must understand that `@model`, `@dot`, and `@func` are tooling contracts, not
native `html/template` syntax.

### It is not compile-time enforcement by default

Unless CI runs `go-doc` checks or generated artifacts are validated, many
benefits are editor-time benefits. That is still useful, but it is not the same
as generated Go code failing to compile.

### Editor adapters increase maintenance cost

Supporting GoLand, VS Code, Sublime Text, Vim, and Neovim is valuable, but it
creates real maintenance work. LSP behavior should stay central, and
editor-specific logic should remain thin.

### Template syntax edge cases are deep

Go templates support pipelines, variables, nested calls, `with`, `range`,
`define`, `template`, `block`, methods, functions, and associated templates.
Getting all of this correct is possible, but it is not trivial.

### Runtime and editor contracts can drift

If an app registers a FuncMap at runtime but does not describe it to go-doc,
the editor may warn incorrectly. If the editor config describes a function that
runtime does not register, templates may still fail at runtime.

The same applies to `@model`: declaring `@model Page ...` helps the editor, but
runtime code must still provide a `Page` accessor before parsing.

### `@gen` is experimental

Generated helper namespaces are promising, but they affect runtime wiring. They
should remain clearly marked as experimental until their ergonomics and failure
modes are proven.

### It does not solve HTML structure validation

`go-doc` is about Go template data contracts. It is not primarily an HTML
validator, accessibility checker, CSS analyzer, or browser testing tool.

## Who Should Avoid It

### Projects already committed to `templ`

If a team is happy with `templ` and wants compile-time generated components,
`go-doc` may be unnecessary.

### Projects that want all markup in Go

Gomponents or similar pure-Go component approaches may be a better fit.

### Very small projects with minimal templates

For a few simple templates, annotations and editor packages may be more process
than value.

### Teams unwilling to install editor tooling

Most value comes from the LSP and editor integrations. Without them, go-doc is
less compelling unless used in CI or as a renderer helper.

### Projects that require hard compile-time guarantees

`go-doc` improves feedback but does not inherently make every template error a
Go compiler error. Teams that require that guarantee may prefer code-generation
view systems.

## Best-Fit Use Cases

- Existing Go applications with many `.gohtml` files.
- Server-rendered apps using `html/template`.
- htmx-style apps that render many partial templates.
- Admin panels and internal tools with reusable row/card templates.
- Teams doing frequent Go model refactors that affect templates.
- Projects that want editor help without adopting a new template language.

## Weak-Fit Use Cases

- Client-heavy SPAs where Go templates are minimal.
- Projects using React/Vue/Svelte/etc. for all UI.
- Projects that already use `templ` pervasively.
- Projects that treat templates as throwaway strings.
- Teams that do not want custom annotations in template comments.

## Strategic Position

The strongest positioning is:

> Typed editor intelligence for normal Go templates.

That is sharper than:

- a new template engine
- a rendering framework
- a component system
- a replacement for `html/template`

The identity should stay narrow. The more go-doc behaves like "normal Go
templates, typed enough to enjoy," the easier it is to explain and trust.

## Recommended Priorities Before v1

1. Keep `@model`, `@dot`, `@func`, includes, and refresh behavior extremely
   reliable.
2. Preserve one shared LSP brain across editors.
3. Make diagnostics boringly predictable and easy to clear after edits.
4. Keep `.go-doc/config.json` optional and minimal.
5. Keep `@gen` clearly experimental until its API stabilizes.
6. Document runtime/editor drift clearly, especially global functions.
7. Add CI-friendly validation commands if not already present.

## Objective Conclusion

`go-doc` solves a real problem for a specific audience: Go developers who like
the standard template runtime but dislike the weak editor experience around it.

It should not try to beat `templ` at compile-time components or gomponents at
pure-Go HTML construction. Its strongest lane is making existing and future
`html/template` codebases feel typed, navigable, and refactorable without
changing how the application renders.

That is a narrow but meaningful niche. If the LSP stays reliable and the
annotation model remains small, the package has a credible path to becoming a
high-value companion tool for server-rendered Go applications.
