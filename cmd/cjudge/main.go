package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rushikeshg25/cjudge/internal/api"
	"github.com/rushikeshg25/cjudge/internal/config"
	"github.com/rushikeshg25/cjudge/internal/judge"
	"github.com/rushikeshg25/cjudge/internal/sandbox"
	"github.com/rushikeshg25/cjudge/internal/store"
	"github.com/rushikeshg25/cjudge/internal/worker"
)

var version = "dev"

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(log, os.Args[1:]); err != nil {
		log.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger, args []string) error {
	if len(args) < 1 {
		return errors.New("usage: cjudge api|worker|migrate|create-principal|revoke-principal|version")
	}
	if args[0] == "version" {
		fmt.Println(version)
		return nil
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	openCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	db, err := store.Open(openCtx, cfg.DatabaseURL)
	cancel()
	if err != nil {
		return err
	}
	defer db.Close()
	switch args[0] {
	case "migrate":
		ctx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		return db.Migrate(ctx)
	case "create-principal":
		flags := flag.NewFlagSet("create-principal", flag.ContinueOnError)
		name := flags.String("name", "", "principal display name")
		admin := flags.Bool("admin", false, "administrator privileges")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		p, token, err := db.CreatePrincipal(ctx, *name, *admin)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"id": p.ID, "token": token, "admin": p.Admin})
	case "revoke-principal":
		flags := flag.NewFlagSet("revoke-principal", flag.ContinueOnError)
		id := flags.String("id", "", "principal ID")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		return db.RevokePrincipal(ctx, *id)
	case "api":
		app := &api.Server{Repo: db, Log: log, QueueLimit: cfg.QueueLimit, RequestsPerMinute: cfg.RequestsPerMinute}
		srv := &http.Server{Addr: cfg.Listen, Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
		done := make(chan error, 1)
		go func() { done <- srv.ListenAndServe() }()
		log.Info("api started", "address", cfg.Listen, "version", version)
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Shutdown)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			srv.Close()
			return err
		}
		return nil
	case "worker":
		docker := &sandbox.Docker{Binary: cfg.DockerBinary, Runtime: cfg.Runtime}
		if err := docker.Check(ctx, []string{cfg.CPPImage, cfg.PythonImage, cfg.GoImage}); err != nil {
			return err
		}
		j := &judge.Judge{Runner: docker, Languages: judge.Languages(cfg.CPPImage, cfg.PythonImage, cfg.GoImage), Workspace: cfg.Workspace}
		w := &worker.Worker{Repo: db, Judge: j, Log: log, Concurrency: cfg.Workers, MaxAttempts: cfg.MaxAttempts, Lease: cfg.Lease, JobTimeout: cfg.JobTimeout, Poll: cfg.Poll}
		done := make(chan struct{})
		go func() { w.Run(ctx); close(done) }()
		log.Info("worker started", "concurrency", cfg.Workers, "runtime", cfg.Runtime, "version", version)
		<-ctx.Done()
		select {
		case <-done:
			return nil
		case <-time.After(cfg.Shutdown):
			return errors.New("worker shutdown deadline exceeded; leases will recover")
		}
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
