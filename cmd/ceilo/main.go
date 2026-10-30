// Ceilo - A Luau runtime.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/ceilo-org/ceilo/internal/cli"
)

// version is set at build time:
//
//	go build -ldflags "-X main.version=v0.1.0" ./cmd/ceilo
//
// A binary made with `go install ...@v0.1.0` takes it from the module
// version instead.
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.Run(ctx, cli.Env{
		Name:    "ceilo",
		Version: buildVersion(),
		Stdin:   os.Stdin,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
	}, os.Args[1:])
	if err := writeInterruptNewline(ctx, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "ceilo: write newline: %v\n", err)
	}
	stop()
	os.Exit(code)
}

func writeInterruptNewline(ctx context.Context, out io.Writer) error {
	if ctx.Err() == nil {
		return nil
	}
	_, err := io.WriteString(out, "\n")
	return err
}

func buildVersion() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return version
}
