package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// invoke runs the program with args and returns its exit code and output.
func invoke(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = Run(context.Background(), Env{Name: "luaurt", Version: "test", Stdout: &out, Stderr: &errb}, args)
	return code, out.String(), errb.String()
}

func TestNoArgumentsShowsHelp(t *testing.T) {
	code, _, stderr := invoke(t)
	if code != 2 || !strings.Contains(stderr, "Commands:") {
		t.Errorf("got code %d and stderr %q", code, stderr)
	}
}

func TestHelpListsCommands(t *testing.T) {
	code, stdout, _ := invoke(t, "help")
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	for _, name := range []string{"run", "help"} {
		if !strings.Contains(stdout, name) {
			t.Errorf("help doesn't list %q:\n%s", name, stdout)
		}
	}
	if strings.Contains(stdout, "\n  version  ") {
		t.Errorf("help lists version as a command:\n%s", stdout)
	}
	if !strings.Contains(stdout, "--version") {
		t.Errorf("help doesn't list the version flag:\n%s", stdout)
	}
}

func TestHelpForACommand(t *testing.T) {
	for _, args := range [][]string{{"help", "run"}, {"run", "-h"}} {
		code, stdout, _ := invoke(t, args...)
		if code != 0 || !strings.Contains(stdout, "-m, --memory int") ||
			strings.Count(stdout, "memory limit in MiB") != 1 ||
			strings.Contains(stdout, "timeout") ||
			strings.Contains(stdout, "\n  -m int") || strings.Contains(stdout, "\n  --memory int") ||
			!strings.Contains(stdout, "Usage:") {
			t.Errorf("%v: code %d, stdout %q", args, code, stdout)
		}
	}
}

func TestUnknownCommand(t *testing.T) {
	code, _, stderr := invoke(t, "nope")
	if code != 2 || !strings.Contains(stderr, `unknown command "nope"`) {
		t.Errorf("got code %d and stderr %q", code, stderr)
	}
}

func TestUnknownFlag(t *testing.T) {
	code, _, stderr := invoke(t, "run", "-nope", "x.luau")
	if code != 2 || !strings.Contains(stderr, "not defined") {
		t.Errorf("got code %d and stderr %q", code, stderr)
	}
}

func TestRunFlagsCanAppearBeforeOrAfterScript(t *testing.T) {
	script := filepath.Join(t.TempDir(), "hello.luau")
	if err := os.WriteFile(script, []byte("assert(select('#', ...) == 2)"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{
		{"run", "--memory=32", script, "a", "-b"},
		{"run", "-m=32", script, "a", "-b"},
		{"run", script, "-m=32", "a", "-b"},
		{"-m=32", "run", script, "a", "-b"},
		{"-m=32", script, "a", "-b"},
	} {
		code, _, stderr := invoke(t, args...)
		if code != 0 {
			t.Errorf("%v: code %d, stderr %q", args, code, stderr)
		}
	}
}

func TestFlagsSupportAliasesAndBooleanFlagsAnywhere(t *testing.T) {
	flags := newFlags("test")
	var check bool
	var output string
	var mode string
	flags.BoolVar(&check, "check", "c", false, "check without writing")
	flags.StringVar(&output, "output", "o", "write output")
	flags.OptionalStringVar(&mode, "mode", "M", "default", "select a mode")

	flagArgs, commandArgs := flags.splitArgs([]string{"main", "--check", "-o=bin/new-app", "--mode", "tail"})
	if err := flags.Parse(flagArgs); err != nil {
		t.Fatal(err)
	}
	if !check || output != "bin/new-app" || mode != "default" {
		t.Errorf("check = %t, output = %q, mode = %q", check, output, mode)
	}
	if got := append(flags.Args(), commandArgs...); strings.Join(got, ",") != "main,tail" {
		t.Errorf("command args = %v, want [main tail]", got)
	}
}

func TestOptionalValueFlagAcceptsExplicitValue(t *testing.T) {
	flags := newFlags("test")
	var mode string
	flags.OptionalStringVar(&mode, "mode", "M", "default", "select a mode")

	if err := flags.Parse([]string{"-M=fast"}); err != nil {
		t.Fatal(err)
	}
	if mode != "fast" {
		t.Errorf("mode = %q, want fast", mode)
	}
}

func TestOptionalValueFlagBeforePositionalAndRequiredFlagWithoutValue(t *testing.T) {
	flags := newFlags("test")
	var optional, required string
	flags.OptionalStringVar(&optional, "optional", "", "default", "optional value")
	flags.StringVar(&required, "required", "", "required value")

	flagArgs, commandArgs := flags.splitArgs([]string{"--optional", "main"})
	if err := flags.Parse(flagArgs); err != nil {
		t.Fatal(err)
	}
	if optional != "default" || strings.Join(commandArgs, ",") != "main" {
		t.Errorf("optional = %q, command args = %v", optional, commandArgs)
	}

	requiredFlags, _ := flags.splitArgs([]string{"--required"})
	if err := flags.Parse(requiredFlags); err == nil || !strings.Contains(err.Error(), "flag needs an argument") {
		t.Errorf("Parse(--required) error = %v, want missing-argument error", err)
	}
}

func TestOptionalValueFlagIsMarkedInHelp(t *testing.T) {
	flags := newFlags("test")
	var mode string
	flags.OptionalStringVar(&mode, "mode", "M", "default", "select a mode")
	var help strings.Builder
	flags.Print(&help)
	if got := help.String(); !strings.Contains(got, "-M, --mode [string]") {
		t.Errorf("optional value not marked in help: %q", got)
	}
}

func TestRunNeedsAScript(t *testing.T) {
	if code, _, _ := invoke(t, "run"); code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
}

func TestVersion(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"-v"}} {
		code, stdout, _ := invoke(t, args...)
		if code != 0 || !strings.Contains(stdout, "luaurt test") {
			t.Errorf("%v: code %d, stdout %q", args, code, stdout)
		}
	}
}

