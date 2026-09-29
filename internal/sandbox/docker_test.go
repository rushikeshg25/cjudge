package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSharedOutputCap(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &capture{remaining: 5, cancel: cancel}
	a, b := &stream{shared: c}, &stream{shared: c}
	a.Write([]byte("1234"))
	b.Write([]byte("56789"))
	if a.buf.String() != "1234" || b.buf.String() != "5" || !c.exceeded || ctx.Err() == nil {
		t.Fatalf("output was not capped: %+v", c)
	}
}

func TestDockerControlBoundsMetadata(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "docker-fixture")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexec head -c 2097152 /dev/zero\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	data, err := (&Docker{Binary: binary}).control(ctx, "info")
	if err == nil || !strings.Contains(err.Error(), "exceeded") || len(data) != 0 {
		t.Fatalf("unbounded metadata: bytes=%d err=%v", len(data), err)
	}
}
func TestDockerControlKeepsSmallResponsesAndErrors(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "docker-fixture")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf metadata\nprintf error >&2\nexit 7\n"), 0700); err != nil {
		t.Fatal(err)
	}
	data, err := (&Docker{Binary: binary}).control(context.Background(), "inspect")
	if err == nil || !strings.Contains(string(data), "metadata") || !strings.Contains(string(data), "error") {
		t.Fatalf("lost command result: %q %v", data, err)
	}
}
