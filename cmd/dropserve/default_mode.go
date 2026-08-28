package main

import "io"

type defaultModeOptions struct {
	ServeArguments []string
	ConfigPath     string
	StatePath      string
	AppsRoot       string
	Executable     string
	Stdout         io.Writer
	Stderr         io.Writer
}
