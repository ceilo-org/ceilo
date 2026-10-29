package net

import (
	"bufio"
	"io"
	"net"
	"strings"
	"testing"
)

func TestReadRequest(t *testing.T) {
	raw := "POST /hello%20world?name=Luau&name=ignored HTTP/1.1\r\n" +
		"Host: localhost\r\n" +
		"X-Tag: first\r\n" +
		"X-Tag: second\r\n" +
		"Content-Length: 4\r\n\r\nbody"
	req, keepAlive, status, err := readRequest(bufio.NewReader(strings.NewReader(raw)))
	if err != nil {
		t.Fatalf("readRequest() error = %v", err)
	}
	if status != 0 {
		t.Fatalf("readRequest() status = %d, want 0", status)
	}
	if !keepAlive {
		t.Fatal("readRequest() keepAlive = false, want true")
	}
	if req.Method != "POST" || req.Path != "/hello world" || req.Body != "body" {
		t.Errorf("request = method %q, path %q, body %q", req.Method, req.Path, req.Body)
	}
	if got := req.Query["name"]; got != "Luau" {
		t.Errorf("query[name] = %q, want Luau", got)
	}
	if got := req.Headers["x-tag"]; got != "first, second" {
		t.Errorf("headers[x-tag] = %q, want %q", got, "first, second")
	}
}

func TestReadRequestRejectsOversizedBody(t *testing.T) {
	raw := "POST / HTTP/1.1\r\nHost: localhost\r\nContent-Length: 10485761\r\n\r\n"
	_, _, status, err := readRequest(bufio.NewReader(strings.NewReader(raw)))
	if err == nil {
		t.Fatal("readRequest() error = nil, want oversized-body error")
	}
	if status != 413 {
		t.Errorf("readRequest() status = %d, want 413", status)
	}
}

func TestReadRequestUsesConfiguredBodyLimit(t *testing.T) {
	raw := "POST / HTTP/1.1\r\nHost: localhost\r\nContent-Length: 4\r\n\r\nbody"
	_, _, status, err := readRequestLimit(bufio.NewReader(strings.NewReader(raw)), 3)
	if err == nil {
		t.Fatal("readRequestLimit() error = nil, want oversized-body error")
	}
	if status != 413 {
		t.Errorf("readRequestLimit() status = %d, want 413", status)
	}
}

func TestReadRequestRejectsExpectContinue(t *testing.T) {
	raw := "POST / HTTP/1.1\r\nHost: localhost\r\nContent-Length: 4\r\nExpect: 100-continue\r\n\r\n"
	_, _, status, err := readRequest(bufio.NewReader(strings.NewReader(raw)))
	if err == nil {
		t.Fatal("readRequest() error = nil, want unsupported-expectation error")
	}
	if status != 417 {
		t.Errorf("readRequest() status = %d, want 417", status)
	}
}

func TestWriteResponse(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- writeResponse(serverConn, response{
			status:  201,
			headers: map[string]string{"Content-Type": "text/plain"},
			body:    "created",
		}, false, false)
		_ = serverConn.Close()
	}()

	raw, err := io.ReadAll(clientConn)
	_ = clientConn.Close()
	if err != nil {
		t.Fatalf("reading response: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("writeResponse() error = %v", err)
	}
	responseText := string(raw)
	for _, want := range []string{
		"HTTP/1.1 201 Created\r\n",
		"Content-Type: text/plain\r\n",
		"Content-Length: 7\r\n",
		"Connection: close\r\n",
		"\r\ncreated",
	} {
		if !strings.Contains(responseText, want) {
			t.Errorf("response %q does not contain %q", responseText, want)
		}
	}
}
