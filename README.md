# go-doc

`go-doc` is an indexer for Go template contracts.

It scans Go structs and template annotations such as:

```gotemplate
{{/*
@param todo github.com/example/app.Todo
*/}}
```

The generated index can be consumed by IDE plugins to provide typeahead and
diagnostics inside Go template files.

## Install

```bash
go install github.com/donseba/go-doc@latest
```

## Usage

From a Go module root:

```bash
go-doc index -o .go-doc/index.json .
```

For compatibility with the first go-partial proof of concept, the GoLand plugin
also reads `.partial/index.json`.

## Commands

```bash
go-doc types [-query Todo] [root]
go-doc templates [root]
go-doc index [-o .go-doc/index.json] [root]
```

## Local Development

This repository includes a `Taskfile.yml` for common local commands.

Install Task first:

```bash
go install github.com/go-task/task/v3/cmd/task@latest
```

Useful tasks:

```bash
task doctor
task install:tools
task test
task build:goland
task build:vscode
task dist
```

`task install:tools` can install the local development toolchain for your
platform. On Windows it uses Scoop to install Go, Node.js, JDK 17, and Gradle.
On macOS it uses Homebrew.

## IDEs

The GoLand plugin lives in:

```text
ide/goland
```

The VS Code extension lives in:

```text
ide/vscode
```

Both consume `.go-doc/index.json` and fall back to `.partial/index.json`.

## Local Artifacts

Build outputs are collected locally in:

```text
dist
```

Current artifacts include:

- GoLand plugin ZIP
- VS Code VSIX
- SHA256 checksums

The CLI is distributed through `go install github.com/donseba/go-doc@latest`;
release archives only contain the IDE packages.

Official release artifacts are built by CircleCI from Git tags. See
[docs/release.md](docs/release.md).
