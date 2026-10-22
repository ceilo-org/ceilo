package json

import (
	stdjson "encoding/json"
	"reflect"
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
	if err := state.RegisterModule("@ceilo/json", Module()); err != nil {
		t.Fatal(err)
	}

	if err := state.DoString(`
		local json = require("@ceilo/json")
		local decoded = json.deserialize('{"name":"Luau","enabled":true,"count":3,"items":[1,"two",null],"meta":{"ok":false}}')
		local duplicate = json.deserialize('{"value":1,"value":2}')
		assert(decoded.name == "Luau")
		assert(decoded.enabled == true)
		assert(decoded.count == 3)
		assert(duplicate.value == 2)
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
		escaped = json.serialize({text = "<>&"})
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
	escaped, err := state.GetGlobal("escaped")
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"text":"\u003c\u003e\u0026"}`; escaped != want {
		t.Errorf("HTML-escaped JSON = %q, want %q", escaped, want)
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

func TestMarshalMatchesStandardJSON(t *testing.T) {
	values := []any{
		map[string]any{
			"html":    "<tag>&",
			"control": "line\n\tend",
			"unicode": "café\u2028\u2029",
			"invalid": string([]byte{0xff}),
			"nested":  []any{true, nil, float64(1e-7), float64(1e20), float64(1e21)},
		},
		[]any{},
		map[string]any{},
	}

	for _, value := range values {
		for _, pretty := range []bool{false, true} {
			got, err := marshal(value, pretty)
			if err != nil {
				t.Fatalf("marshal(%#v, %t): %v", value, pretty, err)
			}
			var want []byte
			if pretty {
				want, err = stdjson.MarshalIndent(value, "", "  ")
			} else {
				want, err = stdjson.Marshal(value)
			}
			if err != nil {
				t.Fatalf("standard marshal(%#v, %t): %v", value, pretty, err)
			}
			if string(got) != string(want) {
				t.Errorf("marshal(%#v, %t) = %q, want %q", value, pretty, got, want)
			}
		}
	}
}

func TestUnmarshalPreservesLegacyInvalidUTF8Behavior(t *testing.T) {
	value, err := unmarshal(string([]byte{'"', 0xff, '"'}))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := value, "\uFFFD"; got != want {
		t.Errorf("unmarshal invalid UTF-8 = %q, want %q", got, want)
	}
}

func TestDecodeToLuauMatchesStandardJSON(t *testing.T) {
	state, err := luau.NewState()
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()

	for _, input := range []string{
		`{"name":"Luau","enabled":true,"count":3,"nested":{"value":-125},"items":["one","two"]}`,
		`{"escaped":"quote\n\u2028\u2029","small":1e-7,"large":1e21}`,
		`{"duplicate":1,"duplicate":2}`,
		`{"invalid":"` + string([]byte{0xff}) + `"}`,
	} {
		if err := decodeToLuau(state, input); err != nil {
			t.Fatalf("decodeToLuau(%q): %v", input, err)
		}
		got, err := state.Get(-1)
		if err != nil {
			t.Fatal(err)
		}
		if err := state.Pop(1); err != nil {
			t.Fatal(err)
		}

		var want any
		if err := stdjson.Unmarshal([]byte(input), &want); err != nil {
			t.Fatalf("standard unmarshal(%q): %v", input, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("decodeToLuau(%q) = %#v, want %#v", input, got, want)
		}
	}
}

func TestDecodeToLuauRestoresStackAfterInvalidJSON(t *testing.T) {
	state, err := luau.NewState()
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()

	if err := state.Push("sentinel"); err != nil {
		t.Fatal(err)
	}
	if err := decodeToLuau(state, `{"nested":[1,`); err == nil {
		t.Fatal("decodeToLuau succeeded for invalid JSON")
	}
	if got, want := state.Top(), 1; got != want {
		t.Fatalf("stack height after failed decode = %d, want %d", got, want)
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
	if err := state.RegisterModule("@ceilo/json", Module()); err != nil {
		t.Fatal(err)
	}

	for _, script := range []string{
		`require("@ceilo/json").deserialize("{")`,
		`require("@ceilo/json").deserialize(123)`,
		`require("@ceilo/json").deserialize("{}","extra")`,
		`require("@ceilo/json").serialize({["key"] = function() end})`,
		`require("@ceilo/json").serialize({}, "extra")`,
		`require("@ceilo/json").serialize({}, 1)`,
	} {
		if err := state.DoString(script); err == nil {
			t.Errorf("DoString(%q) succeeded, want an error", script)
		}
	}
}
