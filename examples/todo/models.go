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

func (u User) Todos() []Todo {
	// In a real application, this would query a database or other data source.
	// Here we just return a static list of todos for demonstration purposes.
	return []Todo{
		{ID: 1, Title: "Buy groceries", Description: "Milk, Bread, Eggs", Priority: "High", Done: false, DueAt: time.Now().Add(24 * time.Hour), Tags: []string{"shopping", "errands"}},
		{ID: 2, Title: "Read book", Description: "Finish reading 'The Go Programming Language'", Priority: "Medium", Done: true, DueAt: time.Now().Add(48 * time.Hour), Tags: []string{"reading", "learning"}},
	}
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

	Un string
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

type Test struct {
	FieldA string
	FieldB string

	FieldC string

	FieldD string
	FieldE string

	FieldF string
}
