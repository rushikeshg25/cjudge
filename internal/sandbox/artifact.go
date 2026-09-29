package sandbox

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// exportArtifact accepts exactly one regular file, never paths from an archive.
func (d *Docker) exportArtifact(ctx context.Context, name, workspace string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, d.Binary, "cp", name+":/tmp/program", "-")
	cmd.WaitDelay = time.Second
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		return err
	}
	err = extractArtifact(pipe, filepath.Join(workspace, "program"))
	if err != nil {
		cancel()
	}
	pipe.Close()
	waitErr := cmd.Wait()
	if err != nil {
		return err
	}
	return waitErr
}

func extractArtifact(r io.Reader, destination string) error {
	const maxArtifact = 32 << 20
	tr := tar.NewReader(io.LimitReader(r, maxArtifact+(64<<10)))
	h, err := tr.Next()
	if err != nil {
		return err
	}
	if h.Typeflag != tar.TypeReg || h.Size < 1 || h.Size > maxArtifact {
		return fmt.Errorf("invalid compiled artifact")
	}
	f, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0555)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, tr)
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if _, err = tr.Next(); err != io.EOF {
		return fmt.Errorf("unexpected extra artifact")
	}
	// Drain only bounded archive padding, allowing Docker to complete its pipe.
	_, err = io.Copy(io.Discard, io.LimitReader(r, 64<<10))
	return err
}
