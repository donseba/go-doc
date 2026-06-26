package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/donseba/go-doc/exp/gen"
)

func main() {
	var opts gen.Options
	out := flag.String("out", "", "write generated Go source to this file")
	templates := flag.String("templates", "", "comma-separated templates to scan for experimental @gen declarations; when omitted, go-doc scans the nearest Go module")
	flag.StringVar(&opts.PackagePath, "pkg", "", "Go package path to wrap")
	flag.StringVar(&opts.Namespace, "namespace", "", "template namespace name")
	flag.StringVar(&opts.PackageName, "package", "godocgen", "generated Go package name")
	flag.Parse()

	if opts.PackagePath == "" && flag.NArg() > 0 {
		opts.PackagePath = flag.Arg(0)
	}

	source, err := generate(context.Background(), opts, splitList(*templates))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if *out == "" {
		_, _ = os.Stdout.Write(source)
		return
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, source, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate(ctx context.Context, opts gen.Options, templates []string) ([]byte, error) {
	if len(templates) == 0 && opts.PackagePath != "" {
		return gen.Generate(ctx, opts)
	}
	if len(templates) == 0 {
		root, err := findModuleRoot(".")
		if err != nil {
			return nil, err
		}
		directives, err := gen.DirectivesFromDir(root)
		if err != nil {
			return nil, err
		}
		return gen.GenerateDirectives(ctx, opts.PackageName, directives)
	}

	directives, err := gen.DirectivesFromFiles(templates...)
	if err != nil {
		return nil, err
	}
	if opts.PackagePath != "" {
		directives = append(directives, gen.Directive{
			Namespace:   opts.Namespace,
			PackagePath: opts.PackagePath,
		})
	}
	return gen.GenerateDirectives(ctx, opts.PackageName, directives)
}

func findModuleRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !info.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("gen: no go.mod found above %s", start)
		}
		dir = parent
	}
}

func splitList(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
