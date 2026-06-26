package textfuncs

import "strings"

// Initials returns uppercase initials for a display name.
func Initials(name string) string {
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return ""
	}
	var out strings.Builder
	for _, part := range parts {
		out.WriteString(strings.ToUpper(part[:1]))
	}
	return out.String()
}

// Upper returns value in uppercase.
func Upper(value string) string {
	return strings.ToUpper(value)
}
