// Package fs is the Go side of the "@std/fs" library: reading, writing
// and inspecting files and directories.
//
// Every function is synchronous: it blocks the whole runtime, including
// other tasks, until the operation finishes. Relative paths are relative to
// the process's working directory. Errors are raised as Luau errors, so
// scripts handle them with pcall.
package fs

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"

	"github.com/Leoj030/go-luau/luau"
)

// Module returns the functions of the library, ready for
// luau.State.RegisterModule.
func Module() map[string]luau.Func {
	return map[string]luau.Func{
		"readFile":        readfile,
		"writeFile":       writefile,
		"appendFile":      appendfile,
		"open":            open,
		"close":           closeFile,
		"read":            read,
		"write":           write,
		"exists":          exists,
		"stat":            stat,
		"type":            typeOf,
		"listdir":         listdir,
		"mkdir":           mkdir,
		"remove":          remove,
		"removeDirectory": removeDirectory,
		"move":            move,
		"copy":            copyFile,
		"link":            link,
		"symbolicLink":    symbolicLink,
		"walk":            walk,
		"watch":           watch,
	}
}

// ---- arguments and errors ----

// isOptionalArg checks the arg if it is an optional arg or not.
// It returns true if it is, otherwise false.
func isOptionalArg(c *luau.State, i int) bool {
	switch c.TypeName(i) {
	case "no value", "nil":
		return true
	}
	return false
}

func optionalBool(c *luau.State, fn string, i int) (bool, error) {
	if isOptionalArg(c, i) {
		return false, nil
	}
	return getArg[bool](c, fn, i)
}

// getArg returns argument i, which must be the type of T.
func getArg[T any](c *luau.State, fn string, i int) (T, error) {
	var zero T

	v, err := c.Get(i)
	val, ok := v.(T)
	if err != nil || !ok {
		return zero, fmt.Errorf("fs.%s: argument %d must be a %T", fn, i, zero)
	}

	return val, nil
}

// wrap prefixes an error with the function it came from.
func wrapErr(fn string, err error) error {
	return fmt.Errorf("fs.%s: %w", fn, err)
}

// ---- reading and writing ----

// fs.readFile(path: string): string
func readfile(c *luau.State) (int, error) {
	path, err := getArg[string](c, "readfile", 1)
	if err != nil {
		return 0, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, wrapErr("readfile", err)
	}
	return 1, c.Push(string(data))
}

// fs.writeFile(path: string, contents: string)
// Creates the file or replaces its contents.
func writefile(c *luau.State) (int, error) {
	path, err := getArg[string](c, "writefile", 1)
	if err != nil {
		return 0, err
	}
	data, err := getArg[string](c, "writefile", 2)
	if err != nil {
		return 0, err
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		return 0, wrapErr("writefile", err)
	}
	return 0, nil
}

// fs.appendFile(path: string, contents: string)
// Adds to the end of the file, creating it if needed.
func appendfile(c *luau.State) (int, error) {
	path, err := getArg[string](c, "appendfile", 1)
	if err != nil {
		return 0, err
	}
	data, err := getArg[string](c, "appendfile", 2)
	if err != nil {
		return 0, err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, wrapErr("appendfile", err)
	}
	_, writeErr := f.WriteString(data)
	closeErr := f.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return 0, wrapErr("appendfile", err)
	}
	return 0, nil
}

type fileHandle struct {
	file   *os.File
	closed bool
}

func (h *fileHandle) Finalize() {
	if !h.closed {
		_ = h.file.Close()
		h.closed = true
	}
}

func modeToFlags(mode string) (int, error) {
	switch mode {
	case "r":
		return os.O_RDONLY, nil
	case "w":
		return os.O_WRONLY | os.O_CREATE | os.O_TRUNC, nil
	case "x":
		return os.O_WRONLY | os.O_CREATE | os.O_EXCL, nil
	case "a":
		return os.O_WRONLY | os.O_CREATE | os.O_APPEND, nil
	case "r+":
		return os.O_RDWR, nil
	case "w+":
		return os.O_RDWR | os.O_CREATE | os.O_TRUNC, nil
	case "x+":
		return os.O_RDWR | os.O_CREATE | os.O_EXCL, nil
	case "a+":
		return os.O_RDWR | os.O_CREATE | os.O_APPEND, nil
	default:
		return 0, fmt.Errorf("invalid mode %q", mode)
	}
}

