package main

import "html/template"

var FuncMap = template.FuncMap{
	"add": Add,
	"sub": Sub,
	"div": Div,
	"mul": Mul,
}

// Add returns the sum of two integers.
func Add(x, y int) int {
	return x + y
}

// Sub subtract returns the difference of two integers.
func Sub(x, y int) int {
	return x - y
}

// Div divides x with y
func Div(x, y int) int {
	return x / y
}

// Mul multiplies x with y
func Mul(x, y int) int {
	return x * y
}
