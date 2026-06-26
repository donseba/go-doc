package helpers

import "time"

type User struct {
	Name string
}

func Add(x, y int) int {
	return x + y
}

func Format(t time.Time, layout string) string {
	return t.Format(layout)
}

func First(users []User) User {
	if len(users) == 0 {
		return User{}
	}
	return users[0]
}

func Join(values ...string) string {
	out := ""
	for _, value := range values {
		out += value
	}
	return out
}

func SkipGeneric[T any](value T) T {
	return value
}

func hidden() {}
