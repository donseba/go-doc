package main

// Page is the normal page model registered from an @model contract.
type Page struct {
	Title       string // some Title documentation
	Description string
}

// Interaction is a framework-style runtime symbol.
//
// A real package such as go-partial could expose this type and register concrete
// interactions for the template runtime.
type Interaction struct {
	ID          string
	Event       string
	Endpoint    string
	Description string
}

// Button is an application component symbol.
type Button struct {
	Label   string
	Href    string
	Variant string
	Enabled bool
}

type runtimeSymbols struct {
	LikesPoll     Interaction
	PrimaryButton Button
}