// fs.open(path: string, mode: string?): file
func open(c *luau.State) (int, error) {
	path, err := getArg[string](c, "open", 1)
	if err != nil {
		return 0, err
	}
	mode := "r"
	if !isOptionalArg(c, 2) {
		mode, err = getArg[string](c, "open", 2)
		if err != nil {
			return 0, err
		}
	}
	flags, err := modeToFlags(mode)
	if err != nil {
		return 0, wrapErr("open", err)
	}
	f, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		return 0, wrapErr("open", err)
	}
	if err := c.PushUserdata(&fileHandle{file: f}); err != nil {
		_ = f.Close()
		return 0, err
	}
	return 1, nil
}

func getFileHandle(c *luau.State, fn string) (*fileHandle, error) {
	v, ok := c.ToUserdata(1)
	h, valid := v.(*fileHandle)
	if !ok || !valid {
		return nil, fmt.Errorf("fs.%s: argument 1 must be a file handle", fn)
	}
	if h.closed {
		return nil, fmt.Errorf("fs.%s: file is closed", fn)
	}
	return h, nil
}

func closeFile(c *luau.State) (int, error) {
	v, ok := c.ToUserdata(1)
	h, valid := v.(*fileHandle)
	if !ok || !valid {
		return 0, errors.New("fs.close: argument 1 must be a file handle")
	}
	if h.closed {
		return 0, errors.New("fs.close: file is already closed")
	}
	if err := h.file.Close(); err != nil {
		h.closed = true
		return 0, wrapErr("close", err)
	}
	h.closed = true
	return 0, nil
}

func read(c *luau.State) (int, error) {
	h, err := getFileHandle(c, "read")
	if err != nil {
		return 0, err
	}
	data, err := io.ReadAll(h.file)
	if err != nil {
		return 0, wrapErr("read", err)
	}
	return 1, c.Push(string(data))
}

func write(c *luau.State) (int, error) {
	h, err := getFileHandle(c, "write")
	if err != nil {
		return 0, err
	}
	data, err := getArg[string](c, "write", 2)
	if err != nil {
		return 0, err
	}
	if _, err := h.file.WriteString(data); err != nil {
		return 0, wrapErr("write", err)
	}
	return 0, nil
}

// ---- inspecting ----

// fs.exists(path: string): boolean
// False means the path is not there. Other failures, such as a permission
// problem, are raised.
func exists(c *luau.State) (int, error) {
	path, err := getArg[string](c, "exists", 1)
	if err != nil {
		return 0, err
	}
	_, err = os.Stat(path)
	switch {
	case err == nil:
		return 1, c.Push(true)
	case errors.Is(err, os.ErrNotExist):
		return 1, c.Push(false)
	}
	return 0, wrapErr("exists", err)
}

// Stat is what fs.stat returns. The luau tags name the Luau fields.
type Stat struct {
	Type     string  `luau:"type"`     // "file", "directory" or "other"
	Size     int64   `luau:"size"`     // bytes
	Modified float64 `luau:"modified"` // seconds since the Unix epoch
}

// fs.type(path: string): string
// Does not follow symbolic links.
func typeOf(c *luau.State) (int, error) {
	path, err := getArg[string](c, "type", 1)
	if err != nil {
		return 0, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return 0, wrapErr("type", err)
	}
	t := kind(info.Mode())
	if info.Mode()&os.ModeSymlink != 0 {
		t = "symlink"
	}
	return 1, c.Push(t)
}

// fs.stat(path: string): { type: string, size: number, modified: number }
// Follows symbolic links.
func stat(c *luau.State) (int, error) {
	path, err := getArg[string](c, "stat", 1)
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, wrapErr("stat", err)
	}
	t := info.ModTime()
	return 1, c.Push(Stat{
		Type:     kind(info.Mode()),
		Size:     info.Size(),
		Modified: float64(t.Unix()) + float64(t.Nanosecond())/1e9,
	})
}

