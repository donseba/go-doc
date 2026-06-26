package main

import (
	"html/template"
	"testing"
	"time"

	"github.com/donseba/go-doc/examples/exp-gen/gen"
	"github.com/donseba/go-doc/renderer"
)

func TestGeneratedNamespacesParseAtRuntime(t *testing.T) {
	page := Page{
		Title:       "Generated helper namespace",
		GeneratedAt: time.Now(),
		Accounts: []Account{
			{Owner: "Ada Lovelace", Plan: "Team", Seats: 8, MonthlyCents: 12900},
		},
		TotalSeats: 8,
	}

	views, err := renderer.New(renderer.Config{
		Mode:  renderer.Development,
		Files: []string{"templates/page.gohtml"},
		Funcs: gen.FuncMap(),
	})
	if err != nil {
		t.Fatal(err)
	}

	tmpl := template.New("page.gohtml")
	if err := views.Register(tmpl, page); err != nil {
		t.Fatal(err)
	}
	if _, err := tmpl.ParseFiles("templates/page.gohtml"); err != nil {
		t.Fatal(err)
	}
}
