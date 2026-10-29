package net

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Leoj030/go-luau/luau"
)

func TestClientRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("X-Reply", "first")
		w.Header().Add("X-Reply", "second")

		switch r.URL.Path {
		case "/default":
			if r.Method != http.MethodGet {
				t.Errorf("default method = %q, want GET", r.Method)
			}
			_, _ = io.WriteString(w, "default response")
		case "/post":
			if r.Method != http.MethodPost {
				t.Errorf("request method = %q, want POST", r.Method)
			}
			if got := r.Header.Get("X-Request"); got != "custom" {
				t.Errorf("X-Request = %q, want custom", got)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("reading request body: %v", err)
			}
			if got := string(body); got != "payload" {
				t.Errorf("request body = %q, want payload", got)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, "created")
		case "/bad":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, "bad request")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	state, err := luau.NewState()
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	if err := state.OpenRequire(luau.FSRequirer(nil)); err != nil {
		t.Fatal(err)
	}
	if err := state.RegisterModule("@ceilo/net/client", ClientModule(context.Background())); err != nil {
		t.Fatal(err)
	}

	script := fmt.Sprintf(`
		local client = require("@ceilo/net/client")

		local defaultResponse = client.request(%q)
		assert(defaultResponse.status == 200)
		assert(defaultResponse.ok == true)
		assert(defaultResponse.body == "default response")
		assert(defaultResponse.headers["X-Reply"] == "first, second")

		local postResponse = client.request(%q, {
			method = "POST",
			body = "payload",
			headers = { ["X-Request"] = "custom" }
		})
		assert(postResponse.status == 201)
		assert(postResponse.ok == true)
		assert(postResponse.body == "created")
		assert(postResponse.headers["X-Reply"] == "first, second")

		local badResponse = client.request(%q, { method = "" })
		assert(badResponse.status == 400)
		assert(badResponse.ok == false)
		assert(badResponse.body == "bad request")
	`, server.URL+"/default", server.URL+"/post", server.URL+"/bad")
	if err := state.DoString(script); err != nil {
		t.Fatal(err)
	}
}

func TestClientRequestRejectsInvalidArguments(t *testing.T) {
	state, err := luau.NewState()
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	if err := state.OpenRequire(luau.FSRequirer(nil)); err != nil {
		t.Fatal(err)
	}
	if err := state.RegisterModule("@ceilo/net/client", ClientModule(context.Background())); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		call string
		want string
	}{
		{
			name: "URL must be a string",
			call: `client.request({})`,
			want: "net.client.request: argument 1 must be a URL string",
		},
		{
			name: "metadata must be a table",
			call: `client.request("http://127.0.0.1", "GET")`,
			want: "net.client.request: argument 2 must be a metadata table",
		},
		{
			name: "invalid URL",
			call: `client.request(":")`,
			want: "net.client.request: create request:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := state.DoString(`
				local client = require("@ceilo/net/client")
				` + tt.call)
			if err == nil {
				t.Fatal("request succeeded, want an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestClientRequestUsesContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	state, err := luau.NewState()
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	if err := state.OpenRequire(luau.FSRequirer(nil)); err != nil {
		t.Fatal(err)
	}
	if err := state.RegisterModule("@ceilo/net/client", ClientModule(ctx)); err != nil {
		t.Fatal(err)
	}

	err = state.DoString(`
		local client = require("@ceilo/net/client")
		client.request("http://127.0.0.1")
	`)
	if err == nil {
		t.Fatal("request succeeded with a canceled context")
	}
	if !strings.Contains(err.Error(), "net.client.request:") {
		t.Errorf("error = %q, want client request context error", err)
	}
}
