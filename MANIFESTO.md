# go-doc Manifesto

HTML templates should not feel blind.

Go already gives us excellent types. Go already gives us a mature standard
template engine. Go already gives us fast builds, boring deployment, and
server-rendered pages that do not need a frontend build pipeline to say hello.

And yet, the moment we step into a `.gohtml` file, too often the editor shrugs.

`{{ .Title }}` might exist.

`{{ Page.User.Profile.Avatar.URL }}` might exist.

`{{ template "user_row.gohtml" . }}` might pass the right thing.

`{{ money .PriceCents }}` might accept that argument.

Maybe.

That is the strange part. Not that Go templates are simple. Their simplicity is
one of their strengths. The strange part is that we have accepted writing typed
Go on one side of the boundary and nearly untyped template expressions on the
other side.

go-doc exists because that boundary should be better.

## The Core Belief

Normal Go templates are good.

They are small. They are stable. They are in the standard library. They escape
HTML. They do not require a framework, bundler, compiler plugin, virtual DOM,
hydration story, or architectural oath.

But they deserve a modern editing experience.

That is the whole point.

go-doc does not say:

> Throw away `html/template`.

It says:

> Keep `html/template`. Make it understandable.

## The Missing Contract

Every template already has a contract.

The contract is just usually hidden in the controller, handler, renderer,
partial loader, or the developer's memory.

```go
tmpl.Execute(w, Page{
    Title: "Dashboard",
    Users: users,
})
```

Then inside the template:

```gotemplate
<h1>{{ .Title }}</h1>
{{ range .Users }}
    {{ template "user_row.gohtml" . }}
{{ end }}
```

The contract exists. The editor just cannot see it.

go-doc makes the raw `html/template` contract visible:

```gotemplate
{{/* @dot github.com/example/app.Page */}}
```

Now dot has a type and a source of truth.

If a project wants a named template accessor such as `Page.Title`, it can use
the optional renderer to register the declared model before parsing:

```gotemplate
{{/*
@model Page github.com/example/app.Page
*/}}

<h1>{{ Page.Title }}</h1>
```

Both forms are real. `@dot` describes normal `tmpl.Execute(w, page)` style
templates. `@model` describes named values registered by go-doc's renderer or
by equivalent application glue.

This is not magic. This is documentation that tools can read.

That is why the name matters: go-doc is not trying to be a framework. It is
structured documentation for Go templates. Like JSDoc, but for Go template
data. Like a map, but one the editor can drive with.

## Why This Should Have Been Standard

Because Go templates sit directly next to Go types.

Because the compiler already knows what `Page` is.

Because editors already understand Go structs, fields, methods, and functions.

Because the missing link is not philosophical. It is mechanical.

Tell the editor:

```gotemplate
@model Page github.com/example/app.Page
```

Then let it do what editors are already good at:

- complete `Page.Title`
- reject `Page.Titel`
- follow `Page.Users` to the Go struct
- understand `range Page.Users`
- know that `.` is now a `User`
- warn when a child template expects a `User` but receives `[]User`
- understand helper function arguments
- follow returned structs from function calls

Or, for plain dot execution:

```gotemplate
@dot github.com/example/app.Page
```

Then the editor can complete `.Title`, reject `.Titel`, and track dot through
`range .Users`.

This is not a new religion.

This is the missing type bridge.

## Templates Are Not Strings

The web has spent years relearning that templates are not throwaway strings.

They are application code.

They contain business data.

They call functions.

They include other templates.

They encode assumptions about structs, slices, methods, permissions, prices,
dates, and user-facing state.

If a Go refactor can break a template, the template should participate in the
refactor.

If a template references a struct field, the editor should know where that field
lives.

If a row template expects one `User`, passing an entire `[]User` should be
reported before the browser shows a broken page.

That is not luxury tooling.

That is table stakes.

## The Beauty of `@dot`

`@model` is useful.

`@func` is useful.

But `@dot` is where the idea becomes obvious.

Small templates are often rendered with dot set to one value:

```gotemplate
{{/* @dot github.com/example/app.User */}}
<tr>
    <td>{{ .Name }}</td>
    <td>{{ .Email }}</td>
    <td>{{ .Status }}</td>
</tr>
```

That one line changes the entire editing experience.

The editor no longer sees a mysterious `.`.

It sees a `User`.

That means completion works. Hover works. Go-to-definition works. Unknown field
diagnostics work. And when a parent calls:

```gotemplate
{{ template "user_row.gohtml" . }}
```

go-doc can check whether `.` is actually a `User`.

This is the kind of boring correctness that makes templates feel trustworthy.

## Functions Should Not Be Shadows

Template functions are powerful, but without type knowledge they become shadows.

```gotemplate
{{ money .PriceCents }}
{{ (userByID 42).Name }}
{{ Page.GeneratedAt.Format "15:04:05" }}
```