// Entry is one item of an fs.listdir result.
type Entry struct {
	Name string `luau:"name"`
	Type string `luau:"type"` // "file", "directory", "symlink" or "other"
}

// fs.listdir(path: string): { { name: string, type: string } }
// Entries come sorted by name.
func listdir(c *luau.State) (int, error) {
	path, err := getArg[string](c, "listdir", 1)
	if err != nil {
		return 0, err
	}
	items, err := os.ReadDir(path)
	if err != nil {
		return 0, wrapErr("listdir", err)
	}
	entries := make([]Entry, 0, len(items)) // never nil, so an empty directory is an empty table
	for _, it := range items {
		t := kind(it.Type())
		if it.Type()&os.ModeSymlink != 0 {
			t = "symlink"
		}
		entries = append(entries, Entry{Name: it.Name(), Type: t})
	}
	return 1, c.Push(entries)
}

func kind(m os.FileMode) string {
	switch {
	case m.IsDir():
		return "directory"
	case m.IsRegular():
		return "file"
	}
	return "other"
}

// ---- changing the file system ----

// fs.mkdir(path: string, recursive: boolean?)
// Without recursive it fails if the parent is missing or the path exists.
// With it, missing parents are created and an existing directory is fine.
func mkdir(c *luau.State) (int, error) {
	path, err := getArg[string](c, "mkdir", 1)
	if err != nil {
		return 0, err
	}

	recursive, err := optionalBool(c, "mkdir", 2)
	if err != nil {
		return 0, err
	}
	if recursive {
		err = os.MkdirAll(path, 0o755)
	} else {
		err = os.Mkdir(path, 0o755)
	}

	if err != nil {
		return 0, wrapErr("mkdir", err)
	}
	return 0, nil
}

// fs.remove(path: string, recursive: boolean?)
// Removes a file or an empty directory. With recursive it also removes a
// directory and everything inside it. A missing path is an error either way.
func remove(c *luau.State) (int, error) {
	path, err := getArg[string](c, "remove", 1)
	if err != nil {
		return 0, err
	}

	recursive, err := optionalBool(c, "remove", 2)
	if err != nil {
		return 0, err
	}
	if recursive {
		if _, err := os.Lstat(path); err != nil {
			return 0, wrapErr("remove", err)
		}
		err = os.RemoveAll(path)
	} else {
		err = os.Remove(path)
	}
	if err != nil {
		return 0, wrapErr("remove", err)
	}
	return 0, nil
}

func removeDirectory(c *luau.State) (int, error) {
	path, err := getArg[string](c, "removeDirectory", 1)
	if err != nil {
		return 0, err
	}
	recursive, err := optionalBool(c, "removeDirectory", 2)
	if err != nil {
		return 0, err
	}
	if recursive {
		if _, err := os.Lstat(path); err != nil {
			return 0, wrapErr("removeDirectory", err)
		}
		err = os.RemoveAll(path)
	} else {
		err = os.Remove(path)
	}
	if err != nil {
		return 0, wrapErr("removeDirectory", err)
	}
	return 0, nil
}

func move(c *luau.State) (int, error) {
	src, err := getArg[string](c, "move", 1)
	if err != nil {
		return 0, err
	}
	dst, err := getArg[string](c, "move", 2)
	if err != nil {
		return 0, err
	}
	if err := os.Rename(src, dst); err != nil {
		return 0, wrapErr("move", err)
	}
	return 0, nil
}

