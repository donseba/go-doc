package main

import (
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

type NavItem struct {
	Path  string
	Label string
	Group string
}

type PageData struct {
	Title       string
	Description string
	Section     string
	Path        string
	Nav         []NavItem
}

type App struct {
	templates string
	nav       []NavItem
}

func main() {
	app := &App{
		templates: "examples/docs/templates",
		nav: []NavItem{
			{Path: "/", Label: "Introduction", Group: "Guide"},
			{Path: "/docs/install", Label: "Install", Group: "Guide"},
			{Path: "/docs/contracts", Label: "Contracts", Group: "Guide"},
			{Path: "/docs/annotations", Label: "Annotations", Group: "Guide"},
			{Path: "/docs/generated-helpers", Label: "Generated helpers", Group: "Guide"},
			{Path: "/docs/editor", Label: "Editor support", Group: "Guide"},
			{Path: "/docs/renderer", Label: "Renderer", Group: "Guide"},
			{Path: "/docs/cli", Label: "CLI and index", Group: "Reference"},
			{Path: "/docs/lsp", Label: "LSP behavior", Group: "Reference"},
			{Path: "/docs/ecosystem", Label: "Ecosystem", Group: "Reference"},
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", app.page("overview.gohtml", "Typed contracts for Go templates", "go-doc adds editor intelligence to normal html/template files.", "Documentation"))
	mux.HandleFunc("/docs/install", app.page("install.gohtml", "Install", "Set up the CLI and editor integrations.", "Getting Started"))
	mux.HandleFunc("/docs/contracts", app.page("contracts.gohtml", "Template contracts", "Declare the data shape once, then let the editor follow it.", "Core Concepts"))
	mux.HandleFunc("/docs/annotations", app.page("annotations.gohtml", "Annotations", "Model, dot, and function annotations that describe template data.", "Core Concepts"))
	mux.HandleFunc("/docs/generated-helpers", app.page("generated_helpers.gohtml", "Generated helpers", "Experimental package-like helper namespaces for normal Go templates.", "Core Concepts"))
	mux.HandleFunc("/docs/editor", app.page("editor.gohtml", "Editor support", "Completion, diagnostics, hover, and navigation across supported editors.", "Tooling"))
	mux.HandleFunc("/docs/renderer", app.page("renderer.gohtml", "Renderer", "A small helper for registering model values without changing template execution.", "Runtime"))
	mux.HandleFunc("/docs/cli", app.page("cli.gohtml", "CLI and index", "How go-doc scans packages and produces editor metadata.", "Reference"))
	mux.HandleFunc("/docs/lsp", app.page("lsp.gohtml", "LSP behavior", "What the language server understands today.", "Reference"))
	mux.HandleFunc("/docs/ecosystem", app.page("ecosystem.gohtml", "Ecosystem", "A place for go-doc, go-partial, go-translator, and the future docs hub.", "Ecosystem"))
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("examples/docs/static"))))

	addr := "localhost:8101"
	log.Printf("go-doc docs running on http://%s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func (app *App) page(templateName, title, description, section string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && !hasRoute(app.nav, r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		files := []string{
			filepath.Join(app.templates, "layout.gohtml"),
			filepath.Join(app.templates, "sidebar.gohtml"),
			filepath.Join(app.templates, "header.gohtml"),
			filepath.Join(app.templates, templateName),
		}
		tmpl, err := template.New("layout.gohtml").Funcs(template.FuncMap{
			"isActive": func(path string) bool {
				return r.URL.Path == path
			},
		}).ParseFiles(files...)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		data := PageData{
			Title:       title,
			Description: description,
			Section:     section,
			Path:        r.URL.Path,
			Nav:         app.nav,
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmpl.ExecuteTemplate(w, "layout.gohtml", data); err != nil {
			log.Printf("render docs: %v", err)
		}
	}
}

func hasRoute(nav []NavItem, path string) bool {
	for _, item := range nav {
		if item.Path == path {
			return true
		}
	}
	_, err := os.Stat(filepath.Join("examples/docs/templates", "overview.gohtml"))
	return path == "/" && err == nil
}
