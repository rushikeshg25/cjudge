// Package sandbox runs fixed language commands with untrusted source as data.
package sandbox

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"time"
)

type Request struct {
	Image       string
	Command     []string
	Workspace   string
	Input       string
	Timeout     time.Duration
	MemoryMB    int
	OutputBytes int
	Writable    bool
}

type Outcome struct {
	Stdout         string
	Stderr         string
	ExitCode       int
	OOM            bool
	TimedOut       bool
	OutputExceeded bool
	Duration       time.Duration
}

type Runner interface {
	Run(context.Context, Request) (Outcome, error)
}

func createArgs(name, runtime string, r Request) ([]string, error) {
	if !filepath.IsAbs(r.Workspace) || r.Image == "" || len(r.Command) == 0 || r.Timeout <= 0 || r.MemoryMB < 1 || r.OutputBytes < 1 {
		return nil, fmt.Errorf("invalid sandbox request")
	}
	// Docker's --mount CSV syntax must not reinterpret a path as extra options.
	for _, c := range r.Workspace {
		if c == ',' || c == '\n' || c == '\r' {
			return nil, fmt.Errorf("invalid workspace path")
		}
	}
	mem := strconv.Itoa(r.MemoryMB) + "m"
	mount := "type=bind,src=" + r.Workspace + ",dst=/work"
	if !r.Writable {
		mount += ",readonly"
	}
	args := []string{"create", "--pull=never", "--name", name, "--label", "cjudge.managed=true", "--label", "cjudge.expires=" + strconv.FormatInt(time.Now().Add(r.Timeout+time.Minute).Unix(), 10), "--network=none", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges:true", "--pids-limit=64", "--memory=" + mem, "--memory-swap=" + mem, "--cpus=1", "--ulimit", "nofile=64:64", "--ulimit", "fsize=67108864:67108864", "--user=65532:65532", "--log-driver=none", "--init", "--tmpfs", "/tmp:rw,nosuid,nodev,size=64m", "--mount", mount, "--workdir=/work", "--env=HOME=/tmp", "--env=TMPDIR=/tmp", "--env=GOCACHE=/tmp/go-cache", "--env=GOPATH=/tmp/go", "--env=GOMAXPROCS=1", "--interactive"}
	if runtime != "" {
		args = append(args, "--runtime", runtime)
	}
	args = append(args, r.Image)
	return append(args, r.Command...), nil
}
