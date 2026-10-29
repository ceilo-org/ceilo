package main

import (
	"bytes"
	"context"
	"testing"
)

func TestWriteInterruptNewline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var output bytes.Buffer
	if err := writeInterruptNewline(ctx, &output); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestWriteInterruptNewlineDoesNothingWithoutCancellation(t *testing.T) {
	var output bytes.Buffer
	if err := writeInterruptNewline(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != "" {
		t.Errorf("output = %q, want empty", got)
	}
}