These calls have types.

`money` has parameters.

`userByID` has a return value.

`Format` is a method.

The editor should know that. go-doc makes it know that.

```gotemplate
{{/*
@func userByID github.com/example/app.UserByID
*/}}
```

And for global helpers:

```json
{
  "functions": {
    "money": "github.com/example/app.Money"
  }
}
```

The goal is simple: if Go can know it, the template editor should know it too.

## Includes Should Be Checked

Template inclusion is where many bugs hide.

```gotemplate
{{ template "user_row.gohtml" . }}
```

What does `user_row.gohtml` expect?

What is `.` right now?

Are those two things compatible?

Without a contract, the answer is vibes.

With go-doc:

```gotemplate
{{/* @dot github.com/example/app.User */}}
```

Now the include can be checked.

This is a big deal.

It means Go templates can start to behave like a connected system instead of a
bag of files whose relationships only become real at runtime.

## Normal Go, Still Normal Go

The best part is what go-doc does not do.

It does not replace your router.

It does not replace your handlers.

It does not replace `html/template`.

It does not require a component compiler.

It does not ask your server-rendered app to become something else.

Your app still owns rendering.

Your app still owns data.

Your app still owns FuncMaps.

Your app still decides how templates are parsed and executed.

go-doc simply gives your editor the missing context.

That restraint matters.

## The Honest Tradeoff

Yes, this adds annotations.

Yes, the editor tooling has to be installed.

Yes, the contract can drift if your runtime FuncMap and go-doc config disagree.

Yes, supporting multiple editors is work.

Yes, Go template syntax has sharp corners.

This is not free.

But the alternative is also not free.

The alternative is guessing.

The alternative is runtime-only failures.

The alternative is refactoring structs while hoping every template still lines
up.

The alternative is telling people to abandon standard templates entirely just to
get a decent typed editing experience.

go-doc is a bet that we do not have to do that.

## Why Not Just Use `templ`?

Sometimes you should.

`templ` is excellent when you want generated Go components and compile-time
checks as the center of your view layer.

But not every project wants a new template language.

Not every project wants to rewrite existing templates.

Not every team wants markup inside Go-shaped component files.

Not every app needs a new rendering model.

For those projects, go-doc is the missing middle:

```text
standard templates
+ typed editor intelligence
+ optional runtime helper
+ no framework lock-in
```

That is a strong place to stand.

## Why Not Just Use Pure Go HTML?

Sometimes you should.

Pure Go HTML builders can be great. They give you types because everything is
Go.

But HTML templates remain valuable because they look like HTML, are easy to
scan, and fit the mental model of server-rendered pages.

go-doc does not argue that templates are better for everyone.

It argues that if you choose templates, you should not have to give up type
awareness.

## The Bigger Idea

Go has always been good at boring software.

Servers.

Handlers.

Structs.

Interfaces.

Small tools that do one thing well.

Server-rendered HTML belongs in that world.

It should not feel like a step backward.

It should feel direct:

```go
type Page struct {
    Title string
    Users []User
}
```

```gotemplate
{{/* @dot github.com/example/app.Page */}}

<h1>{{ .Title }}</h1>
```

The type is there.

The template uses it.

The editor understands it.

That loop should be normal.

## What go-doc Wants To Make True

When you type `Page.`, you should get fields.

When you type `.`, inside a range, you should get the row type.

When you pass the wrong value to a child template, you should know immediately.

When you call a function with the wrong argument, the editor should say so.

When you hover a template field, you should see the Go declaration.

When you ctrl-click a field, you should land in Go code.

When you rename a model contract, stale diagnostics should disappear.

When templates grow, they should become more understandable, not less.

This is not extravagant.

This is the developer experience Go templates deserve.

## The Standard Library Deserves Better Tooling

The standard library is one of Go's greatest strengths.

But standard does not have to mean spartan.

`html/template` can remain small and stable while tooling around it becomes
smarter.

That is the right separation:

- the runtime stays boring
- the editor gets rich
- the contract stays visible
- the app stays in control

go-doc lives in that separation.

## The Bold Claim

If a Go template references Go data, the editor should understand the Go data.

That should be the standard expectation.

Not a luxury.

Not a framework feature.

Not something that requires abandoning `html/template`.

The template should say what it receives.

The editor should believe it.

The language server should validate it.

The developer should move faster with fewer surprises.

That is what go-doc is trying to make real.

## Final Word

go-doc is not trying to make templates clever.

It is trying to make them clear.

Clear enough to complete.

Clear enough to navigate.

Clear enough to refactor.

Clear enough to trust.

Go templates were always good.

They were just missing their type-aware editor layer.

That layer should exist.

That layer should feel boring.

That layer should become expected.

That is go-doc.
