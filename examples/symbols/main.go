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
	renderer      renderer.Renderer
}

func main() {
	app, err := newApp()
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", app.page)

	addr := "localhost:8094"
	log.Printf("symbols example listening on http://%s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func newApp() (*app, error) {
	files := []string{"templates/page.gohtml"}
	contractRenderer, err := renderer.New(renderer.Config{
		Mode:  renderer.Development,
		Files: files,
	})
	if err != nil {
		return nil, err
	}
	return &app{templateFiles: files, renderer: contractRenderer}, nil
}

func (app *app) page(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	page := Page{
		Title:       "Typed runtime symbols",
		Description: "go-doc can understand framework-provided names without hard-coding the framework.",
	}
	symbols := runtimeSymbols{
		LikesPoll: Interaction{
			ID:          "likes-poll",
			Event:       "every 5s",
			Endpoint:    "/posts/42/likes",
			Description: "Polls the current like count without making it part of the page model.",
		},
		PrimaryButton: Button{
			Label:   "Open dashboard",
			Href:    "/dashboard",
			Variant: "primary",
			Enabled: true,
		},
	}

	var body bytes.Buffer
	if err := app.render(&body, page, symbols); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = body.WriteTo(w)
}

func (app *app) render(w io.Writer, page Page, symbols runtimeSymbols) error {
	tmpl := template.New("page.gohtml").Funcs(symbolFuncMap(symbols))
	if err := app.renderer.Register(tmpl, page); err != nil {
		return err
	}
	if _, err := tmpl.ParseFiles(app.templateFiles...); err != nil {
		return err
	}
	return tmpl.ExecuteTemplate(w, "page.gohtml", nil)
}

func symbolFuncMap(symbols runtimeSymbols) template.FuncMap {
	return template.FuncMap{
		"LikesPoll": func() Interaction {
			return symbols.LikesPoll
		},
		"PrimaryButton": func() Button {
			return symbols.PrimaryButton
		},
	}
}
