package main

import (
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/donseba/go-doc/renderer"
)

type ShowcasePage struct {
	Title       string
	Description string
	Active      string
	Users       []User
	Featured    User
	GeneratedAt time.Time
}

type User struct {
	ID          int
	Name        string
	Email       string
	Role        string
	Active      bool
	Permissions []Permission
}

type Permission struct {
	Name  string
	Level string
}

func (user User) Status() string {
	if user.Active {
		return "active"
	}
	return "inactive"
}

func (user User) Initials() string {
	if user.Name == "" {
		return "?"
	}
	initials := user.Name[:1]
	for index, char := range user.Name {
		if char == ' ' && index+1 < len(user.Name) {
			initials += user.Name[index+1 : index+2]
			break
		}
	}
	return initials
}

type App struct {
	renderer renderer.Renderer
	files    []string
	users    []User
}

var showcaseUsers = []User{
	{ID: 1, Name: "Ada Lovelace", Email: "ada@example.test", Role: "Admin", Active: true, Permissions: []Permission{{Name: "templates", Level: "write"}, {Name: "release", Level: "approve"}}},
	{ID: 2, Name: "Grace Hopper", Email: "grace@example.test", Role: "Editor", Active: true, Permissions: []Permission{{Name: "docs", Level: "write"}, {Name: "cli", Level: "read"}}},
	{ID: 3, Name: "Katherine Johnson", Email: "katherine@example.test", Role: "Reviewer", Permissions: []Permission{{Name: "lsp", Level: "read"}}},
}

func main() {
	base := exampleBase()
	files := []string{
		filepath.Join(base, "templates/layout.gohtml"),
		filepath.Join(base, "templates/home.gohtml"),
		filepath.Join(base, "templates/contracts.gohtml"),
		filepath.Join(base, "templates/table.gohtml"),
		filepath.Join(base, "templates/user_card.gohtml"),
		filepath.Join(base, "templates/user_row.gohtml"),
	}
	contractRenderer, err := renderer.New(renderer.Config{
		Mode:  renderer.Development,
		Files: files,
	})
	if err != nil {
		log.Fatal(err)
	}
	app := &App{
		renderer: contractRenderer,
		files:    files,
		users:    showcaseUsers,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", app.render("home.gohtml", "home"))
	mux.HandleFunc("/contracts", app.render("contracts.gohtml", "contracts"))
	mux.HandleFunc("/table", app.render("table.gohtml", "table"))
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(staticBase()))))

	addr := "localhost:8102"
	log.Printf("go-doc showcase running on http://%s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func exampleBase() string {
	if _, err := os.Stat("templates/layout.gohtml"); err == nil {
		return "."
	}
	return "examples/showcase"
}

func staticBase() string {
	if _, err := os.Stat("static/site.css"); err == nil {
		return "static"
	}
	return "examples/showcase/static"
}

func (app *App) render(contentTemplate, active string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		app.renderTemplate(w, r, contentTemplate, active)
	}
}

func (app *App) renderTemplate(w http.ResponseWriter, r *http.Request, contentTemplate, active string) {
	if contentTemplate == "home.gohtml" && r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if contentTemplate == "table.gohtml" && r.URL.Path != "/table" {
		http.NotFound(w, r)
		return
	}
	if contentTemplate == "contracts.gohtml" && r.URL.Path != "/contracts" {
		http.NotFound(w, r)
		return
	}

	page := ShowcasePage{
		Title:       "Typed template showcase",
		Description: "A small app built from annotated Go templates.",
		Active:      active,
		Users:       app.users,
		Featured:    app.users[0],
		GeneratedAt: time.Now(),
	}
	tmpl := template.New("layout.gohtml").Funcs(template.FuncMap{
		"firstUser": FirstUser,
		"userByID":  UserByID,
	})
	if err := app.renderer.Register(tmpl, page, page.Featured); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	files := []string{
		filepath.Join(exampleBase(), "templates/layout.gohtml"),
		filepath.Join(exampleBase(), "templates/user_card.gohtml"),
		filepath.Join(exampleBase(), "templates/user_row.gohtml"),
		filepath.Join(exampleBase(), "templates", contentTemplate),
	}
	if _, err := tmpl.ParseFiles(files...); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "layout.gohtml", nil); err != nil {
		log.Printf("render showcase: %v", err)
	}
}

func FirstUser() User {
	return showcaseUsers[0]
}

func UserByID(id int) User {
	for _, user := range showcaseUsers {
		if user.ID == id {
			return user
		}
	}
	return User{}
}
