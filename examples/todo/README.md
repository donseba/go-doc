# Todo Example

This example shows a small HTTP todo app with typed templates split across
three files:

- `templates/main.gohtml` owns the page shell.
- `templates/todo_list.gohtml` renders the todo list.
- `templates/todo_detail.gohtml` renders one selected todo.

Each template declares the model it uses with `@model`, so editor integrations
can provide completion, diagnostics, hover, and navigation in the file you are
editing.

That model name is not magic template syntax. It is a two-way contract:

- the template annotation is the entrance: `@model Page ...` says this template
  expects a named accessor called `Page`
- runtime registration is the exit: the renderer scans the declarations,
  matches the Go values you pass, and registers a real template function named
  `Page` before parsing

That is why `{{ Page.Title }}` works in this example. Plain `html/template`
would not know what `Page` is unless the application registers it. If you render
with direct dot execution such as `tmpl.Execute(w, page)`, use `@dot` and
`{{ .Title }}` instead.

The app exposes:

- `GET /` redirects to `/todos`
- `GET /todos` renders the todo list and the first detail
- `GET /todos/{id}` renders the same shell with another selected detail
- `POST /todos/{id}/toggle` toggles the todo in memory and redirects back

No generated index is required. The language server can scan this module in
memory from `go.mod`. Generate `examples/todo/.go-doc/index.json` only when you
want to inspect the discovered contracts on disk.

Run the example:

```bash
cd examples/todo
go run .
```

Then open:

```text
http://localhost:8091/todos
```
