package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestUserTableRendersRowsThroughSubtemplate(t *testing.T) {
	app, err := newApp()
	if err != nil {
		t.Fatalf("newApp() error = %v", err)
	}

	var body bytes.Buffer
	if err := app.render(&body); err != nil {
		t.Fatalf("render() error = %v", err)
	}

	html := body.String()
	for _, expected := range []string{
		"<h1>Users</h1>",
		"<td>Ada Lovelace</td>",
		"<td>Grace Hopper</td>",
		"<td>Katherine Johnson</td>",
		"<td>active</td>",
		"<td>invited</td>",
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("rendered html missing %q:\n%s", expected, html)
		}
	}
}

func TestSingleFileTemplateRendersLocalDefines(t *testing.T) {
	app, err := newApp()
	if err != nil {
		t.Fatalf("newApp() error = %v", err)
	}

	var body bytes.Buffer
	if err := app.renderTemplate(&body, "single_file.gohtml", app.singleFile); err != nil {
		t.Fatalf("renderTemplate() error = %v", err)
	}

	html := body.String()
	for _, expected := range []string{
		"go-doc single-file template example",
		`id="single-user-1"`,
		"<td>Ada Lovelace</td>",
		`<span class="pill">active</span>`,
		`<span class="pill">invited</span>`,
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("rendered html missing %q:\n%s", expected, html)
		}
	}
}
