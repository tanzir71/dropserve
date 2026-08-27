// Command dropserve is the Dropserve command-line entry point.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/tanzir71/dropserve/internal/config"
	"github.com/tanzir71/dropserve/internal/version"
)

const usage = `Dropserve hosts folders as local websites.

Usage:
  dropserve version    print the version and build commit
  dropserve add PATH   register an app folder without moving it
  dropserve help       show this help
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	return runWithConfigPath(args, stdout, stderr, "")
}

func runWithConfigPath(args []string, stdout, stderr io.Writer, configPath string) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		if _, err := fmt.Fprint(stdout, usage); err != nil {
			return 1
		}
		return 0
	}

	switch args[0] {
	case "version", "--version", "-v":
		if _, err := fmt.Fprintf(stdout, "dropserve %s (%s)\n", version.Version, version.Commit); err != nil {
			return 1
		}
		return 0
	case "add":
		if len(args) != 2 {
			if _, err := fmt.Fprintln(stderr, "Choose one app folder: dropserve add <path>"); err != nil {
				return 1
			}
			return 2
		}
		if configPath == "" {
			var err error
			configPath, err = config.DefaultPath()
			if err != nil {
				if _, writeErr := fmt.Fprintf(stderr, "Dropserve could not find its config folder: %v\n", err); writeErr != nil {
					return 1
				}
				return 1
			}
		}
		registeredPath, changed, err := config.Register(configPath, args[1])
		if err != nil {
			if _, writeErr := fmt.Fprintf(stderr, "Dropserve could not add that folder: %v\n", err); writeErr != nil {
				return 1
			}
			return 1
		}
		if changed {
			if _, err := fmt.Fprintf(stdout, "Added %s. Dropserve will serve it without moving or changing it.\n", registeredPath); err != nil {
				return 1
			}
		} else if _, err := fmt.Fprintf(stdout, "%s is already registered.\n", registeredPath); err != nil {
			return 1
		}
		return 0
	default:
		if _, err := fmt.Fprintf(stderr, "Unknown command %q. Run 'dropserve help' to see the available commands.\n", args[0]); err != nil {
			return 1
		}
		return 2
	}
}
