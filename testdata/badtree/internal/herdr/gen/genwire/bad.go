package main

// A package under internal/herdr is held to the standard library too, and may
// import gen; only notstd must be reported.
import (
	_ "example.com/notstd"
	_ "github.com/vilosource/herddash/internal/herdr/gen"
)

func main() {}
