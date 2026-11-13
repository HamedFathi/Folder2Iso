package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
)

// version is overridden at release build time via -ldflags "-X main.version=X.Y.Z".
var version = "1.0.0"

func run(ctx context.Context, args []string, out io.Writer) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(out, "ERROR:", err)
		return 1
	}
	return runAt(ctx, args, out, cwd)
}
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdout)
	cancel()
	os.Exit(code)
}
