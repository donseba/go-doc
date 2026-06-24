# Release

Official distributable artifacts are built by CircleCI from Git tags.

Local builds are still useful while developing, but a release should come from CI
so the artifacts are reproducible and attached to the tagged revision.

## Version Rule

The Git tag, GoLand plugin version, and VS Code extension version must match.

For example, tag `v0.1.0` requires:

- `ide/goland/build.gradle.kts`: `version = "0.1.0"`
- `ide/vscode/package.json`: `"version": "0.1.0"`

CircleCI checks this before building artifacts.

## Release Steps

1. Update both IDE versions.
2. Commit the version change.
3. Create and push a tag:

```bash
git tag v0.1.0
git push origin v0.1.0
```

CircleCI runs the release workflow only for tags matching:

```text
vMAJOR.MINOR.PATCH
```

The workflow builds:

- Windows `go-doc` CLI
- macOS amd64 `go-doc` CLI
- macOS arm64 `go-doc` CLI
- GoLand plugin ZIP
- VS Code VSIX
- SHA256 checksums

CircleCI stores the generated `dist` folder as workflow artifacts.

## Local Build

Run the same build script locally when you want to test packaging:

```bash
bash scripts/build-dist.sh
```
