package engine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/Leoj030/go-luau/luau"
)

// diskRequirer serves modules from the real file system, using Luau's own
// require rules (relative paths, init files, .luaurc aliases).
// It also forwards completed yielding modules to the runtime scheduler.
//
// luau.FSRequirer reads chunk names as paths inside an fs.FS, and it can
// never leave that FS. Rooting the FS at the top of the file system lets
// scripts require files anywhere, and this wrapper keeps the names in error
// messages short: they are shown relative to the working directory, while
// the FSRequirer underneath sees paths relative to the root.
type diskRequirer struct {
	luau.Requirer    // FSRequirer(os.DirFS(root))
	root, cwd        string
	onModuleFinished func(*luau.Thread, error)
}

func NewDiskRequirer(onModuleFinished func(*luau.Thread, error)) (*diskRequirer, error) {
	if onModuleFinished == nil {
		return nil, errors.New("disk requirer requires a module completion handler")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	// "/" on Unix, the current drive on Windows. Other drives are out of reach.
	root := filepath.VolumeName(cwd) + string(filepath.Separator)
	return &diskRequirer{
		Requirer:         luau.FSRequirer(os.DirFS(root)),
		root:             root,
		cwd:              cwd,
		onModuleFinished: onModuleFinished,
	}, nil
}

// chunkname returns the name a script at path gets: "@" plus the path,
// relative to the working directory when it lies inside it.
func (d *diskRequirer) Chunkname(path string) string {
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(d.cwd, abs)
	}
	abs = filepath.Clean(abs)
	if rel, err := filepath.Rel(d.cwd, abs); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "@" + filepath.ToSlash(rel)
	}
	return "@" + filepath.ToSlash(abs)
}

// toFS turns one of our chunk names into the path-in-root the FSRequirer
// expects.
func (d *diskRequirer) toFS(chunkname string) string {
	p, ok := strings.CutPrefix(chunkname, "@")
	if !ok {
		return chunkname // "=name": a chunk without a file
	}
	abs := filepath.FromSlash(p)
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(d.cwd, abs)
	}
	rel, err := filepath.Rel(d.root, abs)
	if err != nil {
		return chunkname
	}
	return "@" + filepath.ToSlash(rel)
}

// fromFS is the reverse of toFS.
func (d *diskRequirer) fromFS(chunkname string) string {
	p, ok := strings.CutPrefix(chunkname, "@")
	if !ok {
		return chunkname
	}
	return d.Chunkname(filepath.Join(d.root, filepath.FromSlash(p)))
}

func (d *diskRequirer) AllowRequire(chunkname string) bool {
	return d.Requirer.AllowRequire(d.toFS(chunkname))
}

func (d *diskRequirer) Reset(chunkname string) luau.NavResult {
	return d.Requirer.Reset(d.toFS(chunkname))
}

func (d *diskRequirer) Module() (luau.ModuleInfo, error) {
	info, err := d.Requirer.Module()
	if err == nil {
		info.Chunkname = d.fromFS(info.Chunkname)
	}
	return info, err
}

func (d *diskRequirer) ModuleFinished(waiter *luau.Thread, err error) {
	d.onModuleFinished(waiter, err)
}
