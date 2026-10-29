package fs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Leoj030/go-luau/luau"
	stdtask "github.com/ceilo-org/ceilo/internal/stdlib/task"
)

func TestModeToFlags(t *testing.T) {
	tests := map[string]int{
		"r":  os.O_RDONLY,
		"w":  os.O_WRONLY | os.O_CREATE | os.O_TRUNC,
		"x":  os.O_WRONLY | os.O_CREATE | os.O_EXCL,
		"a":  os.O_WRONLY | os.O_CREATE | os.O_APPEND,
		"r+": os.O_RDWR,
		"w+": os.O_RDWR | os.O_CREATE | os.O_TRUNC,
		"x+": os.O_RDWR | os.O_CREATE | os.O_EXCL,
		"a+": os.O_RDWR | os.O_CREATE | os.O_APPEND,
	}
	for mode, want := range tests {
		got, err := modeToFlags(mode)
		if err != nil || got != want {
			t.Errorf("modeToFlags(%q) = %d, %v; want %d, nil", mode, got, err, want)
		}
	}
	if _, err := modeToFlags("invalid"); err == nil {
		t.Fatal("modeToFlags accepted an invalid mode")
	}
}

func TestModuleFileAndDirectoryOperations(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file.txt")
	copy := filepath.Join(dir, "copy.txt")
	moved := filepath.Join(dir, "moved.txt")
	hardlink := filepath.Join(dir, "hardlink.txt")
	symlink := filepath.Join(dir, "symlink.txt")
	nested := filepath.Join(dir, "nested", "child")
	nestedRoot := filepath.Join(dir, "nested")

	state, err := luau.NewState()
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	if err := state.OpenRequire(luau.FSRequirer(fstest.MapFS{})); err != nil {
		t.Fatal(err)
	}
	if err := state.RegisterModule("@std/fs", Module()); err != nil {
		t.Fatal(err)
	}

	script := `
		local fs = require("@std/fs")
		local dir, file, copy, moved, hardlink, symlink, nested, nestedRoot =
			%s, %s, %s, %s, %s, %s, %s, %s
		fs.mkdir(nested, true)
		fs.writeFile(file, "one")
		fs.appendFile(file, " two")
		local handle = fs.open(file)
		readContents = fs.read(handle)
		fs.close(handle)
		local writable = fs.open(file, "w+")
		fs.write(writable, "updated")
		fs.close(writable)
		fs.copy(file, copy)
		sameCopySucceeded = pcall(fs.copy, file, file)
		fs.link(file, hardlink)
		fs.symbolicLink(file, symlink)
		fileType = fs.type(file)
		linkType = fs.type(symlink)
		fileSize = fs.stat(file).size
		missingExists = fs.exists(file .. ".missing")
		local walked = 0
		fs.walk(dir, function(path, entry, walkError)
			assert(walkError == nil)
			assert(entry ~= nil)
			walked += 1
			return true
		end)
		walkCount = walked
		fs.move(copy, moved)
		fs.removeDirectory(nestedRoot, true)
		fs.remove(symlink)
	`
	script = fmt.Sprintf(script, luaString(dir), luaString(file), luaString(copy),
		luaString(moved), luaString(hardlink), luaString(symlink), luaString(nested),
		luaString(nestedRoot))
	if err := state.DoString(script); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]any{
		"readContents":      "one two",
		"fileType":          "file",
		"linkType":          "symlink",
		"fileSize":          7.0,
		"missingExists":     false,
		"sameCopySucceeded": false,
	} {
		got, err := state.GetGlobal(name)
		if err != nil || got != want {
			t.Errorf("%s = %v, %v; want %v", name, got, err, want)
		}
	}
	walked, err := state.GetGlobal("walkCount")
	count, ok := walked.(float64)
	if err != nil || !ok || count < 7 {
		t.Errorf("walkCount = %v, %v; want at least 7", walked, err)
	}
	for _, path := range []string{moved, hardlink} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "updated" {
			t.Errorf("contents of %s = %q, %v; want updated", path, data, err)
		}
	}
	data, err := os.ReadFile(file)
	if err != nil || string(data) != "updated" {
		t.Errorf("contents of source after self-copy = %q, %v; want updated", data, err)
	}
	for _, path := range []string{copy, symlink, nestedRoot} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Errorf("%s still exists or could not be checked: %v", path, err)
		}
	}
}

func TestTypedefCompiles(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "..", "typedefs", "std", "fs.luau"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := luau.Compile(string(content)); err != nil {
		t.Fatalf("filesystem typedef does not compile: %v", err)
	}
}

func TestWatchCallsBackUntilStopped(t *testing.T) {
	dir := t.TempDir()
	state, err := luau.NewState()
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	if err := state.OpenRequire(luau.FSRequirer(fstest.MapFS{})); err != nil {
		t.Fatal(err)
	}
	scheduler := stdtask.New(stdtask.Options{OnError: func(err error) {
		t.Errorf("watch callback: %v", err)
	}})
	scheduler.BindState(state)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := state.RegisterModule("@std/fs", ModuleWithContext(ctx, scheduler)); err != nil {
		t.Fatal(err)
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = os.WriteFile(filepath.Join(dir, "event.txt"), []byte("event"), 0o644)
	}()

	script := fmt.Sprintf(`
		local fs = require("@std/fs")
		local watcher
		watcher = fs.watch(%s, function(filename, event)
			assert(type(filename) == "string")
			assert(filename == %s)
			assert(type(event) == "table")
			assert(event.change == true)
			watchCalled = true
			watcher:close()
		end)
		watchReturnedHandle = type(watcher.close) == "function"
	`, luaString(dir), luaString(filepath.Join(dir, "event.txt")))
	bytecode, err := luau.Compile(script)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := state.NewThread()
	if err != nil {
		t.Fatal(err)
	}
	defer thread.Close()
	if err := thread.Load(bytecode, "=fs-watch-test"); err != nil {
		t.Fatal(err)
	}
	scheduler.Start(thread, 0)
	done := make(chan bool, 1)
	go func() { done <- scheduler.Run() }()
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("scheduler reported failure")
		}
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("watch callback was not delivered and closed")
	}
	if got, err := state.GetGlobal("watchCalled"); err != nil || got != true {
		t.Errorf("watchCalled = %v, %v; want true", got, err)
	}
	if got, err := state.GetGlobal("watchReturnedHandle"); err != nil || got != true {
		t.Errorf("watchReturnedHandle = %v, %v; want true", got, err)
	}
}

func luaString(s string) string {
	return strconv.Quote(s)
}
