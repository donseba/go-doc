package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTodoRoutesRenderAndToggle(t *testing.T) {
	app := newApp()

	t.Run("list", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/todos", nil)
		rec := httptest.NewRecorder()

		app.todosPage(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "Wire template contracts") || !strings.Contains(body, "Polish diagnostics") {
			t.Fatalf("body does not contain todo list:\n%s", body)
		}
	})

	t.Run("detail", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/todos/2", nil)
		rec := httptest.NewRecorder()

		app.todoRoute(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "Make unknown fields and invalid ranges easy to spot.") {
			t.Fatalf("body does not contain selected todo detail:\n%s", rec.Body.String())
		}
	})

	t.Run("toggle", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/todos/1/toggle", nil)
		rec := httptest.NewRecorder()

		app.todoRoute(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d", rec.Code)
		}
		todo, ok := findTodo(app.snapshot(), 1)
		if !ok || !todo.Done {
			t.Fatalf("todo after toggle = %#v, ok = %v", todo, ok)
		}
	})
}
