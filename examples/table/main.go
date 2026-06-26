package main

import (
	"bytes"
	"html/template"
	"io"
	"log"
	"net/http"

	"github.com/donseba/go-doc/renderer"
)

type app struct {
	templateFiles []string
	singleFile    string
	renderer      renderer.Renderer
	users         []User
}

func main() {
	app, err := newApp()
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", app.usersPage)
	mux.HandleFunc("/single-file", app.singleFilePage)

	addr := "localhost:8100"
	log.Printf("table example listening on http://%s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func newApp() (*app, error) {
	templateFiles := []string{
		"templates/main.gohtml",
		"templates/user_row.gohtml",
		"templates/single_file.gohtml",
	}
	contractRenderer, err := renderer.New(renderer.Config{
		Mode:  renderer.Production,
		Files: templateFiles,
	})
	if err != nil {
		return nil, err
	}
	return &app{
		templateFiles: templateFiles,
		singleFile:    "templates/single_file.gohtml",
		renderer:      contractRenderer,
		users: []User{
			{ID: 1, Name: "Ada Lovelace", Email: "ada@example.test", Role: "Admin", Active: true},
			{ID: 2, Name: "Grace Hopper", Email: "grace@example.test", Role: "Editor", Active: true},
			{ID: 3, Name: "Katherine Johnson", Email: "katherine@example.test", Role: "Viewer"},
		},
	}, nil
}

func (app *app) usersPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body bytes.Buffer
	if err := app.render(&body); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = body.WriteTo(w)
}

func (app *app) singleFilePage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body bytes.Buffer
	if err := app.renderTemplate(&body, "single_file.gohtml", app.singleFile); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = body.WriteTo(w)
}

func (app *app) render(w io.Writer) error {
	return app.renderTemplate(w, "main.gohtml", app.templateFiles...)
}

func (app *app) renderTemplate(w io.Writer, name string, files ...string) error {
	page := UserTablePage{
		Title: "Users",
		Users: app.users,
	}

	tmpl := template.New(name).Funcs(template.FuncMap{
		"firstUser":   FirstUser,
		"activeUsers": ActiveUsers,
		"userByID":    UserByID,
	})
	if err := app.renderer.Register(tmpl, page); err != nil {
		return err
	}
	if _, err := tmpl.ParseFiles(files...); err != nil {
		return err
	}
	return tmpl.ExecuteTemplate(w, name, nil)
}