func TestVersionCommandIsNoLongerSupported(t *testing.T) {
	code, _, stderr := invoke(t, "version")
	if code != 2 || !strings.Contains(stderr, `unknown command "version"`) {
		t.Errorf("got code %d and stderr %q", code, stderr)
	}
}

func TestMissingScriptFails(t *testing.T) {
	code, _, stderr := invoke(t, "run", "missing.luau")
	if code != 1 || !strings.Contains(stderr, "cannot read script") {
		t.Errorf("got code %d and stderr %q", code, stderr)
	}
}

func TestScriptShorthandAndArguments(t *testing.T) {
	script := filepath.Join(t.TempDir(), "hello.luau")
	source := `
		local process = require("@std/process")
		assert(select("#", ...) == #process.args)
		for i = 1, #process.args do
			assert(select(i, ...) == process.args[i])
		end
	`
	if err := os.WriteFile(script, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	// Memory flags can appear before or after the script; non-flag arguments stay ordered.
	code, _, stderr := invoke(t, "run", "--memory=32", script, "a", "-b")
	if code != 0 {
		t.Errorf("run: code %d, stderr %q", code, stderr)
	}
	code, _, stderr = invoke(t, "run", script, "-m=32", "a", "-b")
	if code != 0 {
		t.Errorf("run with trailing flag: code %d, stderr %q", code, stderr)
	}

	// "luaurt script.luau" is short for "luaurt run script.luau".
	code, _, stderr = invoke(t, script)
	if code != 0 {
		t.Errorf("shorthand: code %d, stderr %q", code, stderr)
	}

	// An omitted extension resolves to the corresponding .luau file.
	withoutExtension := strings.TrimSuffix(script, ".luau")
	code, _, stderr = invoke(t, "run", withoutExtension)
	if code != 0 {
		t.Errorf("extensionless script: code %d, stderr %q", code, stderr)
	}

	code, _, stderr = invoke(t, withoutExtension)
	if code != 0 {
		t.Errorf("extensionless shorthand: code %d, stderr %q", code, stderr)
	}
}

func TestScriptsDirectoryFallback(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := os.MkdirAll("scripts", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("scripts", "dev.luau"), []byte(`
		local fs = require("@std/fs")
		assert(select(1, ...) == "arg1")
		fs.writeFile("ran.txt", "scripts")
	`), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{
		{"dev", "arg1"},
		{"dev.luau", "arg1"},
		{"run", "dev", "arg1"},
		{"run", "dev.luau", "arg1"},
		{"-m=32", "dev", "arg1"},
		{"dev", "-m=32", "arg1"},
	} {
		_ = os.Remove("ran.txt")
		code, _, stderr := invoke(t, args...)
		got, err := os.ReadFile("ran.txt")
		if code != 0 || err != nil || string(got) != "scripts" {
			t.Errorf("%v: code %d, got %q, readErr %v, stderr %q", args, code, string(got), err, stderr)
		}
	}

	// Explicit relative path ./dev should not fall back to scripts/dev.luau.
	code, _, stderr := invoke(t, "./dev")
	if code != 2 || !strings.Contains(stderr, `unknown command "./dev"`) {
		t.Errorf("./dev: code %d, stderr %q", code, stderr)
	}

	// A script in the working directory takes precedence over scripts/.
	if err := os.WriteFile("dev.luau", []byte(`
		local fs = require("@std/fs")
		assert(select(1, ...) == "arg1")
		fs.writeFile("ran.txt", "cwd")
	`), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove("ran.txt")
	code, _, stderr = invoke(t, "dev", "arg1")
	got, err := os.ReadFile("ran.txt")
	if code != 0 || err != nil || string(got) != "cwd" {
		t.Errorf("cwd precedence: code %d, got %q, readErr %v, stderr %q", code, string(got), err, stderr)
	}
}
