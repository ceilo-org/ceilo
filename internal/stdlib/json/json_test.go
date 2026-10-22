package json

import (
	"testing"
	"testing/fstest"

	"github.com/Leoj030/go-luau/luau"
)

func TestDeserializeAndSerialize(t *testing.T) {
	state, err := luau.NewState()
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	if err := state.OpenRequire(luau.FSRequirer(fstest.MapFS{})); err != nil {
		t.Fatal(err)
	}
	if err := state.RegisterModule("@std/json", Module()); err != nil {
		t.Fatal(err)
	}

	if err := state.DoString(`
		local json = require("@std/json")
		local decoded = json.deserialize('{"name":"Luau","enabled":true,"count":3,"items":[1,"two",null],"meta":{"ok":false}}')
		assert(decoded.name == "Luau")
		assert(decoded.enabled == true)
		assert(decoded.count == 3)
		assert(decoded.items[1] == 1)
		assert(decoded.items[2] == "two")
		assert(decoded.items[3] == nil)
		assert(decoded.meta.ok == false)

		encoded = json.serialize({
			name = decoded.name,
			enabled = decoded.enabled,
			count = decoded.count,
			items = {1, "two"},
			meta = {ok = decoded.meta.ok},
		})
		array = json.serialize({1, "two", false})
		pretty = json.serialize({name = "Luau", enabled = true}, true)
		compactWithFalse = json.serialize({name = "Luau", enabled = true}, false)
		compactByDefault = json.serialize({name = "Luau", enabled = true})
	`); err != nil {
		t.Fatal(err)
	}

	encoded, err := state.GetGlobal("encoded")
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"count":3,"enabled":true,"items":[1,"two"],"meta":{"ok":false},"name":"Luau"}`; encoded != want {
		t.Errorf("encoded JSON = %q, want %q", encoded, want)
	}
	array, err := state.GetGlobal("array")
	if err != nil {
		t.Fatal(err)
	}
	if want := `[1,"two",false]`; array != want {
		t.Errorf("encoded array = %q, want %q", array, want)
	}
	pretty, err := state.GetGlobal("pretty")
	if err != nil {
		t.Fatal(err)
	}
	if want := "{\n  \"enabled\": true,\n  \"name\": \"Luau\"\n}"; pretty != want {
		t.Errorf("pretty JSON = %q, want %q", pretty, want)
	}
	for _, name := range []string{"compactWithFalse", "compactByDefault"} {
		compact, err := state.GetGlobal(name)
		if err != nil {
			t.Fatal(err)
		}
		if want := `{"enabled":true,"name":"Luau"}`; compact != want {
			t.Errorf("%s = %q, want %q", name, compact, want)
		}
	}
}

func TestDeserializeAndSerializeRejectInvalidArguments(t *testing.T) {
	state, err := luau.NewState()
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	if err := state.OpenRequire(luau.FSRequirer(fstest.MapFS{})); err != nil {
		t.Fatal(err)
	}
	if err := state.RegisterModule("@std/json", Module()); err != nil {
		t.Fatal(err)
	}

	for _, script := range []string{
		`require("@std/json").deserialize("{")`,
		`require("@std/json").deserialize(123)`,
		`require("@std/json").deserialize("{}","extra")`,
		`require("@std/json").serialize({["key"] = function() end})`,
		`require("@std/json").serialize({}, "extra")`,
		`require("@std/json").serialize({}, 1)`,
	} {
		if err := state.DoString(script); err == nil {
			t.Errorf("DoString(%q) succeeded, want an error", script)
		}
	}
}
