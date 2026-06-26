package main

// UserTablePage is the top-level model for the table page.
type UserTablePage struct {
	Title string
	Users []User
}

// User is rendered by the table row subtemplate.
type User struct {
	ID     int
	Name   string
	Email  string
	Role   string
	Active bool
}

// Status returns a display label for the row.
func (u User) Status() string {
	if u.Active {
		return "active"
	}
	return "invited"
}
