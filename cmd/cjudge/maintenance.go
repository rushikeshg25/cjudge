package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/rushikeshg25/cjudge/internal/sandbox"
	"github.com/rushikeshg25/cjudge/internal/store"
)

func maintain(ctx context.Context, db *store.Store, docker *sandbox.Docker, log *slog.Logger, id string, slots int) {
	heartbeat := time.NewTicker(5 * time.Second)
	defer heartbeat.Stop()
	reap := time.NewTicker(30 * time.Second)
	defer reap.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			if err := db.Heartbeat(ctx, id, slots); err != nil {
				log.Error("worker heartbeat failed", "error", err)
			}
		case <-reap.C:
			if err := docker.Reap(ctx); err != nil {
				log.Error("orphan cleanup failed", "error", err)
			}
		}
	}
}
