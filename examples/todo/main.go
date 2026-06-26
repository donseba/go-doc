package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/donseba/go-doc/renderer"
)

type app struct {
	mu            sync.RWMutex
	owner         User
	todos         []Todo
	templateFiles []string
	renderer      renderer.Renderer
}

func main() {
	app := newApp()

	mux := http.NewServeMux()
	mux.HandleFunc("/", app.home)
	mux.HandleFunc("/todos", app.todosPage)
	mux.HandleFunc("/todos/", app.todoRoute)

	addr := "localhost:8099"
	log.Printf("todo example listening on http://%s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func newApp() *app {
	templateFiles := []string{
		"templates/main.gohtml",
		"templates/todo_list.gohtml",
		"templates/todo_detail.gohtml",
	}
	contractRenderer, err := renderer.New(renderer.Config{
		Mode:  renderer.Development,
		Files: templateFiles,
		Funcs: FuncMap,
	})
	if err != nil {
		panic(err)
	}
	return &app{
		owner:         User{ID: 42, Name: "Ada Lovelace", Email: "ada@example.test"},
		templateFiles: templateFiles,
		renderer:      contractRenderer,
		todos: []Todo{
			{
				ID:          1,
				Title:       "Wire template contracts",
				Description: "Add @model annotations so the editor understands template data.",
				Priority:    "high",
				DueAt:       date(2026, time.June, 25),
				Tags:        []string{"templates", "lsp"},
			},
			{
				ID:          2,
				Title:       "Polish diagnostics",
				Description: "Make unknown fields and invalid ranges easy to spot.",
				Priority:    "medium",
				Done:        true,
				DueAt:       date(2026, time.June, 27),
				Tags:        []string{"ide", "quality"},
			},
			{
				ID:          3,
				Title:       "Write example docs",
				Description: "Keep the example small enough to understand at a glance.",
				Priority:    "low",
				DueAt:       date(2026, time.July, 1),
				Tags:        []string{"docs"},
			},
		},
	}
}

func (app *app) home(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/todos", http.StatusFound)
}

func (app *app) todosPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/todos" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	todos := app.snapshot()
	selected := Todo{}
	if len(todos) > 0 {
		selected = todos[0]
	}
	app.render(w, pageModel(todos, selected, app.owner))
}

func (app *app) todoRoute(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/todos/")
	idPart := strings.TrimSuffix(path, "/toggle")
	id, err := strconv.Atoi(strings.Trim(idPart, "/"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if strings.HasSuffix(path, "/toggle") {
		app.toggleTodo(w, r, id)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	todos := app.snapshot()
	selected, ok := findTodo(todos, id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	app.render(w, pageModel(todos, selected, app.owner))
}

func (app *app) toggleTodo(w http.ResponseWriter, r *http.Request, id int) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !app.toggle(id) {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/todos/%d", id), http.StatusSeeOther)
}

func (app *app) render(w http.ResponseWriter, page TodoPage) {
	tmpl := template.New("main.gohtml")
	if err := app.renderer.Register(tmpl, page, page.Selected, page.Owner); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := tmpl.ParseFiles(app.templateFiles...); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "main.gohtml", nil); err != nil {
		log.Printf("render template: %v", err)
	}
}

func (app *app) snapshot() []Todo {
	app.mu.RLock()
	defer app.mu.RUnlock()

	todos := make([]Todo, len(app.todos))
	copy(todos, app.todos)
	return todos
}

func (app *app) toggle(id int) bool {
	app.mu.Lock()
	defer app.mu.Unlock()

	for index := range app.todos {
		if app.todos[index].ID == id {
			app.todos[index].Done = !app.todos[index].Done
			return true
		}
	}
	return false
}

func pageModel(todos []Todo, selected Todo, owner User) TodoPage {
	return TodoPage{
		Title:       "Typed todo templates",
		Owner:       owner,
		Todos:       todos,
		Selected:    selected,
		OpenCount:   countOpen(todos),
		DoneCount:   countDone(todos),
		GeneratedAt: time.Now(),
	}
}

func findTodo(todos []Todo, id int) (Todo, bool) {
	for _, todo := range todos {
		if todo.ID == id {
			return todo, true
		}
	}
	return Todo{}, false
}

func countOpen(todos []Todo) int {
	count := 0
	for _, todo := range todos {
		if !todo.Done {
			count++
		}
	}
	return count
}

func countDone(todos []Todo) int {
	count := 0
	for _, todo := range todos {
		if todo.Done {
			count++
		}
	}
	return count
}

func date(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 9, 0, 0, 0, time.UTC)
}
