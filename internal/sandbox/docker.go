package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rushikeshg25/cjudge/internal/domain"
)

type Docker struct{ Binary, Runtime string }

type capture struct {
	mu        sync.Mutex
	remaining int
	exceeded  bool
	cancel    context.CancelFunc
}
type stream struct {
	shared *capture
	buf    bytes.Buffer
	dst    io.Writer
}

func (s *stream) Write(p []byte) (int, error) {
	s.shared.mu.Lock()
	defer s.shared.mu.Unlock()
	n := min(len(p), s.shared.remaining)
	if s.dst != nil {
		if _, err := s.dst.Write(p[:n]); err != nil {
			s.shared.cancel()
			return 0, err
		}
	} else {
		s.buf.Write(p[:n])
	}
	s.shared.remaining -= n
	if n < len(p) {
		s.shared.exceeded = true
		s.shared.cancel()
	}
	return len(p), nil
}

// control bounds Docker metadata too: daemon failures or excessive orphan
// listings must not grow worker memory without limit.
func (d *Docker) control(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, d.Binary, args...)
	cmd.WaitDelay = time.Second
	cap := &capture{remaining: 1 << 20, cancel: cancel}
	output := &stream{shared: cap}
	cmd.Stdout, cmd.Stderr = output, output
	err := cmd.Run()
	if cap.exceeded {
		return nil, fmt.Errorf("Docker control output exceeded 1 MiB")
	}
	return output.buf.Bytes(), err
}

func (d *Docker) Run(ctx context.Context, r Request) (out Outcome, err error) {
	name := "cjudge-" + domain.NewID()
	args, err := createArgs(name, d.Runtime, r)
	if err != nil {
		return out, err
	}
	defer func() {
		if _, cleanupErr := d.control(context.Background(), "rm", "--force", name); cleanupErr != nil && err == nil {
			err = fmt.Errorf("sandbox cleanup failed: %w", cleanupErr)
		}
	}()
	if _, err = d.control(ctx, args...); err != nil {
		return out, fmt.Errorf("create sandbox: %w", err)
	}
	runCtx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()
	cap := &capture{remaining: r.OutputBytes, cancel: cancel}
	stdout, stderr := &stream{shared: cap}, &stream{shared: cap}
	var artifact *os.File
	if r.ArtifactPath != "" {
		artifact, err = os.CreateTemp(r.Workspace, ".artifact-")
		if err != nil {
			return out, err
		}
		defer os.Remove(artifact.Name())
		defer artifact.Close()
		stdout = &stream{shared: &capture{remaining: (32 << 20) + (64 << 10), cancel: cancel}, dst: artifact}
	}
	cmd := exec.CommandContext(runCtx, d.Binary, "start", "--attach", "--interactive", name)
	cmd.Stdin = strings.NewReader(r.Input)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.WaitDelay = time.Second
	started := time.Now()
	runErr := cmd.Run()
	out.Duration = time.Since(started)
	out.Stdout = stdout.buf.String()
	out.Stderr = stderr.buf.String()
	out.OutputExceeded = cap.exceeded || stdout.shared.exceeded
	out.TimedOut = runCtx.Err() == context.DeadlineExceeded
	if runCtx.Err() != nil {
		if _, killErr := d.control(context.Background(), "kill", name); killErr != nil {
			// Inspect below distinguishes an already exited process from a daemon failure.
		}
	}
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	stateBytes, inspectErr := d.control(context.Background(), "inspect", "--format", "{{json .State}}", name)
	if inspectErr != nil {
		return out, fmt.Errorf("inspect sandbox: %w", inspectErr)
	}
	var state struct {
		Running   bool
		ExitCode  int
		OOMKilled bool
		Error     string
	}
	if err = json.Unmarshal(stateBytes, &state); err != nil {
		return out, fmt.Errorf("decode sandbox state: %w", err)
	}
	if state.Running || state.Error != "" {
		return out, fmt.Errorf("sandbox did not terminate cleanly")
	}
	out.ExitCode = state.ExitCode
	out.OOM = state.OOMKilled
	if runErr != nil && state.ExitCode == 0 && !out.TimedOut && !out.OutputExceeded {
		return out, fmt.Errorf("attach sandbox: %w", runErr)
	}
	if r.ArtifactPath != "" && out.ExitCode == 0 && !out.OOM && !out.TimedOut && !out.OutputExceeded {
		if _, err = artifact.Seek(0, io.SeekStart); err != nil {
			return out, err
		}
		if err = extractArtifact(artifact, filepath.Join(r.Workspace, "program")); err != nil {
			return out, fmt.Errorf("export artifact: %w", err)
		}
	}
	return out, nil
}

func (d *Docker) Check(ctx context.Context, images []string) error {
	data, err := d.control(ctx, "info", "--format", "{{json .}}")
	if err != nil {
		return fmt.Errorf("docker unavailable: %w", err)
	}
	var info struct {
		OSType                 string
		MemoryLimit, PidsLimit bool
		Runtimes               map[string]json.RawMessage
	}
	if err = json.Unmarshal(data, &info); err != nil {
		return err
	}
	if info.OSType != "linux" || !info.MemoryLimit || !info.PidsLimit {
		return fmt.Errorf("Docker requires Linux memory and PID limit support")
	}
	if _, ok := info.Runtimes[d.Runtime]; !ok {
		return fmt.Errorf("required Docker runtime %q unavailable", d.Runtime)
	}
	for _, image := range images {
		if _, err := d.control(ctx, "image", "inspect", image); err != nil {
			return fmt.Errorf("sandbox image %s unavailable: %w", image, err)
		}
	}
	return nil
}