func copyFile(c *luau.State) (int, error) {
	src, err := getArg[string](c, "copy", 1)
	if err != nil {
		return 0, err
	}
	dst, err := getArg[string](c, "copy", 2)
	if err != nil {
		return 0, err
	}
	in, err := os.Open(src)
	if err != nil {
		return 0, wrapErr("copy", err)
	}
	sourceInfo, err := in.Stat()
	if err != nil {
		_ = in.Close()
		return 0, wrapErr("copy", err)
	}
	if destinationInfo, statErr := os.Stat(dst); statErr == nil {
		if os.SameFile(sourceInfo, destinationInfo) {
			_ = in.Close()
			return 0, wrapErr("copy", errors.New("source and destination are the same file"))
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		_ = in.Close()
		return 0, wrapErr("copy", statErr)
	}
	out, err := os.Create(dst)
	if err != nil {
		_ = in.Close()
		return 0, wrapErr("copy", err)
	}
	_, copyErr := io.Copy(out, in)
	inputCloseErr := in.Close()
	outputCloseErr := out.Close()
	if err := errors.Join(copyErr, inputCloseErr, outputCloseErr); err != nil {
		return 0, wrapErr("copy", err)
	}
	return 0, nil
}

func link(c *luau.State) (int, error) {
	oldPath, err := getArg[string](c, "link", 1)
	if err != nil {
		return 0, err
	}
	newPath, err := getArg[string](c, "link", 2)
	if err != nil {
		return 0, err
	}
	if err := os.Link(oldPath, newPath); err != nil {
		return 0, wrapErr("link", err)
	}
	return 0, nil
}

func symbolicLink(c *luau.State) (int, error) {
	target, err := getArg[string](c, "symbolicLink", 1)
	if err != nil {
		return 0, err
	}
	linkPath, err := getArg[string](c, "symbolicLink", 2)
	if err != nil {
		return 0, err
	}
	if err := os.Symlink(target, linkPath); err != nil {
		return 0, wrapErr("symbolicLink", err)
	}
	return 0, nil
}

func walk(c *luau.State) (int, error) {
	root, err := getArg[string](c, "walk", 1)
	if err != nil {
		return 0, err
	}
	if c.TypeName(2) != "function" {
		return 0, errors.New("fs.walk: argument 2 must be a function")
	}
	callback, err := c.Ref(2)
	if err != nil {
		return 0, err
	}
	defer c.Unref(callback)

	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if err := c.PushRef(callback); err != nil {
			return err
		}
		var item any
		if entry != nil {
			t := kind(entry.Type())
			if entry.Type()&os.ModeSymlink != 0 {
				t = "symlink"
			}
			item = Entry{Name: entry.Name(), Type: t}
		}
		var callbackErr any
		if walkErr != nil {
			callbackErr = walkErr.Error()
		}
		results, err := c.Call(path, item, callbackErr)
		if err != nil {
			return err
		}
		if len(results) > 0 {
			if keepGoing, ok := results[0].(bool); ok && !keepGoing {
				return filepath.SkipAll
			}
		}
		return nil
	})
	if err != nil {
		return 0, wrapErr("walk", err)
	}
	return 0, nil
}

type WatchEvent struct {
	Name  string `luau:"name"`
	Op    string `luau:"op"`
	Error string `luau:"error,omitempty"`
}

func watch(c *luau.State) (n int, retErr error) {
	path, err := getArg[string](c, "watch", 1)
	if err != nil {
		return 0, err
	}
	if c.TypeName(2) != "function" {
		return 0, errors.New("fs.watch: argument 2 must be a function")
	}
	callback, err := c.Ref(2)
	if err != nil {
		return 0, err
	}
	defer c.Unref(callback)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return 0, wrapErr("watch", err)
	}
	defer func() {
		if err := watcher.Close(); err != nil {
			retErr = errors.Join(retErr, wrapErr("watch", err))
		}
	}()
	if err := watcher.Add(path); err != nil {
		return 0, wrapErr("watch", err)
	}
	for {
		var event WatchEvent
		select {
		case e, ok := <-watcher.Events:
			if !ok {
				return 0, errors.New("fs.watch: watcher event stream closed")
			}
			event = WatchEvent{Name: e.Name, Op: e.Op.String()}
		case err, ok := <-watcher.Errors:
			if !ok {
				return 0, errors.New("fs.watch: watcher error stream closed")
			}
			event = WatchEvent{Error: err.Error()}
		}
		if err := c.PushRef(callback); err != nil {
			return 0, err
		}
		results, err := c.Call(event)
		if err != nil {
			return 0, wrapErr("watch", err)
		}
		if len(results) > 0 {
			if keepWatching, ok := results[0].(bool); ok && !keepWatching {
				return 0, nil
			}
		}
	}
}
