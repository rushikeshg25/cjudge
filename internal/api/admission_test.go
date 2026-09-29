package api

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

type blockedReadiness struct {
	fakeRepository
	entered chan struct{}
	release chan struct{}
}

func (f *blockedReadiness) Ping(ctx context.Context) error {
	select {
	case f.entered <- struct{}{}:
	default:
	}
	select {
	case <-f.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func TestReadinessAdmissionPreservesLiveness(t *testing.T) {
	repo := &blockedReadiness{entered: make(chan struct{}, 2), release: make(chan struct{})}
	h := (&Server{Repo: repo, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}).Handler()
	done := make(chan int, 1)
	go func() { done <- request(h, "GET", "/readyz", "", "", "").Code }()
	defer close(repo.release)
	select {
	case <-repo.entered:
	case <-time.After(time.Second):
		t.Fatal("probe did not start")
	}
	for range 20 {
		w := request(h, "HEAD", "/readyz", "", "", "")
		if w.Code != 503 || w.Header().Get("Retry-After") != "1" {
			t.Fatalf("unbounded readiness: %d", w.Code)
		}
	}
	select {
	case <-repo.entered:
		t.Fatal("excess probe reached database")
	default:
	}
	if w := request(h, "GET", "/healthz", "", "", ""); w.Code != 200 {
		t.Fatalf("liveness blocked: %d", w.Code)
	}
	// The running probe's own deadline must also release its permit.
	select {
	case code := <-done:
		if code != 503 {
			t.Fatalf("deadline status: %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("probe deadline ignored")
	}
}
