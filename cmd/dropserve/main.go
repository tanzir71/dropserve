package main

import (
	"fmt"
	"io"
	"os"

	"github.com/tanzir71/dropserve/internal/version"
)

const usage = `Dropserve hosts folders as local websites.

Usage:
  dropserve version    print the version and build commit
  dropserve help       show this help
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(stdout, usage)
		return 0
	}

	switch args[0] {
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "dropserve %s (%s)\n", version.Version, version.Commit)
		return 0
	default:
		fmt.Fprintf(stderr, "Unknown command %q. Run 'dropserve help' to see the available commands.\n", args[0])
		return 2
	}
}
