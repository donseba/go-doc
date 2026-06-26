package moneyfuncs

import "fmt"

// EUR formats cents as a euro amount.
func EUR(cents int) string {
	return fmt.Sprintf("EUR %.2f", float64(cents)/100)
}

// Add returns x plus y.
func Add(x, y int) int {
	return x + y
}
