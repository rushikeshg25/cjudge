package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL       string
	Listen            string
	Workers           int
	QueueLimit        int
	MaxAttempts       int
	Lease             time.Duration
	JobTimeout        time.Duration
	Poll              time.Duration
	Shutdown          time.Duration
	DockerBinary      string
	Runtime           string
	Workspace         string
	CPPImage          string
	PythonImage       string
	GoImage           string
	RequestsPerMinute int
}

func Load() (Config, error) {
	c := Config{DatabaseURL: os.Getenv("DATABASE_URL"), Listen: env("CJUDGE_LISTEN", ":8080"), DockerBinary: env("CJUDGE_DOCKER", "docker"), Runtime: env("CJUDGE_RUNTIME", "runsc"), Workspace: env("CJUDGE_WORKSPACE", "/tmp/cjudge"), CPPImage: env("CJUDGE_CPP_IMAGE", "cjudge-cpp:1"), PythonImage: env("CJUDGE_PYTHON_IMAGE", "cjudge-python:1"), GoImage: env("CJUDGE_GO_IMAGE", "cjudge-go:1")}
	if path := os.Getenv("DATABASE_URL_FILE"); path != "" {
		if c.DatabaseURL != "" {
			return c, fmt.Errorf("set only one of DATABASE_URL or DATABASE_URL_FILE")
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return c, fmt.Errorf("read database secret: %w", err)
		}
		if len(b) > 16384 {
			return c, fmt.Errorf("database secret too large")
		}
		c.DatabaseURL = strings.TrimSpace(string(b))
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("DATABASE_URL is required")
	}
	if !filepath.IsAbs(c.Workspace) || filepath.Clean(c.Workspace) == "/" || strings.ContainsAny(c.Workspace, ",\n\r") {
		return c, fmt.Errorf("CJUDGE_WORKSPACE must be an absolute dedicated directory without commas or line breaks")
	}
	for _, spec := range []struct {
		name          string
		dst           *int
		def, min, max int
	}{
		{"CJUDGE_WORKERS", &c.Workers, 2, 1, 64},
		{"CJUDGE_QUEUE_LIMIT", &c.QueueLimit, 1000, 1, 1000000},
		{"CJUDGE_MAX_ATTEMPTS", &c.MaxAttempts, 3, 1, 10},
		{"CJUDGE_REQUESTS_PER_MINUTE", &c.RequestsPerMinute, 120, 1, 100000},
	} {
		n, err := strconv.Atoi(env(spec.name, strconv.Itoa(spec.def)))
		if err != nil || n < spec.min || n > spec.max {
			return c, fmt.Errorf("%s must be %d..%d", spec.name, spec.min, spec.max)
		}
		*spec.dst = n
	}
	for _, spec := range []struct {
		name     string
		dst      *time.Duration
		def      string
		min, max time.Duration
	}{
		{"CJUDGE_LEASE", &c.Lease, "30s", 6 * time.Second, 5 * time.Minute},
		{"CJUDGE_JOB_TIMEOUT", &c.JobTimeout, "5m", 10 * time.Second, 30 * time.Minute},
		{"CJUDGE_POLL", &c.Poll, "500ms", 10 * time.Millisecond, 30 * time.Second},
		{"CJUDGE_SHUTDOWN", &c.Shutdown, "20s", time.Second, 5 * time.Minute},
	} {
		d, err := time.ParseDuration(env(spec.name, spec.def))
		if err != nil || d < spec.min || d > spec.max {
			return c, fmt.Errorf("invalid %s duration", spec.name)
		}
		*spec.dst = d
	}
	return c, nil
}

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
