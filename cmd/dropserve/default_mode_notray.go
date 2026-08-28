//go:build !tray

package main

import "context"

func runDefaultMode(ctx context.Context, options defaultModeOptions) int {
	return serveCommandContext(ctx, options.ServeArguments, options.Stdout, options.Stderr, "")
}
