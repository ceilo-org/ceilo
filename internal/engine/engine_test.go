package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Leoj030/go-luau/luau"
)

func TestRunFileReturnsContextCanceledAfterServerShutdown(t *testing.T) {
	script := filepath.Join(t.TempDir(), "server.luau")
	source := `
		local server = require("@ceilo/net/server")
		server({
			port = 0,
			handler = function()
				return "ok"
			end,
			maxbodysize = 64
		})
	`
	if err := os.WriteFile(script, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	timer := time.AfterFunc(20*time.Millisecond, cancel)
	defer func() {
		timer.Stop()
		cancel()
	}()

	err := RunFile(ctx, Options{Script: script})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunFile error = %v; want context.Canceled", err)
	}
}

func TestRunFileInterruptsInfiniteLoop(t *testing.T) {
	script := filepath.Join(t.TempDir(), "infinite.luau")
	if err := os.WriteFile(script, []byte("while true do end"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- RunFile(ctx, Options{Script: script})
	}()

	timer := time.AfterFunc(20*time.Millisecond, cancel)
	defer func() {
		timer.Stop()
		cancel()
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("RunFile error = %v; want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunFile did not stop after context cancellation")
	}
}

func TestRunFileInterruptsFileWatch(t *testing.T) {
	watchedPath := t.TempDir()
	script := filepath.Join(t.TempDir(), "watch.luau")
	source := fmt.Sprintf(`
		local fs = require("@std/fs")
		fs.watch(%q, function() end)
	`, watchedPath)
	if err := os.WriteFile(script, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- RunFile(ctx, Options{Script: script})
	}()

	timer := time.AfterFunc(20*time.Millisecond, cancel)
	defer func() {
		timer.Stop()
		cancel()
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("RunFile error = %v; want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunFile did not stop after context cancellation while watching")
	}
}

func TestRunFileMakesNetRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("X-Reply", "first")
		w.Header().Add("X-Reply", "second")
		if r.Method == http.MethodPost {
			if got := r.Header.Get("X-Request"); got != "custom" {
				t.Errorf("X-Request header = %q, want custom", got)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read request body: %v", err)
			}
			if got := string(body); got != "payload" {
				t.Errorf("request body = %q, want payload", got)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, "created")
			return
		}
		if r.Method != http.MethodGet {
			t.Errorf("default request method = %q, want GET", r.Method)
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "bad request")
	}))
	defer server.Close()

	script := filepath.Join(t.TempDir(), "client.luau")
	source := fmt.Sprintf(`
		local net = require("@ceilo/net")
		local client = require("@ceilo/net/client")

		local defaultResponse = client.request(%q)
		assert(defaultResponse.status == 400)
		assert(defaultResponse.ok == false)
		assert(defaultResponse.body == "bad request")
		assert(defaultResponse.headers["X-Reply"] == "first, second")

		local postResponse = net.client.request(%q, {
			method = "POST",
			body = "payload",
			headers = { ["X-Request"] = "custom" }
		})
		assert(postResponse.status == 201)
		assert(postResponse.ok == true)
		assert(postResponse.body == "created")
		assert(postResponse.headers["X-Reply"] == "first, second")
	`, server.URL, server.URL)
	if err := os.WriteFile(script, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := RunFile(context.Background(), Options{Script: script}); err != nil {
		t.Fatalf("RunFile error = %v", err)
	}
}

func TestRunFileServesRequestsWithNet(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	script := filepath.Join(t.TempDir(), "server2.luau")
	source := fmt.Sprintf(`
		local net = require("@ceilo/net")
		net.server({
			hostname = "127.0.0.1",
			port = %d,
			handler = function(req, server)
				return {
					status = 201,
					body = req.method .. " " .. req.path .. " " .. req.query.name ..
						" " .. tostring(server.port) .. " " .. type(server.close)
				}
			end
		})
	`, port)
	if err := os.WriteFile(script, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- RunFile(ctx, Options{Script: script})
	}()

	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(5 * time.Second)
	var resp *http.Response
	for time.Now().Before(deadline) {
		resp, err = client.Get(fmt.Sprintf("http://127.0.0.1:%d/hello?name=Luau", port))
		if err == nil {
			break
		}
		select {
		case runErr := <-done:
			t.Fatalf("RunFile returned before serving a request: %v", runErr)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("request to net2 server failed: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	if got, want := string(body), fmt.Sprintf("GET /hello Luau %d function", port); got != want {
		t.Errorf("body = %q, want %q", got, want)
	}

	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("RunFile error = %v; want context.Canceled", err)
	}
}

func TestDiskRequirerAliasFallback(t *testing.T) {
	dir := t.TempDir()
	write := func(name, src string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("main.luau", `return 1`)
	write(".ceilo/.luaurc", `{"aliases": {"std": "./std"}}`)
	write(".ceilo/std/net.luau", `return "net"`)

	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })

	d, err := NewDiskRequirer(func(*luau.Thread, error) {})
	if err != nil {
		t.Fatal(err)
	}

	if got := d.Reset("@main.luau"); got != luau.NavSuccess {
		t.Fatalf("Reset = %v, want NavSuccess", got)
	}
	if got := d.ToAliasFallback("std"); got != luau.NavSuccess {
		t.Fatalf("ToAliasFallback(std) = %v, want NavSuccess", got)
	}
	if got := d.ToChild("net"); got != luau.NavSuccess {
		t.Fatalf("ToChild(net) = %v, want NavSuccess", got)
	}
	if !d.ModulePresent() {
		t.Fatal("expected a module under the cursor")
	}
	info, err := d.Module()
	if err != nil {
		t.Fatal(err)
	}
	if want := "@.ceilo/std/net.luau"; info.Chunkname != want {
		t.Errorf("Chunkname = %q, want %q", info.Chunkname, want)
	}
}

func TestDiskRequirerUnknownAliasStaysUnknown(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(`return 1`), 0o644); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })

	d, err := NewDiskRequirer(func(*luau.Thread, error) {})
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Reset("@main.luau"); got != luau.NavSuccess {
		t.Fatalf("Reset = %v, want NavSuccess", got)
	}
	if got := d.ToAliasFallback("nope"); got != luau.NavNotFound {
		t.Errorf("ToAliasFallback(nope) = %v, want NavNotFound", got)
	}
}
