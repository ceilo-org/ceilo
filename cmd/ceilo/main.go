// Command ceilo is a Luau runtime.
package main

import (
	"context"
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
	stop() // os.Exit doesn't run deferred calls
	os.Exit(code)
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
