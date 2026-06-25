# Todo Example

This example shows a small HTTP todo app with typed templates split across
three files:

- `templates/main.gohtml` owns the page shell.
- `templates/todo_list.gohtml` renders the todo list.
- `templates/todo_detail.gohtml` renders one selected todo.

Each template declares the model it uses with `@model`, so editor integrations
can provide completion, diagnostics, hover, and navigation in the file you are
editing. Runtime rendering uses `renderer.RegisterFromFiles`, which scans those
declarations and exposes the matched values through the declared model names,
for example `{{ Page.Title }}`.

The app exposes:

- `GET /` redirects to `/todos`
- `GET /todos` renders the todo list and the first detail
- `GET /todos/{id}` renders the same shell with another selected detail
- `POST /todos/{id}/toggle` toggles the todo in memory and redirects back

Generate the index from the repository root:

```bash
go run ./cmd/go-doc index -o examples/todo/.go-doc/index.json examples/todo
```

Run the example:

```bash
cd examples/todo
go run .
```

Then open:

```text
http://localhost:8091/todos
```
