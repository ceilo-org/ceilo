package io

import (
	"os"
	"testing"
	"testing/fstest"

	"github.com/Leoj030/go-luau/luau"
)

func TestWriteConcatenatesArgumentsAndArrayTable(t *testing.T) {
	state, err := luau.NewState()
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	if err := state.OpenRequire(luau.FSRequirer(fstest.MapFS{})); err != nil {
		t.Fatal(err)
	}
	if err := state.RegisterModule("@std/io", Module()); err != nil {
		t.Fatal(err)
	}

	output, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()

	originalStdout := os.Stdout
	os.Stdout = output
	defer func() { os.Stdout = originalStdout }()

	if err := state.DoString(`
		local io = require("@std/io")
		io.write("hello", " ", 42, {"!", "?"})
		io.write({" array", " table"})
	`); err != nil {
		t.Fatal(err)
	}

	if err := output.Sync(); err != nil {
		t.Fatal(err)
	}
	if _, err := output.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	if want := "hello 42!? array table"; string(got) != want {
		t.Errorf("write output = %q, want %q", got, want)
	}
}
