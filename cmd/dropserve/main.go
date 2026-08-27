// Command dropserve is the Dropserve command-line entry point.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/tanzir71/dropserve/internal/config"
	"github.com/tanzir71/dropserve/internal/scanner"
	dropserver "github.com/tanzir71/dropserve/internal/server"
	"github.com/tanzir71/dropserve/internal/version"
)

const usage = `Dropserve hosts folders as local websites.

Usage:
  dropserve serve      run in the foreground
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
	case "serve":
		return serveCommand(args[1:], stdout, stderr, configPath)
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

type rootFlags []string

func (roots *rootFlags) String() string {
	return strings.Join(*roots, ",")
}

func (roots *rootFlags) Set(value string) error {
	if value == "" {
		return errors.New("root path cannot be empty")
	}
	*roots = append(*roots, value)
	return nil
}

func serveCommand(arguments []string, stdout, stderr io.Writer, injectedConfigPath string) int {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	listenAddress := flags.String("listen", "", "listener address; use 127.0.0.1:0 for a random local port")
	configPath := flags.String("config", injectedConfigPath, "configuration file")
	var roots rootFlags
	flags.Var(&roots, "root", "Apps root; repeat to use more than one")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		if _, err := fmt.Fprintln(stderr, "The serve command accepts flags only. Run 'dropserve help' for examples."); err != nil {
			return 1
		}
		return 2
	}

	configuration := config.Default()
	shouldLoadConfig := *configPath != "" || len(roots) == 0
	if shouldLoadConfig {
		if *configPath == "" {
			var err error
			*configPath, err = config.DefaultPath()
			if err != nil {
				if _, writeErr := fmt.Fprintf(stderr, "Dropserve could not find its config folder: %v\n", err); writeErr != nil {
					return 1
				}
				return 1
			}
		}
		loaded, err := config.Load(*configPath)
		if err != nil {
			if _, writeErr := fmt.Fprintf(stderr, "Dropserve could not read its config: %v\n", err); writeErr != nil {
				return 1
			}
			return 1
		}
		configuration = loaded
	}
	if len(roots) != 0 {
		configuration.Server.AppsRoots = append([]string(nil), roots...)
	}
	if *listenAddress == "" {
		*listenAddress = net.JoinHostPort(configuration.Server.Bind, strconv.Itoa(configuration.Server.HTTPPort))
	}

	handler, err := dropserver.New(scanner.Options{
		Roots:      configuration.Server.AppsRoots,
		Registered: configuration.Server.RegisteredApps,
	})
	if err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "Dropserve could not scan your app folders: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}

	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(context.Background(), "tcp", *listenAddress)
	if err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "Dropserve could not use %s: %v\n", *listenAddress, err); writeErr != nil {
			return 1
		}
		return 1
	}
	defer func() {
		_ = listener.Close()
	}()

	address := listenerURL(listener.Addr())
	if _, err := fmt.Fprintf(stdout, "Dropserve is ready at %s\n", address); err != nil {
		return 1
	}
	httpServer := &http.Server{
		Handler:           handler.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		if _, writeErr := fmt.Fprintf(stderr, "Dropserve stopped serving: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	return 0
}

func listenerURL(address net.Addr) string {
	host, port, err := net.SplitHostPort(address.String())
	if err != nil {
		return "http://" + address.String()
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}
