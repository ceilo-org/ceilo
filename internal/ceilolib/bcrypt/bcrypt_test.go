package bcrypt

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Leoj030/go-luau/luau"
	"github.com/Leoj030/go-luau/vm"
)

func TestHashAndCompare(t *testing.T) {
	v, err := vm.New()
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	if err := v.State().OpenRequire(luau.FSRequirer(fstest.MapFS{})); err != nil {
		t.Fatal(err)
	}
	if err := v.Module("@ceilo/bcrypt", Module()); err != nil {
		t.Fatal(err)
	}
	if err := v.Do(`
		local bcrypt = require("@ceilo/bcrypt")
		local hash = bcrypt.hash("secure123", 4)
		assert(type(hash) == "string")
		assert(bcrypt.compare("secure123", hash))
		assert(not bcrypt.compare("wrong", hash))
	`); err != nil {
		t.Fatal(err)
	}
}

func TestCompareInvalidHashUsesModuleError(t *testing.T) {
	v, err := vm.New()
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	if err := v.State().OpenRequire(luau.FSRequirer(fstest.MapFS{})); err != nil {
		t.Fatal(err)
	}
	if err := v.Module("@ceilo/bcrypt", Module()); err != nil {
		t.Fatal(err)
	}
	err = v.Do(`require("@ceilo/bcrypt").compare("password", "invalid")`)
	if err == nil {
		t.Fatal("compare succeeded with an invalid hash")
	}
	if !strings.Contains(err.Error(), "ceilo/bcrypt.compare: invalid hashed password") {
		t.Errorf("compare error = %q, want module-specific error", err)
	}
	if strings.Contains(err.Error(), "crypto/bcrypt") {
		t.Errorf("compare error exposed Go package name: %q", err)
	}
}
