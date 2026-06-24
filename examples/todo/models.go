package main

import "time"

// TodoPage is the top-level model for the todo shell.
type TodoPage struct {
	Title       string
	Owner       User
	Todos       []Todo
	Selected    Todo
	OpenCount   int
	DoneCount   int
	GeneratedAt time.Time
}

// User is the person who owns the todo list.
type User struct {
	ID    int
	Name  string
	Email string
}

// Todo is one task in the todo list.
type Todo struct {
	ID          int
	Title       string
	Description string
	Priority    string
	Done        bool
	DueAt       time.Time
	Tags        []string
}

// Status returns the display status for the todo.
func (t Todo) Status() string {
	if t.Done {
		return "done"
	}
	return "open"
}

// DueLabel formats the due date for templates.
func (t Todo) DueLabel() string {
	return t.DueAt.Format("2 Jan 2006")
}
