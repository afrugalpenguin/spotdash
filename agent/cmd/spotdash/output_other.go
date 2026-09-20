//go:build !windows

package main

import "fmt"

// printLine writes one line for a person or a script to read.
func printLine(s string) { fmt.Println(s) }
