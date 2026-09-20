//go:build windows

package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// printLine writes one line for a person or a script to read.
//
// The release build is a GUI-subsystem program, so it starts with no console and
// no standard output unless whoever launched it redirected one. A redirect or a
// pipe (a script capturing the output) gives a working os.Stdout, so that is
// tried first. Failing that, attach to the parent's console and write there,
// which is what someone typing the command at a prompt is looking at.
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
