//go:build windows

package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// printLine writes one line for a person or a script to read. The release build
// has no console, so stdout works only when redirected. Else use the parent's.
func printLine(s string) {
	if _, err := fmt.Fprintln(os.Stdout, s); err == nil {
		return
	}
	attach := windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")
	const attachParentProcess = ^uintptr(0)
	if r, _, _ := attach.Call(attachParentProcess); r == 0 {
		return
	}
	console, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
	if err != nil {
		return
	}
	defer console.Close()
	fmt.Fprintln(console, s)
}
