package io

import (
	"os"
	"testing"
	"testing/fstest"

	"github.com/Leoj030/go-luau/luau"
	"github.com/Leoj030/go-luau/vm"
)

func TestWriteConcatenatesArgumentsAndArrayTable(t *testing.T) {
	v, err := vm.New()
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	if err := v.State().OpenRequire(luau.FSRequirer(fstest.MapFS{})); err != nil {
		t.Fatal(err)
	}
	if err := v.Module("@std/io", Module()); err != nil {
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

	if err := v.Do(`
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
	if want := "hello\t \t42\t!\t?\t array\t table\t"; string(got) != want {
		t.Errorf("write output = %q, want %q", got, want)
	}
}
