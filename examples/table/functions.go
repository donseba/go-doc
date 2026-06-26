package main

// FirstUser returns a complex value that templates can inspect.
func FirstUser() User {
	return User{ID: 1, Name: "Ada Lovelace", Email: "ada@example.test", Role: "Admin", Active: true}
}

// ActiveUsers returns a slice so range blocks can infer the row dot type.
func ActiveUsers() []User {
	return []User{
		{ID: 1, Name: "Ada Lovelace", Email: "ada@example.test", Role: "Admin", Active: true},
		{ID: 2, Name: "Grace Hopper", Email: "grace@example.test", Role: "Editor", Active: true},
	}
}

// UserByID returns a user for parenthesized template call examples.
func UserByID(id int) User {
	for _, user := range ActiveUsers() {
		if user.ID == id {
			return user
		}
	}
	return User{}
}
