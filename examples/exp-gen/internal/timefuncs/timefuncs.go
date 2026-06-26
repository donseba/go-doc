package timefuncs

import "time"

// Format formats t with the given Go time layout.
func Format(t time.Time, layout string) string {
	return t.Format(layout)
}

// Year returns the year component for t.
func Year(t time.Time) int {
	return t.Year()
}
