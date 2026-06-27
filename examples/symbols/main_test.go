package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderSymbolsExample(t *testing.T) {
	app, err := newApp()
	if err != nil {
		t.Fatalf("newApp() error = %v", err)
	}

	var out bytes.Buffer
	err = app.render(&out, Page{
		Title:       "Symbols",
		Description: "Runtime symbols",
	}, runtimeSymbols{
		LikesPoll: Interaction{
			ID:          "likes-poll",
			Event:       "every 5s",
			Endpoint:    "/likes",
			Description: "Poll likes",
		},
		PrimaryButton: Button{
			Label:   "Open",
			Href:    "/open",
			Variant: "primary",
			Enabled: true,
		},
	})
	if err != nil {
		t.Fatalf("render() error = %v", err)
	}

	html := out.String()
	for _, want := range []string{"Symbols", "/likes", "Poll likes", "Open", "/open"} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered html missing %q:\n%s", want, html)
		}
	}
}
