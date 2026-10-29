package bcrypt

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Leoj030/go-luau/luau"
)

func TestHashAndCompare(t *testing.T) {
	state, err := luau.NewState()
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	if err := state.OpenRequire(luau.FSRequirer(fstest.MapFS{})); err != nil {
		t.Fatal(err)
	}
	if err := state.RegisterModule("@ceilo/bcrypt", Module()); err != nil {
		t.Fatal(err)
	}

	if err := state.DoString(`
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
	state, err := luau.NewState()
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	if err := state.OpenRequire(luau.FSRequirer(fstest.MapFS{})); err != nil {
		t.Fatal(err)
	}
	if err := state.RegisterModule("@ceilo/bcrypt", Module()); err != nil {
		t.Fatal(err)
	}

	err = state.DoString(`require("@ceilo/bcrypt").compare("password", "invalid")`)
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
