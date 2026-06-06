package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "cut":
		cmdCut(os.Args[2:])
	case "rotate":
		cmdRotate(os.Args[2:])
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	prog := filepath.Base(os.Args[0])
	fmt.Fprintf(os.Stderr, "usage: %s <command> [flags] <path>\n\n", prog)
	fmt.Fprintln(os.Stderr, "commands:")
	fmt.Fprintln(os.Stderr, "  cut     split half-frame scans into left/right halves")
	fmt.Fprintln(os.Stderr, "  rotate  rotate images by 90, 180, or 270 degrees clockwise")
	fmt.Fprintf(os.Stderr, "\nrun '%s <command> -h' for command-specific flags\n", prog)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
