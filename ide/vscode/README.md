# go-doc VS Code extension

This extension reads `.go-doc/index.json` and improves Go template editing in
VS Code.

It supports:

- completions for `@param` and `@var` Go type names
- completions for typed template accessors such as `_page.`
- completions for dot context inside `range` blocks
- diagnostics for unknown accessors and fields
- diagnostics for invalid range sources such as `range _page.Title`
- quick fixes for close field-name matches
- hover and go to definition for contract types and fields
- debounced automatic index rebuilds

## Requirements

Install the `go-doc` CLI and make sure it is available on `PATH`:

```bash
go install github.com/donseba/go-doc@latest
```

If the CLI is missing when the extension rebuilds the index, VS Code asks before
running that install command for you.

Generate the first index from a Go module root:

```bash
go-doc index -o .go-doc/index.json .
```

The extension also falls back to `.partial/index.json` for compatibility.

## Commands

- `go-doc: Rebuild Index`
- `go-doc: Show Index Status`
- `go-doc: Toggle Auto Index`

## Settings

- `goDoc.autoIndex`
- `goDoc.debounceMilliseconds`
