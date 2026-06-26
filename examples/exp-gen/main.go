package main

import (
	"html/template"
	"log"
	"net/http"
	"time"

	"github.com/donseba/go-doc/examples/exp-gen/gen"
	"github.com/donseba/go-doc/renderer"
)

type Page struct {
	Title       string
	GeneratedAt time.Time
	Accounts    []Account
	TotalSeats  int
}

type Account struct {
	Owner        string
	Plan         string
	Seats        int
	MonthlyCents int
}

func main() {
	page := Page{
		Title:       "Generated helper namespace",
		GeneratedAt: time.Now(),
		Accounts: []Account{
			{Owner: "Ada Lovelace", Plan: "Team", Seats: 8, MonthlyCents: 12900},
			{Owner: "Grace Hopper", Plan: "Starter", Seats: 3, MonthlyCents: 3900},
			{Owner: "Katherine Johnson", Plan: "Business", Seats: 12, MonthlyCents: 22900},
		},
		TotalSeats: 23,
	}

	views, err := renderer.New(renderer.Config{
		Mode:  renderer.Development,
		Files: []string{"templates/page.gohtml"},
		Funcs: gen.FuncMap(),
	})
	if err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		tmpl := template.New("page.gohtml")
		if err := views.Register(tmpl, page); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if _, err := tmpl.ParseFiles("templates/page.gohtml"); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := tmpl.ExecuteTemplate(w, "page.gohtml", nil); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	log.Println("exp-gen example on http://localhost:8093")
	log.Fatal(http.ListenAndServe(":8093", nil))
}
